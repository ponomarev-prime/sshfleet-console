package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func TestConnectionLabelsDescribeActualTransport(t *testing.T) {
	for _, tt := range []struct {
		name       string
		host       inventory.Host
		effective  openssh.Effective
		unresolved bool
		want       string
	}{
		{name: "ssh", effective: openssh.Effective{User: "deploy", Hostname: "192.0.2.20", Port: "22"}, want: "[deploy @ 192.0.2.20]"},
		{name: "dns stays dns", effective: openssh.Effective{User: "deploy", Hostname: "app.example", Port: "2222"}, want: "[deploy @ app.example:2222]"},
		{name: "ipv6", effective: openssh.Effective{User: "root", Hostname: "2001:db8::1", Port: "2222"}, want: "[root @ [2001:db8::1]:2222]"},
		{name: "ipv6 default", effective: openssh.Effective{User: "root", Hostname: "2001:db8::1", Port: "22"}, want: "[root @ [2001:db8::1]]"},
		{name: "jump chain", effective: openssh.Effective{User: "root", Hostname: "192.0.2.21", ProxyJump: "gateway, ops@second:2222"}, want: "[root @ 192.0.2.21; via gateway -> ops@second:2222]"},
		{name: "custom proxy", effective: openssh.Effective{User: "git", Hostname: "git.example", CustomProxy: true}, want: "[git @ git.example; custom proxy]"},
		{name: "missing", effective: openssh.Effective{}, want: "[? @ ?]"},
		{name: "pending", unresolved: true, want: "[resolving...]"},
		{name: "failed", effective: openssh.Effective{Error: "private diagnostic"}, want: "[resolve failed]"},
		{name: "local", host: inventory.Host{Transport: inventory.TransportLocal}, want: "[local]"},
		{name: "local sshd", host: inventory.Host{Hostname: "127.0.0.1"}, effective: openssh.Effective{User: "demo", Hostname: "127.0.0.1", Port: "2200"}, want: "[demo @ 127.0.0.1:2200]"},
		{name: "container", host: inventory.Host{Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerContext: "remote-engine", ContainerID: strings.Repeat("a", 64)}, want: "[docker / remote-engine / aaaaaaaaaaaa]"},
		{name: "stale podman", host: inventory.Host{Transport: inventory.TransportContainer, ContainerRuntime: "podman", ContainerContext: "local", ContainerID: "abcd", ContainerDiscoveryState: "stale"}, want: "[podman / local / abcd; stale]"},
		{name: "external controls", effective: openssh.Effective{User: "\x1b[31mоператор\x1b[0m", Hostname: "app.example\n"}, want: "[оператор @ app.example]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.host.ID = "fixture:target"
			m := New([]inventory.Host{tt.host}, nil, openssh.Client{}, time.Minute, 2)
			if !tt.unresolved {
				m.effective[tt.host.ID] = tt.effective
			}
			if got := m.connectionLabel(tt.host); got != tt.want {
				t.Fatalf("label = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnectionResolutionCoversUnselectedNoProbeHostsAndCaches(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	binary := filepath.Join(dir, "ssh-fixture")
	script := fmt.Sprintf("#!/bin/sh\ncase \" $* \" in *' -G '*) ;; *) exit 90;; esac\nfor arg do target=$arg; done\nprintf '%%s\\n' \"$target\" >> %q\nprintf 'hostname %%s.example\\nuser deploy\\nport 22\\n' \"$target\"\n", log)
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := make([]inventory.Host, 13)
	for i := range hosts {
		hosts[i] = inventory.Host{ID: fmt.Sprintf("source:%d", i), Alias: fmt.Sprintf("node-%d", i), Probe: false}
	}
	m := New(hosts, nil, openssh.Client{Binary: binary}, time.Hour, 2)
	var pending []tea.Cmd
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		if batch, ok := cmd().(tea.BatchMsg); ok {
			pending = append(pending, batch...)
			return
		}
		t.Fatal("expected initial batch")
	}
	updated, cmd := m.Update(startResolveMsg{})
	m = updated.(Model)
	if len(m.resolveRunning) != 2 {
		t.Fatalf("started %d resolvers", len(m.resolveRunning))
	}
	run(cmd)
	for len(pending) > 0 {
		cmd = pending[0]
		pending = pending[1:]
		updated, next := m.Update(cmd())
		m = updated.(Model)
		if len(m.resolveRunning) > 2 {
			t.Fatalf("unbounded resolvers: %d", len(m.resolveRunning))
		}
		if next != nil {
			msg := next()
			if batch, ok := msg.(tea.BatchMsg); ok {
				pending = append(pending, batch...)
			} else {
				pending = append(pending, func() tea.Msg { return msg })
			}
		}
	}
	if len(m.effective) != len(hosts) {
		t.Fatalf("resolved %d of %d", len(m.effective), len(hosts))
	}
	if cmd = m.fillResolveSlots(); cmd != nil {
		t.Fatal("cached targets scheduled again")
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(string(calls))) != len(hosts) {
		t.Fatalf("unexpected calls: %s", calls)
	}
}

func TestConnectionResolutionReloadRejectsLateResults(t *testing.T) {
	host := inventory.Host{ID: "one:api", Alias: "api"}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Hour, 1)
	m.resolveRunning[host.ID] = 0
	m.effective[host.ID] = openssh.Effective{User: "old", Hostname: "old.example"}
	updated, _ := m.Update(sourceEditedMsg{id: host.ID, hosts: []inventory.Host{host}})
	m = updated.(Model)
	if len(m.effective) != 0 {
		t.Fatal("old endpoint remained after invalidation")
	}
	if cmd := m.fillResolveSlots(); cmd != nil {
		t.Fatal("reload exceeded running slot bound")
	}
	updated, _ = m.Update(resolveMsg{id: host.ID, epoch: 0, effective: openssh.Effective{Hostname: "old.example"}})
	m = updated.(Model)
	if _, ok := m.effective[host.ID]; ok {
		t.Fatal("late result restored the old endpoint")
	}
	if epoch, ok := m.resolveRunning[host.ID]; !ok || epoch != 1 {
		t.Fatal("replacement was not scheduled")
	}
	updated, _ = m.Update(resolveMsg{id: host.ID, epoch: 1, effective: openssh.Effective{User: "new", Hostname: "new.example"}})
	m = updated.(Model)
	if got := m.connectionLabel(host); got != "[new @ new.example]" {
		t.Fatal(got)
	}
}

func TestConnectionResolutionCapsLargeFleetAtFour(t *testing.T) {
	hosts := make([]inventory.Host, 30)
	for i := range hosts {
		hosts[i] = inventory.Host{ID: fmt.Sprint(i), Alias: fmt.Sprint(i)}
	}
	m := New(hosts, nil, openssh.Client{}, time.Hour, 100)
	if cmd := m.fillResolveSlots(); cmd == nil || len(m.resolveRunning) != 4 {
		t.Fatalf("running=%d, want 4", len(m.resolveRunning))
	}
	if cmd := m.fillResolveSlots(); cmd != nil {
		t.Fatal("scheduled work beyond the cap")
	}
}

func TestConnectionRowsPreserveMetricsProcessAndVisibleSelection(t *testing.T) {
	hosts := make([]inventory.Host, 12)
	for i := range hosts {
		hosts[i] = inventory.Host{ID: fmt.Sprint(i), Alias: fmt.Sprintf("node-%02d", i)}
	}
	m := New(hosts, nil, openssh.Client{}, time.Hour, 2)
	for _, host := range hosts {
		m.effective[host.ID] = openssh.Effective{User: "deploy", Hostname: "192.0.2.20", ProxyJump: "jump-1,jump-2"}
		m.results[host.ID] = probe.Result{Status: probe.StatusOnline, CPUCount: 4, TopProcess: probe.Process{PID: 42, Command: "worker"}}
	}
	m.selected = 11
	for _, width := range []int{35, 80, 180} {
		text := ansi.Strip(m.renderHosts(width, 13))
		for _, want := range []string{"node-11", "CPU", "MEM", "[deploy @", "worker"} {
			if !strings.Contains(text, want) {
				t.Fatalf("width %d missing %q:\n%s", width, want, text)
			}
		}
		if len(strings.Split(text, "\n")) > 13 {
			t.Fatalf("view overflows height:\n%s", text)
		}
		for _, line := range strings.Split(text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("line overflows width: %q", line)
			}
		}
	}
}

func TestConnectionLabelsAlwaysOccupyTheirOwnRow(t *testing.T) {
	hosts := []inventory.Host{
		{ID: "one:ssh", Alias: "6.16"},
		{ID: "container:one", Alias: "redis", Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerContext: "default", ContainerID: "0123456789abcdef"},
		{ID: "local", Alias: "workstation", Transport: inventory.TransportLocal},
	}
	m := New(hosts, nil, openssh.Client{}, time.Hour, 2)
	m.effective[hosts[0].ID] = openssh.Effective{User: "root", Hostname: "192.0.2.16"}
	for _, width := range []int{35, 80, 180} {
		lines := strings.Split(ansi.Strip(m.renderHosts(width, 14)), "\n")
		for _, host := range hosts {
			found := false
			for i, line := range lines {
				if strings.Contains(line, host.Alias) {
					found = true
					if strings.Contains(line, "[") || i+1 >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "[") {
						t.Fatalf("width %d alias %s label is not below name:\n%s", width, host.Alias, strings.Join(lines, "\n"))
					}
				}
			}
			if !found {
				t.Fatalf("width %d missing %s", width, host.Alias)
			}
		}
	}
}
