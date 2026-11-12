//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/ponomarev-prime/sshfleet-console/internal/runlog"
)

func TestTUIProgressiveStartupMetricsAndLaunchLogs(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "fixture-ssh")
	docker := filepath.Join(dir, "docker")
	source := filepath.Join(dir, "ssh.conf")
	app := filepath.Join(dir, "app.toml")
	gate := filepath.Join(dir, "release-runtime")
	logDir := filepath.Join(dir, "logs")
	writeExecutable(t, ssh, fmt.Sprintf(`#!/bin/sh
case " $* " in
 *" -V "*) printf 'OpenSSH_fixture\n' >&2; exit 0;;
 *" -G "*) printf 'hostname 192.0.2.20\nuser deploy\nport 22\n'; exit 0;;
esac
case " $* " in
 *" zslow "*) while ! [ -f '%s' ]; do sleep 0.02; done; printf 'fixture: connection timed out\n' >&2; exit 255;;
esac
n=0
if [ -f '%s/count' ]; then n=$(cat '%s/count'); fi
n=$((n+1)); printf '%%s' "$n" > '%s/count'
printf 'cpu_total=%%s\ncpu_idle=%%s\n' "$((n*100))" "$((n*50))"
printf 'cpu_count=4\nmem_total_kb=8192\nmem_available_kb=6144\nload=0.1 0.2 0.3\nuptime_seconds=3600\nroot_total_kb=16384\nroot_available_kb=8192\nprocess=42|S|worker|1.5\n'
`, gate, dir, dir, dir))
	writeExecutable(t, docker, fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "context show" ]; then
 while ! [ -f '%s' ]; do sleep 0.02; done
 printf 'fixture-context\n'; exit 0
fi
if [ "$1 $2" = "context inspect" ]; then printf '"unix:///fixture.sock"\n'; exit 0; fi
if [ "$1" = "ps" ]; then exit 0; fi
exit 64
`, gate))
	writeFile(t, source, "Host alpha zslow\n", 0o600)
	writeFile(t, app, fmt.Sprintf(`version = 1
[app]
refresh_interval = "1h"
connect_timeout = "1s"
max_concurrent = 2
probe_enabled = true
load_user_ssh_config = false
ssh_binary = %q
[app.containers]
enabled = true
runtimes = ["docker"]
refresh_interval = "1h"
[[sources]]
name = "fixture"
kind = "ssh_config"
path = %q
`, ssh, source), 0o600)
	binary := resolveE2EBinary(t, dir)
	command := exec.Command(binary, "--config", app, "--log-dir", logDir, "--no-user-ssh-config", "--sources-dir", filepath.Join(dir, "sources.d"), "--groups-dir", filepath.Join(dir, "groups.d"), "--overrides-dir", filepath.Join(dir, "hosts.d"))
	command.Env = append(os.Environ(), "TERM=xterm-256color", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 34, Cols: 180})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 180, 34)
	defer h.close()
	h.waitForScreen("Loading sources", 8*time.Second)
	h.waitForScreen("alpha", 8*time.Second)
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(h.screenText(), "\n") {
			if strings.Contains(line, "alpha") && strings.Contains(line, "50%") && strings.Contains(line, "25%") {
				ready = true
				break
			}
		}
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("CPU/MEM bars did not arrive while runtime was blocked:\n%s", h.screenText())
	}
	lines := strings.Split(h.screenText(), "\n")
	for i, line := range lines {
		if strings.Contains(line, "alpha") && strings.Contains(line, "50%") {
			if strings.Contains(line, "[deploy @") || i+1 >= len(lines) || !strings.Contains(lines[i+1], "[deploy @") {
				t.Fatalf("connection label is not below the alias:\n%s", h.screenText())
			}
		}
	}
	h.screenshot("startup-source-loading-metrics-ready")
	// The second worker is still inside a different SSH request. CPU must not
	// depend on releasing it or on the next (one-hour) refresh tick.
	pathBeforeRelease, err := runlog.Latest(logDir)
	if err != nil {
		t.Fatal(err)
	}
	beforeRelease, err := os.ReadFile(pathBeforeRelease)
	if err != nil {
		t.Fatal(err)
	}
	starts, ends := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(beforeRelease)), "\n") {
		var event runlog.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Active > 2 {
			t.Fatal("probe concurrency exceeded configured bound")
		}
		if event.Event == "probe.start" {
			starts++
		}
		if event.Event == "probe.end" {
			ends++
		}
	}
	if starts != 2 || ends != 1 {
		t.Fatalf("expected fast result with slow SSH pending: starts=%d ends=%d", starts, ends)
	}
	h.send("/alpha")
	h.waitForScreen("global host search: alpha", 3*time.Second)
	h.send("\033")
	h.waitForScreenGone("global host search: alpha", 3*time.Second)
	writeFile(t, gate, "ready", 0o600)
	path, err := runlog.Latest(logDir)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), `"event":"startup.ready"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.send("q")
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	firstView, metric, runtimeReady := -1, -1, -1
	for index, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event runlog.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "ui.first_view" {
			firstView = index
		}
		if event.Event == "probe.end" && event.CPUReady != nil && *event.CPUReady && metric < 0 {
			metric = index
		}
		if event.Event == "runtime.loaded" {
			runtimeReady = index
		}
	}
	if firstView < 0 || metric < 0 || runtimeReady < 0 || firstView >= runtimeReady || metric >= runtimeReady {
		t.Fatalf("event order: view=%d metrics=%d runtime=%d", firstView, metric, runtimeReady)
	}
	for _, private := range []string{"192.0.2.20", "fixture-ssh", "release-runtime", "deploy"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("private metadata logged: %s", private)
		}
	}
	logs := exec.Command(binary, "logs", "--dir", logDir, "--path")
	if output, err := logs.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != path {
		t.Fatalf("logs command: %s %v", output, err)
	}
}
