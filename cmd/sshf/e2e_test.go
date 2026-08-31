//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

func TestTUIEndToEndProbeFilterOverlayGitAndQuit(t *testing.T) {
	dir := t.TempDir()
	sshConfig := filepath.Join(dir, "ssh_config")
	fakeSSH := filepath.Join(dir, "fake-ssh")
	fakeEditor := filepath.Join(dir, "fake-editor")
	inventoryPath := filepath.Join(dir, "inventory.toml")
	overridesDir := filepath.Join(dir, "hosts.d")
	binary := filepath.Join(dir, "sshf")

	writeExecutable(t, fakeSSH, `#!/bin/sh
alias_name=""
for arg do alias_name="$arg"; done
case " $* " in
  *" -G "*)
    if [ "$alias_name" = "git-demo" ]; then user=git; else user=root; fi
    printf 'hostname %s.example\nuser %s\nport 22\nidentityfile ~/.ssh/test-key\n' "$alias_name" "$user"
    exit 0
    ;;
esac
case " $* " in
  *" git-demo "*)
    printf '%s\n' 'debug1: Authenticated to git-demo.example ([192.0.2.20]:22) using "publickey".' >&2
    printf '%s\n' 'Welcome to GitLab, @fixture!' >&2
    exit 1
    ;;
esac
case "$*" in
  *"group-command-ok"*)
    printf '%s\n' 'group-command-ok'
    exit 0
    ;;
esac
case "$*" in
  *" -- alpha")
    if [ -t 0 ]; then
      printf '%s\n' 'preview-shell-ready'
      IFS= read -r input
      printf 'preview:%s\n' "$input"
      exit 0
    fi
    ;;
esac
printf '%s\n' \
  'cpu_total=1200' \
  'cpu_idle=900' \
  'cpu_count=4' \
  'mem_total_kb=8388608' \
  'mem_available_kb=4194304' \
  'swap_total_kb=2097152' \
  'swap_available_kb=1048576' \
  'root_total_kb=16777216' \
  'root_available_kb=8388608' \
  'load=0.10 0.20 0.30' \
  'uptime_seconds=3600' \
  'process=42|S|fixture|1.5' \
  'os_name=Fixture Linux' \
  'kernel=Linux 6.12.0' \
  'architecture=x86_64' \
  'init=systemd' \
  'systemd_version=257' \
  'systemd_state=running' \
  'systemd_failed_units=0' \
  'docker_state=not-installed' \
  'containerd_state=not-installed' \
  'podman_state=not-installed' \
  'kubelet_state=not-installed'
`)
	writeExecutable(t, fakeEditor, `#!/bin/sh
if ! [ -t 0 ] || ! [ -t 1 ] || ! [ -t 2 ]; then
  printf '%s\n' 'EDITOR-TTY-ERROR'
  exit 70
fi
printf '%s\n' 'EDITOR-TTY-READY'
saved_state=$(stty -g)
stty raw -echo
key=$(dd bs=1 count=3 2>/dev/null)
stty "$saved_state"
if [ "$key" != "$(printf '\033[B')" ]; then
  printf '\r\nEDITOR-ARROW-ERROR:%s\r\n' "$key"
  exit 71
fi
printf '\r\n%s\r\n' 'EDITOR-ARROW-OK'
printf '\nname = "Edited Git"\n' >> "$1"
`)
	writeFile(t, sshConfig, `Host alpha
    HostName alpha.example
    User root

Host git-demo
    HostName git-demo.example
    User git
`, 0o600)
	writeFile(t, inventoryPath, fmt.Sprintf(`version = 1

[app]
refresh_interval = "1h"
connect_timeout = "1s"
max_concurrent = 2
ssh_binary = %q

[app.containers]
enabled = false

[[sources]]
name = "fixture"
kind = "ssh_config"
path = %q

[[hosts]]
alias = "alpha"
source = "fixture"
name = "Alpha"

[[hosts]]
alias = "git-demo"
source = "fixture"
`, fakeSSH, sshConfig), 0o600)

	binary = resolveE2EBinary(t, dir)

	command := exec.Command(binary,
		"--config", inventoryPath,
		"--no-user-ssh-config",
		"--groups-dir", filepath.Join(dir, "groups.d"),
		"--overrides-dir", overridesDir,
		"--editor", fakeEditor,
	)
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 38, Cols: 150})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 150, 38)
	defer h.close()

	h.waitFor("cpu: 4 cores", 8*time.Second)
	h.waitFor("git: access via publickey", 8*time.Second)

	mark := h.mark()
	h.send("/git-demo\r")
	h.waitForAfter(mark, "git-demo", 3*time.Second)

	mark = h.mark()
	h.send("e")
	h.waitForAfter(mark, "EDITOR-TTY-READY", 5*time.Second)
	h.send("\033[B")
	h.waitForAfter(mark, "EDITOR-ARROW-OK", 5*time.Second)
	h.waitForAfter(mark, "Edited Git", 5*time.Second)
	overrides, err := filepath.Glob(filepath.Join(overridesDir, "*.toml"))
	if err != nil || len(overrides) != 1 {
		t.Fatalf("host overlays = %v, err = %v", overrides, err)
	}
	overlay, err := os.ReadFile(overrides[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(overlay), `alias = 'git-demo'`) || !strings.Contains(string(overlay), `name = "Edited Git"`) {
		t.Fatalf("unexpected overlay:\n%s", overlay)
	}

	mark = h.mark()
	h.send("\r\r")
	h.waitForAfter(mark, "Welcome to GitLab, @fixture!", 5*time.Second)
	// Git endpoints conventionally exit non-zero after a successful greeting.
	// Completion messages are intentionally transient and may be superseded by
	// another asynchronous UI update, so wait for the durable restored Fleet
	// state and captured session tail instead.
	h.waitForAfter(mark, "LAST SESSION · REMOTE", 8*time.Second)

	// Return to Alpha, choose the second menu action, and exercise a real PTY
	// nested in the Preview pane without suspending the fleet UI.
	mark = h.mark()
	h.send("\033")
	h.waitForAfter(mark, "Alpha", 3*time.Second)
	mark = h.mark()
	h.send("\r")
	h.waitForAfter(mark, "ACTIONS · Alpha", 3*time.Second)
	h.send("\033[B\r")
	h.waitForAfter(mark, "preview-shell-ready", 5*time.Second)
	h.send("hello-embedded\r")
	h.waitForAfter(mark, "preview:hello-embedded", 5*time.Second)
	h.waitForAfter(mark, "Preview terminal finished", 5*time.Second)

	mark = h.mark()
	h.send("c")
	h.waitForAfter(mark, "EDITOR-TTY-READY", 5*time.Second)
	h.send("\033[B")
	h.waitForAfter(mark, "EDITOR-ARROW-OK", 5*time.Second)
	h.waitForAfter(mark, "Config saved; restart SSH Fleet Console to apply", 5*time.Second)

	h.send("q")
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatalf("sshf did not exit cleanly: %v\n%s", err, h.tail(4000))
	}
}

