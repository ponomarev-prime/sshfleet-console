//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestTUIQuitConfirmationAndCancel(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "ssh.conf")
	app := filepath.Join(dir, "app.toml")
	writeFile(t, source, "Host fixture\n", 0o600)
	writeFile(t, app, fmt.Sprintf("version = 1\n[app]\nload_user_ssh_config = false\nprobe_enabled = false\n[[sources]]\nname = \"fixture\"\nkind = \"ssh_config\"\npath = %q\n", source), 0o600)
	command := exec.Command(resolveE2EBinary(t, dir), "--config", app, "--no-probe", "--no-log", "--sources-dir", filepath.Join(dir, "sources"), "--groups-dir", filepath.Join(dir, "groups"), "--overrides-dir", filepath.Join(dir, "hosts"))
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 100, 24)
	defer h.close()
	h.waitForScreen("HOSTS", 5*time.Second)
	h.send("q")
	h.waitForScreen("QUIT SSH FLEET?", 3*time.Second)
	h.screenshot("quit-confirmation")
	// A single q must leave the app running long enough to cancel and work.
	h.send("\033")
	h.waitForScreenGone("QUIT SSH FLEET?", 3*time.Second)
	h.send("/fixture")
	h.waitForScreen("global host search: fixture", 3*time.Second)
	h.send("q")
	h.waitForScreen("global host search: fixtureq", 3*time.Second)
	h.send("\025\033") // clear and leave search; q was literal search input.
	h.waitForScreenGone("global host search:", 3*time.Second)
	h.send("q")
	h.waitForScreen("QUIT SSH FLEET?", 3*time.Second)
	h.send("n")
	h.waitForScreenGone("QUIT SSH FLEET?", 3*time.Second)
	h.quit()
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatal(err)
	}
}
