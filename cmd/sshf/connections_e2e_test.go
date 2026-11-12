//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

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

func TestTUIEndToEndConnectionLabelsWithoutProbes(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh-fixture")
	docker := filepath.Join(dir, "docker")
	config := filepath.Join(dir, "app.toml")
	source := filepath.Join(dir, "ssh.conf")
	local := filepath.Join(dir, "local.toml")
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	writeExecutable(t, ssh, `#!/bin/sh
for arg do target=$arg; done
case " $* " in
  *" -V "*) printf 'OpenSSH_fixture\n' >&2; exit 0;;
  *" -G "*) ;;
  *) printf 'UNEXPECTED_REMOTE_CONNECTION\n' >&2; exit 91;;
esac
case "$target" in
  api) printf 'hostname 192.0.2.20\nuser deploy\nport 22\n';;
  db) printf 'hostname db.internal.example\nuser postgres\nport 2222\nproxyjump jump-1,jump-2\n';;
  ipv6) printf 'hostname 2001:db8::1\nuser ops\nport 2200\n';;
  proxy) printf 'hostname proxy.example\nuser git\nport 22\nproxycommand private-command-hidden\n';;
esac
`)
	writeExecutable(t, docker, fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "context show" ]; then printf 'lab-context\n'; exit 0; fi
if [ "$1 $2" = "context inspect" ]; then printf '"unix:///var/run/docker.sock"\n'; exit 0; fi
if [ "$1" = "ps" ]; then
  printf '%%s\n' '{"ID":"%s","Names":"redis","Image":"fixture:local","State":"exited","Status":"Exited","Ports":""}'
  exit 0
fi
if [ "$1" = "inspect" ]; then
  printf '%%s\n' '[{"Id":"%s","Name":"/redis","Platform":"linux/amd64","Config":{"Image":"fixture:local"},"State":{"Status":"exited"}}]'
  exit 0
fi
exit 92
`, id, id))
	writeFile(t, source, "Host api db ipv6 proxy\n", 0o600)
	writeFile(t, local, "version = 1\n[[local_hosts]]\nalias = \"workstation\"\nmode = \"direct\"\nshell = \"/bin/sh\"\n", 0o600)
	writeFile(t, config, fmt.Sprintf(`version = 1
[app]
refresh_interval = "1h"
max_concurrent = 2
load_user_ssh_config = false
probe_enabled = false
ssh_binary = %q
[app.containers]
enabled = true
runtimes = ["docker"]
refresh_interval = "1h"
include_stopped = true
[[sources]]
name = "demo"
kind = "ssh_config"
path = %q
[[sources]]
name = "workstation"
kind = "local_config"
path = %q
`, ssh, source, local), 0o600)
	binary := resolveE2EBinary(t, dir)
	command := exec.Command(binary, "--config", config, "--no-probe", "--no-user-ssh-config", "--groups-dir", filepath.Join(dir, "groups.d"), "--overrides-dir", filepath.Join(dir, "hosts.d"))
	command.Env = append(os.Environ(), "TERM=xterm-256color", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 38, Cols: 180})
	if err != nil {
		t.Fatal(err)
	}
	h := newPTYHarness(t, command, terminal, 180, 38)
	defer h.close()
	for _, want := range []string{"[deploy @ 192.0.2.20]", "[postgres @ db.internal.example:2222; via jump-1 -> jump-2]", "[ops @ [2001:db8::1]:2200]", "[git @ proxy.example; custom proxy]", "[docker / lab-context / 0123456789ab]", "[local]"} {
		h.waitForScreen(want, 8*time.Second)
	}
	h.screenshot("connection-labels-wide")
	if text := h.screenText(); containsAny(text, "private-command-hidden", "UNEXPECTED_REMOTE_CONNECTION") {
		t.Fatal(text)
	}
	h.screen.Resize(100, 30)
	h.cols, h.rows = 100, 30
	if err := pty.Setsize(terminal, &pty.Winsize{Rows: 30, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	h.waitForScreen("PREVIEW", 5*time.Second)
	h.waitForScreen("[deploy @ 192.0.2.20]", 5*time.Second)
	h.screenshot("connection-labels-narrow")
	// G selects the last current row. Wait for the final startup sort rather
	// than assuming discovery order already matches alphabetical order.
	h.waitForScreenGone("Loading sources", 8*time.Second)
	h.send("G")
	h.waitForScreen("shell origin", 5*time.Second)
	h.waitForScreen("[local]", 5*time.Second)
	h.screenshot("connection-labels-last-selected")
	h.quit()
	if err := h.wait(5 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