func TestTUIRealNeovimReceivesArrowKeys(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	nvim := filepath.Join(repoRoot, "tools", "bin", "nvim")
	if info, err := os.Stat(filepath.Join(repoRoot, ".toolchain", "bin", "nvim")); err != nil || info.Mode().Perm()&0o111 == 0 {
		var lookupErr error
		nvim, lookupErr = exec.LookPath("nvim")
		if lookupErr != nil {
			t.Skip("Neovim is unavailable; generic editor TTY transfer remains covered by the main PTY E2E")
		}
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	resultPath := filepath.Join(dir, "cursor-line")
	binary := filepath.Join(dir, "sshf")
	writeFile(t, configPath, `version = 1

[app.containers]
enabled = false

[app.ui]
sources_width_percent = 10
preview_width_percent = 24
host_column_percent = 30
`, 0o600)
	if err := os.Mkdir(filepath.Join(dir, "sources.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	binary = resolveE2EBinary(t, dir)
	command := exec.Command(binary,
		"--config", configPath,
		"--no-user-ssh-config",
		"--no-probe",
		"--sources-dir", filepath.Join(dir, "sources.d"),
		"--groups-dir", filepath.Join(dir, "groups.d"),
		"--overrides-dir", filepath.Join(dir, "hosts.d"),
		"--editor", nvim,
	)
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 30, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 120, 30)
	defer h.close()
	h.waitForScreen("GROUPS", 8*time.Second)
	h.send("c")
	h.waitForScreen("host_column_percent", 8*time.Second)
	h.send("\033[B")
	h.send(fmt.Sprintf(":call writefile([string(line('.'))], '%s')\r", resultPath))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, readErr := os.ReadFile(resultPath); readErr == nil {
			if strings.TrimSpace(string(data)) != "2" {
				t.Fatalf("Neovim cursor line after Down = %q, want 2", data)
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("Neovim did not execute cursor assertion after Down: %v", err)
	}
	if screenshotDir := os.Getenv("SSHF_SCREENSHOT_DIR"); screenshotDir != "" {
		h.screenshot("real-nvim-arrow-down")
	}
	h.send(":q!\r")
	h.waitForScreen("Config saved; restart", 5*time.Second)
	h.send("q")
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatalf("sshf did not exit after Neovim: %v\n%s", err, h.tail(4000))
	}
}

func TestTUIEndToEndScreenshotsAndActionMenuTraversal(t *testing.T) {
	dir := t.TempDir()
	fakeSSH := filepath.Join(dir, "fake-ssh")
	sshConfig := filepath.Join(dir, "ssh_config")
	inventoryPath := filepath.Join(dir, "inventory.toml")
	sourcesDir := filepath.Join(dir, "sources.d")
	overridesDir := filepath.Join(dir, "hosts.d")
	workspaceBundle := filepath.Join(dir, "workspace.tar.gz")
	binary := filepath.Join(dir, "sshf")
	if err := os.MkdirAll(sourcesDir, 0o700); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, fakeSSH, `#!/bin/sh
alias_name=""
for arg do alias_name="$arg"; done
case " $* " in
  *" -G "*)
    printf 'hostname %s.example\nuser root\nport 22\nidentityfile ~/.ssh/test-key\n' "$alias_name"
    exit 0
    ;;
esac
case "$*" in
  *"group-command-ok"*)
    printf '%s\n' 'group-command-ok'
    exit 0
    ;;
esac
case "$*" in
  *" -- alpha")
    printf '%s\n' 'fixture-shell-ready'
    IFS= read -r input
    printf 'fixture:%s\n' "$input"
    exit 0
    ;;
esac
printf '%s\n' \
  'cpu_total=1200' \
  'cpu_idle=900' \
  'cpu_count=8' \
  'mem_total_kb=33554432' \
  'mem_available_kb=16777216' \
  'swap_total_kb=8388608' \
  'swap_available_kb=4194304' \
  'root_total_kb=67108864' \
  'root_available_kb=33554432' \
  'load=0.10 0.20 0.30' \
  'uptime_seconds=3600' \
  'process=42|S|fixture|1.5' \
  'os_name=Fixture Linux' \
  'kernel=Linux 6.12.0' \
  'architecture=x86_64' \
  'init=systemd' \
  'systemd_version=257' \
  'systemd_state=running' \
  'systemd_failed_units=0' \
  'docker_state=active' \
  'containerd_state=active' \
  'podman_state=not-installed' \
  'kubelet_state=not-installed'
`)
	writeFile(t, sshConfig, `Host alpha bravo charlie delta echo foxtrot
    HostName %h.example
    User root
`, 0o600)
	writeFile(t, inventoryPath, fmt.Sprintf(`version = 1

[app]
refresh_interval = "1h"
connect_timeout = "1s"
max_concurrent = 4
ssh_binary = %q

[app.containers]
enabled = false

[app.ui]
sources_width_percent = 10
preview_width_percent = 24

[[sources]]
name = "fixture"
kind = "ssh_config"
path = %q

[[groups]]
name = "all-fixtures"
match = ["fixture:*"]

[[command_presets]]
name = "group-health"
argv = ["printf", "group-command-ok"]
timeout = "3s"
max_concurrent = 2
`, fakeSSH, sshConfig), 0o600)
	writeFile(t, workspaceBundle, "workspace menu fixture", 0o600)

	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sshf: %v\n%s", err, output)
	}

	for _, size := range []struct {
		name       string
		cols, rows uint16
	}{
		{name: "medium", cols: 90, rows: 30},
		{name: "wide", cols: 150, rows: 38},
	} {
		t.Run(size.name, func(t *testing.T) {
			command := exec.Command(binary,
				"--config", inventoryPath,
				"--no-user-ssh-config",
				"--sources-dir", sourcesDir,
				"--groups-dir", filepath.Join(dir, "groups-"+size.name),
				"--overrides-dir", overridesDir,
			)
			command.Env = append(os.Environ(), "TERM=xterm-256color", "SSHF_WORKSPACE_BUNDLE="+workspaceBundle)
			terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: size.rows, Cols: size.cols})
			if err != nil {
				t.Fatal(err)
			}
			h := newPTYHarness(t, command, terminal, int(size.cols), int(size.rows))
			defer h.close()

			h.waitForScreen("CPU%", 8*time.Second)
			h.waitForHostRowHasBars("alpha", 8*time.Second)
			h.assertHostPaneDominates()
			if size.name == "medium" {
				h.assertHeaderAt("PREVIEW", 69)
			} else {
				h.assertHeaderAt("HOSTS", 20)
				h.assertHeaderAt("PREVIEW", 115)
			}

			h.send("?")
			h.waitForScreen("APPLICATION HEALTHCHECK", 3*time.Second)
			h.waitForScreen("editor", 3*time.Second)
			h.screenshot(size.name + "-healthcheck")
			h.send("?")
			h.waitForScreenGone("APPLICATION HEALTHCHECK", 3*time.Second)

			// Select the virtual cross-source group and exercise both plan and
			// confirmation before the bounded fan-out command starts.
			h.send("h\033[B\033[Blx")
			h.waitForScreen("GROUP COMMAND · all-fixtures", 3*time.Second)
			h.screenshot(size.name + "-group-command-select")
			h.send("\r")
			h.waitForScreen("targets: 6", 3*time.Second)
			h.screenshot(size.name + "-group-command-plan")
			h.send("\r")
			h.waitForScreen("Group all", 6*time.Second)
			h.screenshot(size.name + "-group-command-result")
			h.screenshot(size.name + "-fleet")

			h.send("\r")
			h.waitForScreen("ACTIONS · alpha", 3*time.Second)
			actions := []string{
				"Open terminal tab (default)",
				"Open terminal in Preview",
				"Manage group membership",
				"Open SSH Fleet workspace",
				"Refresh host",
				"Edit host overlay",
				"Edit source SSH config",
			}
			for index, label := range actions {
				h.waitForScreen("› "+label, 3*time.Second)
				h.screenshot(fmt.Sprintf("%s-menu-%d", size.name, index+1))
				if index+1 < len(actions) {
					h.send("\033[B")
				}
			}
			h.send("\033")
			h.waitForScreenGone("ACTIONS · alpha", 3*time.Second)

			// Open two independent PTY tabs, switch between them, and keep Fleet
			// available without launching a second TUI.
			h.send("\r\r")
			h.waitForScreen("fixture-shell-ready", 5*time.Second)
			h.waitForScreen("2:SSH · alpha", 5*time.Second)
			if screen := h.screenText(); strings.Contains(screen, "выход: Ctrl+D") {
				t.Fatalf("legacy foreground SSH path returned instead of a terminal tab:\n%s", screen)
			}
			h.screenshot(size.name + "-terminal-tab-1")
			// A plain q belongs to the nested terminal. It must neither quit sshf
			// nor become a Fleet hotkey while the terminal tab is active.
			h.send("q")
			h.waitForScreen("› 2:SSH · alpha", 3*time.Second)
			h.send("\0331") // Alt+1: permanent Fleet tab.
			h.waitForScreen("› 1:Fleet", 5*time.Second)
			h.send("\r\r")
			h.waitForScreen("3:SSH · alpha", 5*time.Second)
			h.screenshot(size.name + "-terminal-tab-2")
			h.send("\0332") // Alt+2: first terminal tab.
			h.waitForScreen("› 2:SSH · alpha", 5*time.Second)
			// A real outer bracketed paste must cross Bubble Tea as one PasteMsg
			// and reach the nested PTY without becoming SSH Fleet hotkeys.
			h.send("\033[200~first-tab-exit\n\033[201~")
			h.waitForScreen("qfirst-tab-exit", 5*time.Second)
			h.waitForScreen("› 1:Fleet", 5*time.Second)
			h.screenshot(size.name + "-terminal-tab-paste")
			h.send("\0333") // Alt+3: second terminal tab.
			h.waitForScreen("› 3:SSH · alpha", 5*time.Second)
			h.send("second-tab-exit\r")
			h.waitForScreen("second-tab-exit", 5*time.Second)
			h.waitForScreen("› 1:Fleet", 5*time.Second)
			h.screenshot(size.name + "-terminal-tabs-completed")

			// Closing a live tab is deliberately two-step and cancellable.
			h.send("\r\r")
			h.waitForScreen("fixture-shell-ready", 5*time.Second)
			closedAt := time.Now()
			h.send("\004") // Ctrl+D: immediate local close, even if the child ignores EOF.
			h.waitForScreen("› 1:Fleet", 2*time.Second)
			h.waitForScreenGone("4:SSH · alpha", 2*time.Second)
			if elapsed := time.Since(closedAt); elapsed > 1500*time.Millisecond {
				t.Fatalf("Ctrl+D tab close took %s", elapsed)
			}
			h.screenshot(size.name + "-terminal-tab-ctrl-d-close")

			// Ctrl+] remains the guarded close path for a live foreground process.
			h.send("\r\r")
			h.waitForScreen("fixture-shell-ready", 5*time.Second)
			h.send("\035") // Ctrl+]
			h.waitForScreen("Live foreground process", 5*time.Second)
			h.screenshot(size.name + "-terminal-tab-close-confirm")
			h.send("\033")
			h.waitForScreenGone("Live foreground process", 3*time.Second)
			h.send("\035\035")
			h.waitForScreenGone("4:SSH · alpha", 5*time.Second)

			// Then traverse to the Preview action and run the nested PTY while the
			// fleet remains on screen.
			h.send("\r\033[B\r")
			h.waitForScreen("fixture-shell-ready", 5*time.Second)
			h.waitForScreen("TERMINAL · alpha · Ctrl+]", 5*time.Second)
			if size.name == "medium" {
				h.assertHeaderAt("PREVIEW", 42)
			} else {
				h.assertHeaderAt("PREVIEW", 84)
			}
			h.screenshot(size.name + "-preview-terminal")
			h.send("embedded-exit\r")
			h.waitForScreenGone("TERMINAL · alpha · Ctrl+]", 5*time.Second)
			h.waitForScreen("fixture:embedded-ex", 5*time.Second)

			h.send("q")
			if err := h.wait(5 * time.Second); err != nil {
				t.Fatalf("sshf did not exit cleanly: %v\n%s", err, h.tail(4000))
			}
		})
	}
}

