//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package session

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestEmbeddedPTYCarriesInputAndRendersOutput(t *testing.T) {
	command := exec.Command("sh", "-c", `IFS= read -r line; printf 'remote:%s\r\n' "$line"`)
	embedded, err := StartEmbedded(command, 40, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = embedded.Close() }()

	embedded.Terminal.SendText("hello-preview\n")
	for {
		data, readErr := embedded.ReadChunk()
		if len(data) > 0 {
			if _, err := embedded.Terminal.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if readErr != nil {
			break
		}
	}
	if err := embedded.Finish(); err != nil {
		t.Fatalf("embedded command: %v", err)
	}

	rendered := ansi.Strip(embedded.Terminal.Render())
	if !strings.Contains(rendered, "remote:hello-preview") {
		t.Fatalf("virtual screen does not contain remote output: %q", rendered)
	}
	if tail := strings.Join(embedded.Capture.LastLines(4), "\n"); !strings.Contains(tail, "remote:hello-preview") {
		t.Fatalf("capture does not contain remote output: %q", tail)
	}
}

func TestEmbeddedPTYResizeUpdatesVirtualAndRealTerminal(t *testing.T) {
	embedded, err := StartEmbedded(exec.Command("sh", "-c", "sleep 30"), 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = embedded.Close() }()
	if err := embedded.Resize(64, 12); err != nil {
		t.Fatal(err)
	}
	if embedded.Terminal.Width() != 64 || embedded.Terminal.Height() != 12 {
		t.Fatalf("virtual terminal = %dx%d", embedded.Terminal.Width(), embedded.Terminal.Height())
	}
}

func TestEmbeddedClosePromptlyReapsProcessAndIsIdempotent(t *testing.T) {
	embedded, err := StartEmbedded(exec.Command("sh", "-c", "trap '' HUP INT TERM; while :; do sleep 1; done"), 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	pid := embedded.Cmd.Process.Pid
	started := time.Now()
	firstErr := embedded.Close()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Close took %s", elapsed)
	}
	if firstErr == nil {
		t.Fatal("forced close unexpectedly reported a successful child exit")
	}
	if embedded.Cmd.ProcessState == nil {
		t.Fatal("child process was not reaped")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child pid %d still exists after Close: %v", pid, err)
	}
	if secondErr := embedded.Close(); secondErr == nil || secondErr.Error() != firstErr.Error() {
		t.Fatalf("second Close = %v, first = %v", secondErr, firstErr)
	}
}
