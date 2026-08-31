package session

import (
	"io"
	"os/exec"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const defaultMaxBytes = 128 * 1024

// Capture keeps only a bounded, in-memory tail. It is never persisted and is
// discarded with the application process.
type Capture struct {
	mu       sync.Mutex
	data     []byte
	maxBytes int
}

func NewCapture(maxBytes int) *Capture {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	return &Capture{maxBytes: maxBytes}
}

func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = append(c.data, p...)
	if len(c.data) > c.maxBytes {
		copy(c.data, c.data[len(c.data)-c.maxBytes:])
		c.data = c.data[:c.maxBytes]
	}
	return len(p), nil
}

func (c *Capture) LastLines(limit int) []string {
	if limit <= 0 {
		return nil
	}
	c.mu.Lock()
	raw := string(append([]byte(nil), c.data...))
	c.mu.Unlock()

	lines := terminalLines(ansi.Strip(raw))
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

// CleanText removes terminal escapes, control characters and invalid byte
// sequences from external errors before they reach the TUI or artifacts.
func CleanText(value string) string {
	clean := ansi.Strip(strings.ToValidUTF8(value, "�"))
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r == '\n' || unicode.IsPrint(r) {
			return r
		}
		return -1
	}, clean)
}

func terminalLines(s string) []string {
	var lines []string
	var current []rune
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\r':
			// SSH commonly emits CRLF, while a PTY with ONLCR may turn an
			// application's explicit CRLF into CRCRLF. Treat the whole run before
			// LF as one newline. A standalone CR still means "return to column
			// zero" for progress lines and other terminal-style output.
			end := i
			for end+1 < len(runes) && runes[end+1] == '\r' {
				end++
			}
			if end+1 < len(runes) && runes[end+1] == '\n' {
				lines = append(lines, strings.TrimRight(string(current), " \t"))
				current = current[:0]
				i = end + 1
			} else {
				current = current[:0]
				i = end
			}
		case '\n':
			lines = append(lines, strings.TrimRight(string(current), " \t"))
			current = current[:0]
		case '\b':
			if len(current) > 0 {
				current = current[:len(current)-1]
			}
		case '\t':
			current = append(current, ' ', ' ', ' ', ' ')
		default:
			if unicode.IsPrint(r) {
				current = append(current, r)
			}
		}
	}
	if len(current) > 0 {
		lines = append(lines, strings.TrimRight(string(current), " \t"))
	}
	return lines
}

// Command decorates an exec.Cmd with bounded output capture while streaming
// bytes to the terminal. SSH stdin remains attached to the real TTY.
type Command struct {
	Cmd     *exec.Cmd
	Capture *Capture
	Banner  string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

func (c *Command) SetStdin(r io.Reader)  { c.stdin = r }
func (c *Command) SetStdout(w io.Writer) { c.stdout = w }
func (c *Command) SetStderr(w io.Writer) { c.stderr = w }

func (c *Command) Run() error {
	c.Cmd.Stdin = c.stdin
	if c.stdout != nil {
		if c.Banner != "" {
			_, _ = io.WriteString(c.stdout, c.Banner+"\n")
		}
		c.Cmd.Stdout = io.MultiWriter(c.stdout, c.Capture)
	}
	if c.stderr != nil {
		c.Cmd.Stderr = io.MultiWriter(c.stderr, c.Capture)
	}
	return c.Cmd.Run()
}