func TestTUIEndToEndGroupsCRUDAndMembership(t *testing.T) {
	dir := t.TempDir()
	sshConfig := filepath.Join(dir, "ssh_config")
	configPath := filepath.Join(dir, "config.toml")
	groupsDir := filepath.Join(dir, "groups.d")
	fakeSSH := filepath.Join(dir, "ssh")
	fakeEditor := filepath.Join(dir, "editor")
	binary := filepath.Join(dir, "sshf")
	writeExecutable(t, fakeSSH, `#!/bin/sh
alias_name=""
for arg do alias_name="$arg"; done
case " $* " in
  *" -G "*)
    printf 'hostname %s.example\nuser root\nport 22\n' "$alias_name"
    exit 0
    ;;
esac
exit 0
`)
	writeExecutable(t, fakeEditor, `#!/bin/sh
if ! [ -t 0 ] || ! [ -t 1 ] || ! [ -t 2 ]; then
  exit 70
fi
printf '\n# edited through group fragment\n' >> "$1"
`)
	writeFile(t, sshConfig, "Host demo-01 demo-02\n    HostName %h.example\n    User root\n", 0o600)
	writeFile(t, configPath, fmt.Sprintf(`version = 1

[app]
refresh_interval = "1h"
connect_timeout = "1s"
max_concurrent = 2
ssh_binary = %q
editor = %q

[app.containers]
enabled = false

[[sources]]
name = "fixture"
kind = "ssh_config"
path = %q
`, fakeSSH, fakeEditor, sshConfig), 0o600)
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sshf: %v\n%s", err, output)
	}
	command := exec.Command(binary,
		"--config", configPath,
		"--groups-dir", groupsDir,
		"--no-user-ssh-config",
		"--no-probe",
	)
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 32, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 120, 32)
	defer h.close()
	h.waitForScreen("GROUPS", 8*time.Second)

	// Create an empty local group from the left pane.
	h.send("hn")
	h.waitForScreen("CREATE GROUP", 3*time.Second)
	h.screenshot("groups-create-dialog")
	h.send("demo-stands\r")
	h.waitForScreenGone("CREATE GROUP", 3*time.Second)
	h.waitForScreen("demo-stands", 3*time.Second)
	h.screenshot("groups-empty")

	// Return to All, add both stable host IDs through the membership popup.
	h.send("gl")
	h.waitForScreen("demo-01", 3*time.Second)
	h.send("\r")
	h.waitForScreen("ACTIONS · demo-01", 3*time.Second)
	h.waitForScreen("Manage group membership", 3*time.Second)
	h.screenshot("groups-membership-action")
	h.send("\033[B\033[B\r")
	h.waitForScreen("GROUP MEMBERSHIP · demo-01", 3*time.Second)
	h.screenshot("groups-membership-demo-01")
	h.send("\r")
	h.waitForScreenGone("GROUP MEMBERSHIP", 3*time.Second)
	h.waitForScreen("demo-01 added", 3*time.Second)
	h.send("jm")
	h.waitForScreen("GROUP MEMBERSHIP · demo-02", 3*time.Second)
	h.send("\r")
	h.waitForScreenGone("GROUP MEMBERSHIP", 3*time.Second)
	h.waitForScreen("demo-02 added", 3*time.Second)

	// Select the group, verify both members, then rename and delete it.
	h.send("hG")
	h.waitForScreen("demo-stands", 3*time.Second)
	h.screenshot("groups-two-members")
	h.send("R")
	h.waitForScreen("RENAME GROUP", 3*time.Second)
	h.send("\025Demo Fleet\r")
	h.waitForScreenGone("RENAME GROUP", 3*time.Second)
	h.waitForScreen("Demo Fleet", 3*time.Second)
	h.screenshot("groups-renamed")
	h.send("e")
	// Race instrumentation can make the suspend/editor/reload round trip
	// noticeably slower on shared hosted runners. Keep the assertion strict but
	// give the real subprocess lifecycle the same bounded budget as startup.
	h.waitForScreen("Group edited", 8*time.Second)
	h.screenshot("groups-edited")
	h.send("D")
	h.waitForScreen("DELETE GROUP", 3*time.Second)
	h.screenshot("groups-delete-confirm")
	h.send("\r")
	h.waitForScreenGone("DELETE GROUP", 3*time.Second)
	h.waitForScreen("n create group", 3*time.Second)
	h.screenshot("groups-deleted")

	h.send("q")
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatalf("sshf did not exit cleanly: %v\n%s", err, h.tail(4000))
	}
}

