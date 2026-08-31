package session

import (
	"bytes"
	"os/exec"
	"reflect"
	"testing"
)

func TestLastLinesStripsANSIAndAppliesCarriageReturn(t *testing.T) {
	c := NewCapture(1024)
	_, _ = c.Write([]byte("kept\r\npty\r\r\nold\rnew\n\x1b[31mred\x1b[0m\nlast\n"))
	got := c.LastLines(5)
	want := []string{"kept", "pty", "new", "red", "last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}

func TestCaptureIsBounded(t *testing.T) {
	c := NewCapture(5)
	_, _ = c.Write([]byte("123456789"))
	got := c.LastLines(1)
	if len(got) != 1 || got[0] != "56789" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestCommandStreamsAndCapturesOutput(t *testing.T) {
	capture := NewCapture(1024)
	var stdout bytes.Buffer
	command := &Command{
		Cmd:     exec.Command("sh", "-c", "printf 'first\\r\\nsecond\\n'"),
		Capture: capture,
		Banner:  "SSH Fleet Console banner",
	}
	command.SetStdout(&stdout)
	command.SetStderr(&stdout)
	if err := command.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := stdout.String(), "SSH Fleet Console banner\nfirst\r\nsecond\n"; got != want {
		t.Fatalf("streamed output = %q, want %q", got, want)
	}
	if got, want := capture.LastLines(2), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("captured lines = %#v, want %#v", got, want)
	}
}

func TestCleanTextRemovesEscapesControlsAndRepairsUTF8(t *testing.T) {
	got := CleanText("\x1b[31mred\x1b[0m\x00\x07 invalid:\xff")
	if got != "red invalid:�" {
		t.Fatalf("clean text = %q", got)
	}
}
