//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestContainerRuntimeTUIEndToEnd exercises the real runtime CLI through the
// real TUI and PTYs. The Makefile runs the same contract for Docker and Podman
// when their daemon/service is available.
func TestContainerRuntimeTUIEndToEnd(t *testing.T) {
	if os.Getenv("SSHF_CONTAINER_RUNTIME_E2E") != "1" {
		t.Skip("run make test-container-docker or make test-container-podman")
	}
	runtimeName := os.Getenv("SSHF_CONTAINER_RUNTIME")
	if runtimeName != "docker" && runtimeName != "podman" {
		t.Fatalf("SSHF_CONTAINER_RUNTIME must be docker or podman, got %q", runtimeName)
	}
	requireCommand(t, runtimeName)
	image := os.Getenv("SSHF_CONTAINER_IMAGE")
	if image == "" {
		image = "sshfleet-test-sshd:local"
	}
	if output, err := exec.Command(runtimeName, "image", "inspect", image).CombinedOutput(); err != nil {
		t.Fatalf("inspect %s fixture image %q: %v: %s", runtimeName, image, err, output)
	}

	name := fmt.Sprintf("sshfleet-runtime-e2e-%s-%d", runtimeName, time.Now().UnixNano())
	start := exec.Command(runtimeName,
		"run", "--rm", "--detach", "--name", name,
		"--label", "sshfleet.e2e=true",
		image, "/bin/sh", "-c",
		"printf 'runtime-log-ready\\n'; trap 'exit 0' TERM INT; while :; do sleep 60; done",
	)
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start %s fixture: %v: %s", runtimeName, err, output)
	}
	t.Cleanup(func() { _ = exec.Command(runtimeName, "rm", "--force", name).Run() })
	waitForRuntimeContainer(t, runtimeName, name)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	binary := filepath.Join(dir, "sshf")
	writeFile(t, configPath, fmt.Sprintf(`version = 1

[app]
refresh_interval = "1h"
probe_enabled = false
load_user_ssh_config = false

[app.containers]
enabled = true
runtimes = [%q]
refresh_interval = "1h"
include_stopped = false
shell_policy = "first_available"
shell_priority = ["/bin/sh"]
`, runtimeName), 0o600)
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sshf: %v\n%s", err, output)
	}
	command := exec.Command(binary, "--config", configPath, "--no-user-ssh-config", "--no-probe", "--groups-dir", filepath.Join(dir, "groups.d"))
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 42, Cols: 170})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 170, 42)
	defer h.close()

	h.waitForScreen("GROUPS", 12*time.Second)
	h.send("/" + name + "\r")
	h.waitForScreen(name, 8*time.Second)
	h.waitForScreen("LOCAL CONTAINER", 8*time.Second)
	for _, field := range []string{"context:", "endpoint:", "platform:", "health: not-configured", "entrypoint:", "command:", "restart:", "networks:", "shell policy: first_available"} {
		h.waitForScreen(field, 8*time.Second)
	}
	h.screenshot(runtimeName + "-runtime-inspect")

	h.send("\r")
	menuTitle := "ACTIONS · sshfleet-runtime-e2e-" + runtimeName + "-"
	terminalTitle := "TERMINAL · sshfleet-runtime-e2e-" + runtimeName + "-"
	h.waitForScreen(menuTitle, 4*time.Second)
	for index, label := range []string{"Open container tab (default)", "Open container shell in Preview", "Follow container logs", "Refresh container"} {
		h.waitForScreen("› "+label, 4*time.Second)
		h.screenshot(fmt.Sprintf("%s-runtime-menu-%d", runtimeName, index+1))
		if index < 3 {
			h.send("\033[B")
		}
	}
	h.send("\033")
	h.waitForScreenGone(menuTitle, 4*time.Second)

	// A real runtime exec owns a nested PTY in Preview and returns cleanly.
	h.send("\r")
	h.waitForScreen(menuTitle, 4*time.Second)
	h.send("\033[B\r")
	h.waitForScreen(terminalTitle, 8*time.Second)
	h.send("printf 'runtime-shell-ready\\n'\r")
	h.waitForScreen("runtime-shell-ready", 8*time.Second)
	h.screenshot(runtimeName + "-runtime-preview-shell")
	h.send("exit\r")
	h.waitForScreenGone(terminalTitle, 8*time.Second)

	// Follow logs owns an independent terminal tab. Closing a live tab is an
	// explicit two-step action and restores the same filtered fleet/selection.
	h.send("\r\033[B\033[B\r")
	h.waitFor("runtime-log-ready", 8*time.Second)
	h.screenshot(runtimeName + "-runtime-follow-logs")
	h.send("\035") // Ctrl+]
	h.waitForScreen("Ctrl+] confirm close", 8*time.Second)
	h.send("\035")
	h.waitForScreen("› 1:Fleet", 8*time.Second)
	h.waitForScreen(name, 8*time.Second)
	h.screenshot(runtimeName + "-runtime-logs-return")

	h.send("q")
	if err := h.wait(8 * time.Second); err != nil {
		t.Fatalf("sshf did not exit cleanly after %s PTY test: %v\n%s", runtimeName, err, h.tail(5000))
	}
}

func waitForRuntimeContainer(t *testing.T, runtimeName, name string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command(runtimeName, "inspect", "--format", "{{.State.Running}}", name).CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == "true" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	logs, _ := exec.Command(runtimeName, "logs", name).CombinedOutput()
	t.Fatalf("%s fixture did not stay running: %s", runtimeName, logs)
}