func TestTUIEndToEndLocalContainerMenuPreviewAndLogs(t *testing.T) {
	dir := t.TempDir()
	fakeDocker := filepath.Join(dir, "docker")
	configPath := filepath.Join(dir, "config.toml")
	binary := filepath.Join(dir, "sshf")
	containerID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	writeExecutable(t, fakeDocker, fmt.Sprintf(`#!/bin/sh
if [ "$1" = "ps" ]; then
  printf '%%s\n' '{"ID":"%s","Names":"demo-box","Image":"alpine:3.20","State":"running","Status":"Up 2 minutes","Ports":"127.0.0.1:8080->80/tcp"}'
  exit 0
fi
if [ "$1" = "exec" ] && [ "$3" = "test" ]; then exit 0; fi
if [ "$1" = "exec" ] && [ "$2" = "--interactive" ]; then
  printf 'container-shell-ready\n'
  IFS= read -r input
  printf 'container:%%s\n' "$input"
  exit 0
fi
if [ "$1" = "logs" ]; then
  printf 'container-log-line\n'
  exit 0
fi
exit 64
`, containerID))
	writeFile(t, configPath, `version = 1

[app]
refresh_interval = "1h"
probe_enabled = false
load_user_ssh_config = false

[app.containers]
enabled = true
runtimes = ["docker"]
refresh_interval = "1h"
shell_priority = ["/bin/sh"]
`, 0o600)
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sshf: %v\n%s", err, output)
	}
	command := exec.Command(binary, "--config", configPath, "--no-user-ssh-config", "--no-probe", "--groups-dir", filepath.Join(dir, "groups.d"))
	command.Env = append(os.Environ(), "TERM=xterm-256color", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 34, Cols: 150})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 150, 34)
	defer h.close()

	h.waitForScreen("demo-box", 8*time.Second)
	h.waitForScreen("LOCAL CONTAINER", 8*time.Second)
	h.send("\r")
	h.waitForScreen("ACTIONS · demo-box", 3*time.Second)
	actions := []string{"Open container tab (default)", "Open container shell in Preview", "Follow container logs", "Refresh container"}
	for index, label := range actions {
		h.waitForScreen("› "+label, 3*time.Second)
		h.screenshot(fmt.Sprintf("container-menu-%d", index+1))
		if index+1 < len(actions) {
			h.send("\033[B")
		}
	}
	h.send("\033")
	h.waitForScreenGone("ACTIONS · demo-box", 3*time.Second)
	h.send("\r\r")
	h.waitForScreen("container-shell-ready", 5*time.Second)
	h.waitForScreen("2:Container shell", 5*time.Second)
	h.screenshot("container-terminal-tab")
	h.send("container-tab-exit\r")
	h.waitForScreen("container:container-tab-exit", 5*time.Second)
	h.waitForScreen("› 1:Fleet", 5*time.Second)
	h.send("\r")
	h.waitForScreen("ACTIONS · demo-box", 3*time.Second)
	h.send("\033[B\r")
	h.waitForScreen("container-shell-ready", 5*time.Second)
	h.waitForScreen("TERMINAL · demo-box · Ctrl+]", 5*time.Second)
	h.screenshot("container-preview-terminal")
	h.send("hello-container\r")
	h.waitForScreen("container:hello-container", 5*time.Second)
	h.waitForScreenGone("TERMINAL · demo-box · Ctrl+]", 5*time.Second)

	h.send("\r\033[B\033[B\r")
	h.waitForScreen("container-log-line", 5*time.Second)
	h.waitForScreen("› 1:Fleet", 5*time.Second)
	h.screenshot("container-logs-tab")
	h.send("q")
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatalf("sshf did not exit cleanly: %v\n%s", err, h.tail(4000))
	}
}

