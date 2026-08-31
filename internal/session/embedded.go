package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

// Embedded owns a real local PTY running OpenSSH and a bounded virtual screen
// rendered inside the Preview pane. Nothing is persisted to disk.
type Embedded struct {
	Cmd      *exec.Cmd
	PTY      *os.File
	Terminal *vt.SafeEmulator
	Capture  *Capture

	inputDone    chan struct{}
	shutdownOnce sync.Once
	waitOnce     sync.Once
	waitErr      error
}

func StartEmbedded(cmd *exec.Cmd, width, height int) (*Embedded, error) {
	if cmd == nil {
		return nil, errors.New("embedded command is nil")
	}
	capabilities := platform.Current()
	if !capabilities.EmbeddedTerminalAvailable {
		return nil, fmt.Errorf("embedded terminal unavailable: %s adapter is not implemented on %s", capabilities.PTYBackend, capabilities.Platform())
	}
	width, height = max(1, width), max(1, height)
	environment := cmd.Env
	if environment == nil {
		environment = os.Environ()
	}
	cmd.Env = append(environment, "TERM=xterm-256color")
	terminal := vt.NewSafeEmulator(width, height)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)}) // #nosec G115 -- dimensions are bounded by the terminal.
	if err != nil {
		_ = terminal.Close()
		return nil, err
	}
	s := &Embedded{
		Cmd:       cmd,
		PTY:       ptmx,
		Terminal:  terminal,
		Capture:   NewCapture(defaultMaxBytes),
		inputDone: make(chan struct{}),
	}
	// The emulator encodes keys according to active terminal modes. Its input
	// pipe is copied into the real PTY for the lifetime of this session.
	go func() {
		defer close(s.inputDone)
		_, _ = io.Copy(ptmx, terminal)
	}()
	return s, nil
}

func (s *Embedded) ReadChunk() ([]byte, error) {
	buffer := make([]byte, 32*1024)
	n, err := s.PTY.Read(buffer)
	if n > 0 {
		buffer = buffer[:n]
		_, _ = s.Capture.Write(buffer)
		return buffer, err
	}
	return nil, err
}

func (s *Embedded) Resize(width, height int) error {
	width, height = max(1, width), max(1, height)
	s.Terminal.Resize(width, height)
	return pty.Setsize(s.PTY, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)}) // #nosec G115 -- dimensions are bounded by the terminal.
}

func (s *Embedded) Wait() error {
	s.waitOnce.Do(func() { s.waitErr = s.Cmd.Wait() })
	return s.waitErr
}

func (s *Embedded) Finish() error {
	s.stopIO()
	return s.Wait()
}

func (s *Embedded) Close() error {
	s.stopIO()
	if s.Cmd.Process != nil {
		_ = s.Cmd.Process.Kill()
	}
	return s.Wait()
}

func (s *Embedded) stopIO() {
	s.shutdownOnce.Do(func() {
		// Close the raw pipe writer first. SafeEmulator.Close currently mutates
		// emulator state while Read observes it without the same lock, so closing
		// the io.Pipe directly lets the forwarding goroutine leave before the
		// emulator itself is closed.
		_ = s.PTY.Close()
		if closer, ok := s.Terminal.InputPipe().(io.Closer); ok {
			_ = closer.Close()
		}
		<-s.inputDone
		_ = s.Terminal.Close()
	})
}