func TestTUIEndToEndTrustedLocalConfigMenuAndPreview(t *testing.T) {
	dir := t.TempDir()
	localShell := filepath.Join(dir, "local-shell")
	localConfig := filepath.Join(dir, "local.toml")
	configPath := filepath.Join(dir, "config.toml")
	binary := filepath.Join(dir, "sshf")
	writeExecutable(t, localShell, `#!/bin/sh
printf 'local-shell-ready\n'
IFS= read -r input
printf 'local:%s\n' "$input"
`)
	writeFile(t, localConfig, fmt.Sprintf(`version = 1
[[local_hosts]]
alias = "this-machine"
name = "Local workstation"
mode = "direct"
working_directory = %q
`, dir), 0o600)
	writeFile(t, configPath, fmt.Sprintf(`version = 1
[terminal]
default_shell = %q
shell_args = ["global arg with spaces"]
[app]
refresh_interval = "1h"
probe_enabled = false
load_user_ssh_config = false
[app.containers]
enabled = false
`, localShell), 0o600)
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sshf: %v\n%s", err, output)
	}
	command := exec.Command(binary, "--config", configPath, "--no-user-ssh-config", "--no-probe", "--groups-dir", filepath.Join(dir, "groups.d"), "--local-config", "workstation="+localConfig)
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 30, Cols: 140})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 140, 30)
	defer h.close()
	h.waitForScreen("Local workstation", 8*time.Second)
	h.waitForScreen("LOCAL MACHINE", 5*time.Second)
	h.waitForScreen("shell origin: toml", 5*time.Second)
	h.send("\r")
	h.waitForScreen("ACTIONS · Local workstation", 3*time.Second)
	for index, label := range []string{"Open local shell tab (default)", "Open local shell in Preview", "Refresh local host"} {
		h.waitForScreen("› "+label, 3*time.Second)
		h.screenshot(fmt.Sprintf("local-menu-%d", index+1))
		if index < 2 {
			h.send("\033[B")
		}
	}
	h.send("\033")
	h.waitForScreenGone("ACTIONS · Local workstation", 3*time.Second)
	h.send("\r\r")
	h.waitForScreen("local-shell-ready", 5*time.Second)
	h.waitForScreen("2:Local shell", 5*time.Second)
	h.screenshot("local-terminal-tab")
	h.send("local-tab-exit\r")
	h.waitForScreen("local:local-tab-exit", 5*time.Second)
	h.waitForScreen("› 1:Fleet", 5*time.Second)
	h.send("\r")
	h.waitForScreen("ACTIONS · Local workstation", 3*time.Second)
	h.send("\033[B\r")
	h.waitForScreen("local-shell-ready", 5*time.Second)
	h.waitForScreen("TERMINAL · Local workstation · Ctrl+]", 5*time.Second)
	h.screenshot("local-preview-terminal")
	h.send("hello-local\r")
	h.waitForScreen("local:hello-local", 5*time.Second)
	h.waitForScreenGone("TERMINAL · Local workstation · Ctrl+]", 5*time.Second)
	h.send("q")
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatalf("sshf did not exit cleanly: %v\n%s", err, h.tail(4000))
	}
}

type ptyHarness struct {
	t        *testing.T
	command  *exec.Cmd
	terminal *os.File
	mu       sync.Mutex
	output   []byte
	screen   *vt.SafeEmulator
	cols     int
	rows     int
	done     chan error
}

func newPTYHarness(t *testing.T, command *exec.Cmd, terminal *os.File, cols, rows int) *ptyHarness {
	t.Helper()
	h := &ptyHarness{
		t:        t,
		command:  command,
		terminal: terminal,
		screen:   vt.NewSafeEmulator(cols, rows),
		cols:     cols,
		rows:     rows,
		done:     make(chan error, 1),
	}
	// VT answers terminal capability/mode queries through its input pipe. The
	// application already receives the real PTY responses; this emulator is a
	// passive screen recorder, so drain its replies to keep parsing non-blocking.
	go func() { _, _ = io.Copy(io.Discard, h.screen) }()
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				h.mu.Lock()
				h.output = append(h.output, buffer[:n]...)
				h.mu.Unlock()
				_, _ = h.screen.Write(buffer[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { h.done <- command.Wait() }()
	return h
}

func (h *ptyHarness) screenText() string {
	return ansi.Strip(h.screen.Render())
}

func (h *ptyHarness) waitForScreen(needle string, timeout time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(h.screenText(), needle) {
			return
		}
		select {
		case err := <-h.done:
			h.t.Fatalf("process exited before screen contained %q: %v\n%s", needle, err, h.screenText())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timeout waiting for current screen to contain %q\n%s", needle, h.screenText())
}

func (h *ptyHarness) waitForScreenGone(needle string, timeout time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !strings.Contains(h.screenText(), needle) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timeout waiting for current screen to drop %q\n%s", needle, h.screenText())
}

func (h *ptyHarness) assertHostPaneDominates() {
	h.t.Helper()
	lines := strings.Split(h.screenText(), "\n")
	if len(lines) == 0 {
		h.t.Fatal("empty terminal screen")
	}
	header := ""
	for _, line := range lines {
		if strings.Contains(line, "HOSTS") && strings.Contains(line, "PREVIEW") {
			header = line
			break
		}
	}
	hostX := strings.Index(header, "HOSTS")
	previewX := strings.Index(header, "PREVIEW")
	if hostX < 0 || previewX < 0 || previewX <= hostX {
		h.t.Fatalf("HOSTS/PREVIEW headings not visible on the terminal screen:\n%s", h.screenText())
	}
	hostWidth := previewX - hostX
	previewWidth := h.cols - previewX
	if hostWidth <= previewWidth {
		h.t.Fatalf("HOSTS pane is not wider than PREVIEW: %d <= %d\n%s", hostWidth, previewWidth, h.screenText())
	}
}

func (h *ptyHarness) assertHostRowHasBars(host string) {
	h.t.Helper()
	for _, line := range strings.Split(h.screenText(), "\n") {
		if strings.Contains(line, host) && strings.Contains(line, "█") && strings.Contains(line, "░") {
			return
		}
	}
	h.t.Fatalf("host row %q does not preserve filled and empty utilization bars:\n%s", host, h.screenText())
}

func (h *ptyHarness) waitForHostRowHasBars(host string, timeout time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(h.screenText(), "\n") {
			if strings.Contains(line, host) && strings.Contains(line, "█") && strings.Contains(line, "░") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.assertHostRowHasBars(host)
}

func (h *ptyHarness) assertHeaderAt(title string, want int) {
	h.t.Helper()
	lines := strings.Split(h.screenText(), "\n")
	if len(lines) == 0 {
		h.t.Fatal("empty terminal screen")
	}
	header := ""
	for _, line := range lines {
		if strings.Contains(line, title) && (strings.Contains(line, "HOSTS") || strings.Contains(line, "PREVIEW")) {
			header = line
			break
		}
	}
	byteIndex := strings.Index(header, title)
	got := -1
	if byteIndex >= 0 {
		got = ansi.StringWidth(header[:byteIndex])
	}
	if got != want {
		h.t.Fatalf("%s header column = %d, want %d\n%s", title, got, want, h.screenText())
	}
}

func (h *ptyHarness) screenshot(name string) {
	h.t.Helper()
	directory := os.Getenv("SSHF_SCREENSHOT_DIR")
	if directory == "" {
		directory = h.t.TempDir()
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		h.t.Fatal(err)
	}
	text := h.screenText()
	if os.Getenv("SSHF_PUBLIC_SCREENSHOTS") == "1" {
		text = sanitizePublicScreenshot(text)
		assertPublicScreenshotSafe(h.t, text)
	}
	width := h.cols*9 + 24
	height := h.rows*19 + 24
	var rows strings.Builder
	for index, line := range strings.Split(text, "\n") {
		fmt.Fprintf(&rows, `<text x="12" y="%d">%s</text>`+"\n", 20+index*19, html.EscapeString(strings.TrimRight(line, " ")))
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<rect width="100%%" height="100%%" fill="#071015"/>
<g fill="#d8dee9" font-family="DejaVu Sans Mono,monospace" font-size="15" xml:space="preserve">
%s</g>
</svg>
`, width, height, width, height, rows.String())
	svgPath := filepath.Join(directory, name+".svg")
	if err := os.WriteFile(svgPath, []byte(svg), 0o600); err != nil {
		h.t.Fatal(err)
	}
	if magick, err := exec.LookPath("magick"); err == nil {
		pngPath := filepath.Join(directory, name+".png")
		command := exec.Command(magick, svgPath, pngPath)
		if output, err := command.CombinedOutput(); err != nil {
			h.t.Fatalf("render screenshot %s: %v\n%s", name, err, output)
		}
	}
	h.t.Logf("terminal screenshot: %s", svgPath)
}

var (
	publicHomePath = regexp.MustCompile(`/home/[A-Za-z0-9._-]+`)
	publicTestPath = regexp.MustCompile(`/tmp/Test[A-Za-z0-9._-]+(?:/[0-9]+)?`)
	privateIPv4    = regexp.MustCompile(`\b(?:10(?:\.[0-9]{1,3}){3}|192\.168(?:\.[0-9]{1,3}){2}|172\.(?:1[6-9]|2[0-9]|3[01])(?:\.[0-9]{1,3}){2})\b`)
	publicLatency  = regexp.MustCompile(`(?m)(latency:\s+)[^\s]+`)
	publicProbeAge = regexp.MustCompile(`(?m)( · )(?:now|\d+s ago)(\s*$)`)
)

func sanitizePublicScreenshot(text string) string {
	text = publicHomePath.ReplaceAllString(text, "/home/demo")
	text = publicTestPath.ReplaceAllString(text, "/tmp/sshfleet-demo")
	text = privateIPv4.ReplaceAllString(text, "192.0.2.10")
	text = publicLatency.ReplaceAllString(text, "${1}fixture")
	text = publicProbeAge.ReplaceAllString(text, "${1}fixture-age${2}")
	return text
}

func assertPublicScreenshotSafe(t testing.TB, text string) {
	t.Helper()
	for _, home := range publicHomePath.FindAllString(text, -1) {
		if home != "/home/demo" {
			t.Fatalf("public screenshot contains an unsanitized home path %q", home)
		}
	}
	if privateIPv4.MatchString(text) {
		t.Fatal("public screenshot contains a private IPv4 address")
	}
	if publicTestPath.MatchString(text) {
		t.Fatal("public screenshot contains a Go test temporary path")
	}
}

func TestPublicScreenshotSanitizer(t *testing.T) {
	input := "config: /home/private-user/my_code/sshfleet-console · binary: /tmp/TestPublic123/001/bin/ssh · target 10.23.45.67 · latency: 17ms\ntop: fixture[42] S 1.5% · 2s ago"
	got := sanitizePublicScreenshot(input)
	assertPublicScreenshotSafe(t, got)
	if got != "config: /home/demo/my_code/sshfleet-console · binary: /tmp/sshfleet-demo/bin/ssh · target 192.0.2.10 · latency: fixture\ntop: fixture[42] S 1.5% · fixture-age" {
		t.Fatalf("sanitized screenshot = %q", got)
	}
}

func (h *ptyHarness) send(value string) {
	h.t.Helper()
	if _, err := io.WriteString(h.terminal, value); err != nil {
		h.t.Fatalf("write PTY: %v", err)
	}
}

func (h *ptyHarness) mark() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.output)
}

func (h *ptyHarness) waitFor(needle string, timeout time.Duration) {
	h.waitForAfter(0, needle, timeout)
}

func (h *ptyHarness) waitForAfter(mark int, needle string, timeout time.Duration) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		start := min(mark, len(h.output))
		output := ansi.Strip(string(h.output[start:]))
		h.mu.Unlock()
		if strings.Contains(output, needle) {
			return
		}
		select {
		case err := <-h.done:
			h.t.Fatalf("process exited before %q: %v\n%s", needle, err, h.tail(4000))
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timeout waiting for %q\n%s", needle, h.tail(4000))
}

func (h *ptyHarness) wait(timeout time.Duration) error {
	select {
	case err := <-h.done:
		return err
	case <-time.After(timeout):
		return errors.New("timeout")
	}
}

func (h *ptyHarness) tail(limit int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	output := ansi.Strip(string(h.output))
	if len(output) > limit {
		output = output[len(output)-limit:]
	}
	return output
}

func (h *ptyHarness) close() {
	_ = h.terminal.Close()
	if h.command.Process != nil {
		_ = h.command.Process.Kill()
	}
}

func resolveE2EBinary(t *testing.T, dir string) string {
	t.Helper()
	if installed := strings.TrimSpace(os.Getenv("SSHF_E2E_BINARY")); installed != "" {
		absolute, err := filepath.Abs(installed)
		if err != nil {
			t.Fatalf("resolve SSHF_E2E_BINARY: %v", err)
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("SSHF_E2E_BINARY is not executable: %s", absolute)
		}
		return absolute
	}
	binary := filepath.Join(dir, "sshf")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sshf: %v\n%s", err, output)
	}
	return binary
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, content, 0o700)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
