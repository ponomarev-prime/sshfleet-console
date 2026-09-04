package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/knownhosts"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
	"github.com/ponomarev-prime/sshfleet-console/internal/toolcheck"
	"github.com/ponomarev-prime/sshfleet-console/internal/workspace"
)

func TestViewUsesConsoleProductName(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 100, 30
	if got := m.View().WindowTitle; got != "SSH Fleet Console" {
		t.Fatalf("window title = %q", got)
	}
}

func TestInterruptedExitAcceptsConventionalCtrlCExitCode(t *testing.T) {
	for _, code := range []string{"1", "130"} {
		err := exec.Command("sh", "-c", "exit "+code).Run()
		if !interruptedExit(err) {
			t.Fatalf("exit %s was not recognized as a runtime Ctrl+C stop: %v", code, err)
		}
	}
	if interruptedExit(exec.Command("sh", "-c", "exit 2").Run()) {
		t.Fatal("ordinary command failure was mistaken for Ctrl+C")
	}
}

func TestConfigKeyStartsConfiguredEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sshfleet", "config.toml")
	m := New(nil, nil, openssh.Client{}, time.Minute, 2,
		WithHostEditor("", "true", nil),
		WithAppConfigEditor(path),
	)
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c'}))
	got := updated.(Model)
	if cmd == nil || !got.editing {
		t.Fatalf("config editor = cmd:%v editing:%v message:%q", cmd != nil, got.editing, got.message)
	}
	if created, err := config.EnsureAppConfig(path); err != nil || created != path {
		t.Fatalf("config file = %q, %v", created, err)
	}
}

func TestSessionCompletionUsesBoundedScheduler(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "default:demo", Alias: "demo", Probe: true}},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	updated, cmd := m.Update(sessionFinishedMsg{index: 0, lines: []string{"uname -a", "Linux demo"}})
	got := updated.(Model)
	if got.active != 0 {
		t.Fatalf("active = %d, want 0 before scheduler message", got.active)
	}
	if cmd == nil {
		t.Fatal("expected a scheduler command")
	}
	if _, ok := cmd().(startPollMsg); !ok {
		t.Fatalf("command returned %T, want startPollMsg", cmd())
	}
	if len(got.sessionTail["default:demo"]) != 2 {
		t.Fatalf("session tail = %#v", got.sessionTail)
	}
}

func TestProbeSchedulerNeverExceedsConfiguredLimit(t *testing.T) {
	hosts := make([]inventory.Host, 50)
	for i := range hosts {
		hosts[i] = inventory.Host{ID: fmt.Sprintf("user:%d", i), Alias: fmt.Sprintf("host-%d", i), Probe: true}
	}
	m := New(hosts, nil, openssh.Client{}, time.Minute, 7)
	m.beginPoll()
	cmd := m.fillPollSlots()
	if cmd == nil {
		t.Fatal("expected scheduled probes")
	}
	if m.active != 7 {
		t.Fatalf("active = %d, want 7", m.active)
	}
	if len(m.queue) != 43 {
		t.Fatalf("queued = %d, want 43", len(m.queue))
	}
}

func TestRefreshTickIsQueuedWhileSweepIsActive(t *testing.T) {
	m := New([]inventory.Host{{ID: "user:demo", Alias: "demo", Probe: true}}, nil, openssh.Client{}, time.Minute, 1)
	m.active = 1
	updated, _ := m.Update(tickMsg(time.Now()))
	if !updated.(Model).refreshAfter {
		t.Fatal("refresh tick was lost during active sweep")
	}
}

func TestWideLayoutPrioritizesHostTable(t *testing.T) {
	left, middle, right := widePaneWidths(190, config.DefaultSourcesWidthPercent, config.DefaultPreviewWidthPercent)
	if left != 19 || middle != 124 || right != 45 {
		t.Fatalf("widths = %d/%d/%d", left, middle, right)
	}
	if left+middle+right+2 != 190 {
		t.Fatalf("panes do not fill terminal")
	}
	if !(middle > left && middle > right) {
		t.Fatalf("host pane is not dominant: %d/%d/%d", left, middle, right)
	}
	layout := newHostTableLayout(middle)
	if layout.usageWidth < 16 {
		t.Fatalf("usage bar width = %d, want at least 16", layout.usageWidth)
	}
}

func TestMediumLayoutPrioritizesHostTable(t *testing.T) {
	hosts, preview := hostAndPreviewPaneWidths(90, config.DefaultPreviewWidthPercent)
	if hosts != 67 || preview != 22 {
		t.Fatalf("host/preview widths = %d/%d", hosts, preview)
	}
	if hosts <= preview {
		t.Fatalf("host pane is not dominant: %d/%d", hosts, preview)
	}
	sources, hosts := sourceAndHostPaneWidths(90, config.DefaultSourcesWidthPercent)
	if sources != 18 || hosts != 71 || hosts <= sources {
		t.Fatalf("source/host widths = %d/%d", sources, hosts)
	}
}

func TestUILayoutPercentagesChangePaneWidths(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2, WithUILayout(config.UIConfig{
		SourcesWidthPercent: 15,
		PreviewWidthPercent: 25,
		HostColumnPercent:   26,
	}))
	if m.sourcesWidthPct != 15 || m.previewWidthPct != 25 || m.hostColumnPct != 26 {
		t.Fatalf("model UI layout = %d/%d/%d", m.sourcesWidthPct, m.previewWidthPct, m.hostColumnPct)
	}
	left, middle, right := widePaneWidths(200, m.sourcesWidthPct, m.previewWidthPct)
	if left != 30 || middle != 118 || right != 50 {
		t.Fatalf("configured widths = %d/%d/%d", left, middle, right)
	}
}

func TestHostColumnPercentageGivesSpaceToUtilizationBars(t *testing.T) {
	narrowHost := newHostTableLayout(135, 25)
	wideHost := newHostTableLayout(135, 50)
	if narrowHost.nameWidth >= wideHost.nameWidth {
		t.Fatalf("HOST widths = %d/%d, configured percentage has no effect", narrowHost.nameWidth, wideHost.nameWidth)
	}
	if narrowHost.usageWidth <= wideHost.usageWidth {
		t.Fatalf("usage widths = %d/%d, reclaimed space did not reach bars", narrowHost.usageWidth, wideHost.usageWidth)
	}
}

func TestHostTableShowsCapacityHeadersWithoutLoadAverage(t *testing.T) {
	result := probe.Result{
		Status:             probe.StatusOnline,
		CPUCount:           16,
		CPUAvailablePct:    75,
		CPUValid:           true,
		MemoryTotal:        32 * 1024 * 1024 * 1024,
		MemoryAvailablePct: 25,
		SwapTotal:          8 * 1024 * 1024 * 1024,
		Load:               [3]float64{12.71, 12.2, 12.25},
	}
	layout := newHostTableLayout(90)
	header := ansi.Strip(renderHostTableHeader(layout))
	got := ansi.Strip(renderHostMetrics(result, true, layout))
	for _, want := range []string{"CPU", "MEM", "SWAP", "CPU%", "MEM%"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header %q does not contain %q", header, want)
		}
	}
	for _, want := range []string{"16c", "32.0G", "8.0G", "25%", "75%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("metrics %q do not contain %q", got, want)
		}
	}
	if strings.Contains(header+got, "LA") || strings.Contains(got, "12.71") {
		t.Fatalf("host table unexpectedly contains load average: %q / %q", header, got)
	}
	result.SwapTotal = 0
	if got := ansi.Strip(renderHostMetrics(result, true, layout)); !strings.Contains(got, "—") {
		t.Fatalf("metrics %q do not show absent swap", got)
	}
}

func TestUtilizationCellUsesBoundedPercentages(t *testing.T) {
	for _, tt := range []struct {
		used float64
		want string
	}{
		{-5, "0%"},
		{42.4, "42%"},
		{140, "100%"},
	} {
		if got := ansi.Strip(utilizationCell(tt.used, true, 7)); !strings.Contains(got, tt.want) {
			t.Fatalf("utilizationCell(%v) = %q, want %q", tt.used, got, tt.want)
		}
		if got := ansi.StringWidth(utilizationCell(tt.used, true, 7)); got != 7 {
			t.Fatalf("utilizationCell(%v) width = %d, want 7", tt.used, got)
		}
	}
	if got := ansi.Strip(utilizationCell(0, false, 7)); !strings.Contains(got, "…") {
		t.Fatalf("invalid utilization = %q", got)
	}
}

func TestSelectedHostRowPreservesDtopStyleUtilizationBars(t *testing.T) {
	host := inventory.Host{ID: "fixture:203", Alias: "203", SourceName: "fixture"}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Minute, 1)
	m.focus = focusHosts
	m.results[host.ID] = probe.Result{
		Status:             probe.StatusOnline,
		CPUCount:           8,
		CPUValid:           true,
		CPUAvailablePct:    10,
		MemoryTotal:        32 * 1024 * 1024 * 1024,
		MemoryAvailablePct: 53,
		SwapTotal:          8 * 1024 * 1024 * 1024,
	}
	raw := m.renderHosts(120, 8)
	if !strings.Contains(raw, "48;5;238m") {
		t.Fatalf("selected host row does not use the neutral dtop-style highlight: %q", raw)
	}
	rendered := ansi.Strip(raw)
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 || !strings.Contains(lines[1], "203") || !strings.Contains(lines[1], "█") || !strings.Contains(lines[1], "░") {
		t.Fatalf("selected host row lost visible bars:\n%s", rendered)
	}
	if !strings.Contains(lines[1], "90%") || !strings.Contains(lines[1], "47%") {
		t.Fatalf("selected host row lost percentages:\n%s", rendered)
	}
}

func TestComponentText(t *testing.T) {
	if got, want := componentText(probe.Component{State: "active", Version: "28.1.1"}), "active · 28.1.1"; got != want {
		t.Fatalf("componentText() = %q, want %q", got, want)
	}
}

func TestEditorCommandRejectsShellSyntax(t *testing.T) {
	if _, err := editorCommand("vi --cmd", "/tmp/host.toml"); err == nil {
		t.Fatal("expected editor arguments to be rejected")
	}
}

func TestHealthcheckOverlayShowsResolvedOrigins(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2,
		WithHealth([]toolcheck.Result{{Name: "nvim", Path: "/opt/sshfleet/nvim", Origin: toolcheck.OriginSSHFleet}}, toolcheck.Result{Name: "nvim", Path: "/opt/sshfleet/nvim", Origin: toolcheck.OriginSSHFleet}),
	)
	m.width, m.height = 120, 28
	m = updateWithKey(t, m, tea.Key{Text: "?", Code: '?'})
	view := ansi.Strip(m.View().Content)
	if !m.healthVisible || !strings.Contains(view, "APPLICATION HEALTHCHECK") || !strings.Contains(view, "sshfleet · /opt/sshfleet/nvim") ||
		!strings.Contains(view, "SOURCES 10% · HOSTS 66% · PREVIEW 24%") || !strings.Contains(view, "HOST name 30%") {
		t.Fatalf("health overlay = visible:%v\n%s", m.healthVisible, view)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEsc})
	if m.healthVisible {
		t.Fatal("health overlay did not close")
	}
}

func TestGroupCommandPlanConfirmAndBoundedRun(t *testing.T) {
	dir := t.TempDir()
	fakeSSH := filepath.Join(dir, "ssh")
	if err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\nprintf 'group-output:%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := []inventory.Host{
		{ID: "user:alpha", Alias: "alpha", SourceName: "user", Groups: []string{"mixed"}},
		{ID: "lab:beta", Alias: "beta", SourceName: "lab", Groups: []string{"mixed"}},
	}
	m := New(hosts, nil, openssh.Client{Binary: fakeSSH}, time.Minute, 2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "mixed"}}, []config.Command{{Name: "uptime", Argv: []string{"uptime"}, Timeout: config.Duration{Duration: time.Second}, MaxConcurrent: 1}}),
	)
	m.width, m.height, m.source = 120, 30, 1
	m = updateWithKey(t, m, tea.Key{Text: "x", Code: 'x'})
	if !m.groupCommandMenu || m.groupCommandConfirm {
		t.Fatal("x did not open group command selection")
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	if !m.groupCommandConfirm || !strings.Contains(ansi.Strip(m.View().Content), "targets: 2") {
		t.Fatal("first Enter did not render immutable plan")
	}
	updated, command := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if !m.groupCommandRunning || command == nil {
		t.Fatal("second Enter did not start group command")
	}
	updated, _ = m.Update(command())
	m = updated.(Model)
	if m.groupCommandRunning || len(m.groupResults) != 2 || !strings.Contains(m.message, "0 failed") {
		t.Fatalf("group result = running:%v results:%#v message:%q", m.groupCommandRunning, m.groupResults, m.message)
	}
}

func TestGroupCommandRefusesEmptyGroup(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "empty"}}, []config.Command{{Name: "uptime", Argv: []string{"uptime"}, Timeout: config.Duration{Duration: time.Second}}}),
	)
	m.width, m.height, m.source = 120, 30, 1
	m = updateWithKey(t, m, tea.Key{Text: "x", Code: 'x'})
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	updated, command := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if command != nil || m.groupCommandRunning || !strings.Contains(m.message, "has no hosts") {
		t.Fatalf("empty group command = cmd:%v running:%v message:%q", command != nil, m.groupCommandRunning, m.message)
	}
}

func TestGitSessionTreatsNoShellGreetingAsSuccess(t *testing.T) {
	lines := []string{"Hi octocat! You've successfully authenticated, but GitHub does not provide shell access."}
	if !gitSessionSucceeded(lines) {
		t.Fatal("expected standard GitHub no-shell greeting to be successful")
	}
	if gitSessionSucceeded([]string{"Permission denied (publickey)."}) {
		t.Fatal("authentication failure must not be successful")
	}
}

func TestPreviewIncludesCapacityAndSystemMetadata(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "default:demo", Alias: "demo", SourceName: "default", ConfigPath: "/tmp/test-ssh-config", CredentialName: "stands", CredentialProvider: "secret-service"}},
		[]inventory.SourceSummary{{Name: "default", Hosts: 1}},
		openssh.Client{LocalClient: openssh.LocalClient{Path: "/usr/bin/ssh", Component: probe.Component{State: "installed", Version: "OpenSSH_9.8p1 local"}}},
		time.Minute,
		2,
	)
	m.width, m.height = 190, 45
	m.results["default:demo"] = probe.Result{
		Status:             probe.StatusOnline,
		CPUCount:           16,
		MemoryTotal:        32 * 1024 * 1024 * 1024,
		MemoryAvail:        18 * 1024 * 1024 * 1024,
		MemoryAvailablePct: 56.25,
		SwapTotal:          8 * 1024 * 1024 * 1024,
		SwapAvail:          6 * 1024 * 1024 * 1024,
		OSName:             "Ubuntu 24.04.3 LTS",
		Kernel:             "Linux 6.8.0",
		Architecture:       "x86_64",
		Init:               "systemd",
		Systemd:            probe.Component{State: "running", Version: "255"},
		SSHClient:          probe.Component{State: "installed", Version: "OpenSSH_9.9p2"},
		SSHServer:          probe.Component{State: "active", Version: "OpenSSH_9.9p2"},
		SSHAgent:           probe.Component{State: "running"},
		OpenSSL:            probe.Component{State: "installed", Version: "OpenSSL 3.5.0"},
		SSHServiceUnit:     "ssh.service",
		SSHSocketState:     "ssh.socket inactive",
		SSHTools:           "scp, sftp, ssh-keygen, ssh-add",
		Docker:             probe.Component{State: "active", Version: "28.1.1"},
		Containerd:         probe.Component{State: "active", Version: "v2.0.5"},
		Podman:             probe.Component{State: "not-installed"},
		Kubelet:            probe.Component{State: "not-installed"},
	}
	content := ansi.Strip(m.renderHosts(140, 44) + "\n" + m.renderPreview(100, 44))
	for _, want := range []string{
		"CPU",
		"MEM",
		"SWAP",
		"16c",
		"32.0G",
		"8.0G",
		"cpu: 16 cores",
		"memory: 32.0 GiB total · 18.0 GiB available",
		"swap: 8.0 GiB total · 6.0 GiB available",
		"CONNECTION · LOCAL",
		"config: /tmp/test-ssh-config",
		"credential: stands · secret-service",
		"SSH CLIENT · LOCAL",
		"binary: /usr/bin/ssh",
		"version: installed · OpenSSH_9.8p1 local",
		"SSH SOFTWARE · REMOTE",
		"client: installed · OpenSSH_9.9p2",
		"server: active · OpenSSH_9.9p2",
		"service unit: ssh.service",
		"agent: running",
		"openssl: installed · OpenSSL 3.5.0",
		"HOST STATUS · REMOTE",
		"SYSTEM · REMOTE",
		"os: Ubuntu 24.04.3 LTS",
		"systemd: running · 255 · failed 0",
		"docker: active · 28.1.1",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered preview does not contain %q\n%s", want, content)
		}
	}
	if strings.Contains(content, "load:") || strings.Contains(content, "LA ") {
		t.Fatalf("rendered UI unexpectedly contains load average\n%s", content)
	}
}

func TestPreviewShowsGitEndpointWithoutSystemMetrics(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "default:github.com", Alias: "github.com", SourceName: "default", Probe: true}},
		[]inventory.SourceSummary{{Name: "default", Hosts: 1}},
		openssh.Client{},
		time.Minute,
		2,
	)
	m.width, m.height = 150, 30
	m.results["default:github.com"] = probe.Result{
		Status:        probe.StatusGit,
		GitAuthMethod: "publickey",
		GitMessage:    "Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.",
	}
	content := ansi.Strip(m.renderHosts(100, 29) + "\n" + m.renderPreview(100, 29))
	for _, want := range []string{"git: access via publickey", "state: git-access", "authentication: publickey", "Git endpoint; shell"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered Git preview does not contain %q\n%s", want, content)
		}
	}
	if strings.Contains(content, "cpu:") || strings.Contains(content, "memory:") {
		t.Fatalf("Git endpoint unexpectedly shows system metrics\n%s", content)
	}
}

func TestHostKeyRepairIsTwoStepAndVisible(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "default:demo", Alias: "demo", SourceName: "default"}},
		[]inventory.SourceSummary{{Name: "default", Hosts: 1}},
		openssh.Client{},
		time.Minute,
		2,
	)
	m.width, m.height = 190, 35
	m.results["default:demo"] = probe.Result{
		Status:           probe.StatusHostKey,
		PresentedHostKey: "SHA256:new-key",
		KnownHostsFile:   "/home/test/.ssh/known_hosts",
		KnownHostsLine:   17,
	}
	m.sessionTail["default:demo"] = []string{"@@@@@@@@ warning banner @@@@@@@@"}
	updated, inspectCmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "K", Code: 'K'}))
	inspecting := updated.(Model)
	if !inspecting.hostKeyBusy || inspectCmd == nil || inspecting.hostKeyPlan != nil {
		t.Fatalf("first K must only start inspection: busy=%v cmd=%v plan=%#v", inspecting.hostKeyBusy, inspectCmd != nil, inspecting.hostKeyPlan)
	}

	plan := knownhosts.Plan{
		Lookup:               "demo",
		File:                 "/home/test/.ssh/known_hosts",
		Line:                 17,
		PresentedFingerprint: "SHA256:new-key",
		StoredFingerprints:   []string{"SHA256:old-key"},
	}
	updated, _ = inspecting.Update(hostKeyPlanMsg{index: 0, plan: plan})
	confirmation := updated.(Model)
	content := ansi.Strip(confirmation.renderPreview(100, 34))
	for _, want := range []string{"HOST KEY CHANGE", "SHA256:new-key", "SHA256:old-key", "VERIFY PRESENTED FINGERPRINT OUT OF BAND"} {
		if !strings.Contains(content, want) {
			t.Fatalf("confirmation does not contain %q\n%s", want, content)
		}
	}
	if strings.Contains(content, "LAST SESSION") || strings.Contains(content, "warning banner") {
		t.Fatalf("structured host-key repair must suppress duplicate raw warning\n%s", content)
	}
	updated, applyCmd := confirmation.Update(tea.KeyPressMsg(tea.Key{Text: "K", Code: 'K'}))
	applying := updated.(Model)
	if !applying.hostKeyBusy || applyCmd == nil {
		t.Fatalf("second K must start backup+remove: busy=%v cmd=%v", applying.hostKeyBusy, applyCmd != nil)
	}
}

func TestSuccessfulRepairForcesOneHostKeyPrompt(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "default:demo", Alias: "demo"}},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	updated, _ := m.Update(hostKeyAppliedMsg{
		index:  0,
		result: knownhosts.Applied{BackupPath: "/tmp/known_hosts.sshfleet-backup"},
	})
	repaired := updated.(Model)
	if !repaired.hostKeyPrompt["default:demo"] || repaired.hostKeyBackup["default:demo"] == "" {
		t.Fatalf("repair state = prompt:%v backup:%q", repaired.hostKeyPrompt["default:demo"], repaired.hostKeyBackup["default:demo"])
	}
	updated, _ = repaired.Update(sessionFinishedMsg{index: 0})
	connected := updated.(Model)
	if connected.hostKeyPrompt["default:demo"] {
		t.Fatal("successful repair connection must clear one-time host-key prompt")
	}
}

func TestSourceSelectionFiltersHosts(t *testing.T) {
	m := New(
		[]inventory.Host{
			{ID: "one:a", Alias: "a", SourceName: "one"},
			{ID: "two:b", Alias: "b", SourceName: "two"},
		},
		[]inventory.SourceSummary{{Name: "one"}, {Name: "two"}},
		openssh.Client{},
		time.Minute,
		2,
	)
	if len(m.visibleIndices()) != 2 {
		t.Fatal("All available must include every source")
	}
	m.source = 2
	visible := m.visibleIndices()
	if len(visible) != 1 || m.hosts[visible[0]].Alias != "b" {
		t.Fatalf("visible = %#v", visible)
	}
}

func TestOverlayReloadKeepsFilteredSelectionRelative(t *testing.T) {
	m := New(
		[]inventory.Host{
			{ID: "one:a", Alias: "a", SourceName: "one"},
			{ID: "one:z", Alias: "z", SourceName: "one"},
		},
		[]inventory.SourceSummary{{Name: "one", Hosts: 2}},
		openssh.Client{},
		time.Minute,
		2,
	)
	m.filter = "z"
	m.selected = 0
	updated, _ := m.Update(hostEditedMsg{
		id: "one:z",
		hosts: []inventory.Host{
			{ID: "one:a", Alias: "a", SourceName: "one"},
			{ID: "one:z", Alias: "z", Name: "Edited Z", SourceName: "one"},
		},
		sources: []inventory.SourceSummary{{Name: "one", Hosts: 2}},
	})
	got := updated.(Model)
	visible := got.visibleIndices()
	if got.selected != 0 || len(visible) != 1 || got.hosts[visible[0]].ID != "one:z" {
		t.Fatalf("selection = %d, visible = %#v", got.selected, visible)
	}
	// This was the crashing sequence: Enter after an overlay reload while a
	// filter reduced the visible list to one host. Enter now opens the action
	// menu; the second Enter executes its context-sensitive Git action.
	got.results["one:z"] = probe.Result{Status: probe.StatusGit, GitAuthMethod: "publickey"}
	opened, cmd := got.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd != nil || !opened.(Model).actionMenu {
		t.Fatal("expected Enter to open the action menu")
	}
	if _, cmd := opened.(Model).Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); cmd == nil {
		t.Fatal("expected a Git access command")
	}
}

func TestHostActionMenuIsContextAwareAndKeyboardDriven(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	m.width, m.height = 140, 30

	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	if !m.actionMenu || m.actionHostIndex != 0 {
		t.Fatalf("action menu state = open:%v host:%d", m.actionMenu, m.actionHostIndex)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"ACTIONS · alpha", "Open terminal tab (default)", "Open terminal in Preview", "Refresh host"} {
		if !strings.Contains(view, want) {
			t.Fatalf("action menu view does not contain %q:\n%s", want, view)
		}
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyDown})
	if m.actionSelected != 1 {
		t.Fatalf("action selection = %d, want 1", m.actionSelected)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEsc})
	if m.actionMenu {
		t.Fatal("Esc must close the action menu")
	}

	m.results["one:alpha"] = probe.Result{Status: probe.StatusGit}
	actions := m.availableHostActions(0)
	if len(actions) < 2 || actions[0].kind != actionGitCheck || actions[0].label != "Check Git access" {
		t.Fatalf("Git actions = %#v", actions)
	}
	m.results["one:alpha"] = probe.Result{Status: probe.StatusHostKey}
	actions = m.availableHostActions(0)
	if len(actions) < 3 || actions[0].kind != actionRepairHostKey {
		t.Fatalf("host-key actions = %#v", actions)
	}
	m.hostKeyPrompt["one:alpha"] = true
	actions = m.availableHostActions(0)
	if len(actions) < 2 || actions[0].kind != actionFullTerminal || actions[0].label != "Verify host key in terminal tab" {
		t.Fatalf("replacement-key actions = %#v", actions)
	}
}

func TestHostActionMenuExactContextMatrix(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one"}
	reload := func() ([]inventory.Host, []inventory.SourceSummary, error) { return []inventory.Host{host}, nil, nil }
	base := New([]inventory.Host{host}, nil, openssh.Client{}, time.Minute, 2, WithHostEditor(t.TempDir(), "true", reload))
	base.width, base.height = 140, 30

	tests := []struct {
		name   string
		setup  func(*Model)
		kinds  []hostActionKind
		labels []string
	}{
		{"normal", func(*Model) {}, []hostActionKind{actionFullTerminal, actionPreviewTerminal, actionRefreshHost, actionEditHost}, []string{"Open terminal tab (default)", "Open terminal in Preview", "Refresh host", "Edit host overlay"}},
		{"git", func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusGit} }, []hostActionKind{actionGitCheck, actionRefreshHost, actionEditHost}, []string{"Check Git access", "Refresh host", "Edit host overlay"}},
		{"host-key", func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusHostKey} }, []hostActionKind{actionRepairHostKey, actionFullTerminal, actionRefreshHost, actionEditHost}, []string{"Inspect host-key repair", "Open terminal tab", "Refresh host", "Edit host overlay"}},
		{"replacement-key", func(m *Model) { m.hostKeyPrompt[host.ID] = true }, []hostActionKind{actionFullTerminal, actionRefreshHost, actionEditHost}, []string{"Verify host key in terminal tab", "Refresh host", "Edit host overlay"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base
			m.results = make(map[string]probe.Result)
			m.hostKeyPrompt = make(map[string]bool)
			tt.setup(&m)
			actions := m.availableHostActions(0)
			if len(actions) != len(tt.kinds) {
				t.Fatalf("actions = %#v", actions)
			}
			for i := range actions {
				if actions[i].kind != tt.kinds[i] || actions[i].label != tt.labels[i] {
					t.Fatalf("action[%d] = %#v, want %v/%q", i, actions[i], tt.kinds[i], tt.labels[i])
				}
			}
		})
	}
	base.width = 50
	if actions := base.availableHostActions(0); len(actions) != 3 || actions[1].kind != actionRefreshHost {
		t.Fatalf("narrow actions must omit Preview: %#v", actions)
	}
	if base.availableHostActions(-1) != nil || base.availableHostActions(1) != nil {
		t.Fatal("out-of-range hosts must not expose actions")
	}
}

func TestHostActionMenuAddsMembershipOnlyWhenGroupsExist(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one"}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Minute, 2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "operators"}}, nil),
	)
	m.width, m.height = 140, 30
	actions := m.availableHostActions(0)
	if len(actions) < 3 || actions[2].kind != actionGroupMembership || actions[2].label != "Manage group membership" {
		t.Fatalf("membership action = %#v", actions)
	}
	m.actionMenu, m.actionHostIndex, m.actionSelected = true, 0, 2
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	got := updated.(Model)
	if cmd != nil || got.actionMenu || got.groupDialog.mode != groupDialogMembership || got.groupDialog.hostID != host.ID {
		t.Fatalf("membership dispatch = menu:%v dialog:%#v cmd:%v", got.actionMenu, got.groupDialog, cmd != nil)
	}
}

func TestHostActionMenuSelectionBoundariesCancelAndDispatch(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Probe: true}
	m := New([]inventory.Host{host}, nil, openssh.Client{Binary: "true"}, time.Minute, 2)
	m.width, m.height = 140, 30
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	for range 20 {
		m = updateWithKey(t, m, tea.Key{Code: tea.KeyDown})
	}
	if m.actionSelected != 2 {
		t.Fatalf("selection passed last action: %d", m.actionSelected)
	}
	for range 20 {
		m = updateWithKey(t, m, tea.Key{Code: tea.KeyUp})
	}
	if m.actionSelected != 0 {
		t.Fatalf("selection passed first action: %d", m.actionSelected)
	}
	m = updateWithKey(t, m, tea.Key{Text: "j", Code: 'j'})
	if m.actionSelected != 1 || !strings.Contains(ansi.Strip(m.View().Content), "Open terminal in Preview") {
		t.Fatalf("keyboard selection/view = %d\n%s", m.actionSelected, ansi.Strip(m.View().Content))
	}
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	dispatched := updated.(Model)
	if dispatched.actionMenu || !dispatched.embeddedStarting || cmd == nil {
		t.Fatalf("Preview dispatch = menu:%v starting:%v cmd:%v", dispatched.actionMenu, dispatched.embeddedStarting, cmd != nil)
	}

	for _, cancel := range []tea.Key{{Code: tea.KeyEsc}, {Text: "h", Code: 'h'}, {Text: "q", Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}} {
		candidate := m
		candidate.actionMenu = true
		candidate.actionSelected = 1
		candidate = updateWithKey(t, candidate, cancel)
		if candidate.actionMenu {
			t.Fatalf("%v did not close action menu", cancel)
		}
	}
}

func TestHostActionMenuLocalAndContainerTargetsExposeTransportSpecificRows(t *testing.T) {
	local := inventory.Host{ID: "local:here", Alias: "here", SourceName: "local", ConfigPath: "/tmp/local.toml", Transport: inventory.TransportLocal, Shell: "/bin/sh"}
	container := inventory.Host{
		ID: "container:docker:0123456789abcdef", Alias: "demo-box", SourceName: "containers · docker",
		Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerID: "0123456789abcdef",
		ContainerImage: "alpine:3.20", ContainerState: "running", ContainerStatus: "Up 1 minute", ContainerShells: []string{"/bin/sh"},
		ContainerContext: "default", ContainerEndpoint: "unix:///var/run/docker.sock", ContainerPlatform: "linux/amd64",
		ContainerHealth: "healthy", ContainerEntrypoint: "/entrypoint", ContainerCommand: "serve", ContainerRestart: "unless-stopped",
		ContainerMounts: "/src→/dst (bind,ro)", ContainerNetworks: "bridge", ContainerShellPolicy: config.ContainerShellPolicyFirstAvailable,
	}
	reload := func() ([]inventory.Host, []inventory.SourceSummary, error) {
		return []inventory.Host{local, container}, nil, nil
	}
	m := New([]inventory.Host{local, container}, nil, openssh.Client{}, time.Minute, 2, WithHostEditor(t.TempDir(), "true", reload))
	m.width, m.height = 140, 30
	localActions := m.availableHostActions(0)
	wantLocal := []hostActionKind{actionFullTerminal, actionPreviewTerminal, actionRefreshHost, actionEditSource}
	if len(localActions) != len(wantLocal) {
		t.Fatalf("local actions = %#v", localActions)
	}
	for i := range wantLocal {
		if localActions[i].kind != wantLocal[i] {
			t.Fatalf("local action[%d] = %#v", i, localActions[i])
		}
	}
	containerActions := m.availableHostActions(1)
	wantContainer := []hostActionKind{actionFullTerminal, actionPreviewTerminal, actionContainerLogs, actionRefreshHost}
	if len(containerActions) != len(wantContainer) {
		t.Fatalf("container actions = %#v", containerActions)
	}
	for i := range wantContainer {
		if containerActions[i].kind != wantContainer[i] {
			t.Fatalf("container action[%d] = %#v", i, containerActions[i])
		}
	}
	m.selected = 1
	m.results[container.ID] = probe.Result{Status: probe.StatusOnline}
	view := ansi.Strip(m.renderHosts(100, 20) + "\n" + m.renderPreview(60, 25))
	for _, want := range []string{"alpine:3.20", "LOCAL CONTAINER", "runtime: docker", "context: default", "platform: linux/amd64", "health: healthy", "shell policy: first_available", "shell priority: /bin/sh"} {
		if !strings.Contains(view, want) {
			t.Fatalf("container view missing %q:\n%s", want, view)
		}
	}
}

func TestHostActionMenuStoppedContainerOffersNoShell(t *testing.T) {
	host := inventory.Host{ID: "container:docker:0123456789abcdef", Alias: "stopped", Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerID: "0123456789abcdef", ContainerState: "exited"}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 140, 30
	actions := m.availableHostActions(0)
	if len(actions) != 2 || actions[0].kind != actionContainerLogs || actions[1].kind != actionRefreshHost {
		t.Fatalf("stopped container actions = %#v", actions)
	}
}

func TestDynamicContainerRefreshReplacesOnlyDynamicTargets(t *testing.T) {
	static := inventory.Host{ID: "ssh:alpha", Alias: "alpha"}
	oldContainer := inventory.Host{ID: "container:docker:0123456789abcdef", Alias: "old", Transport: inventory.TransportContainer}
	newContainer := inventory.Host{ID: "container:docker:abcdef0123456789", Alias: "new", Transport: inventory.TransportContainer}
	m := New([]inventory.Host{static, oldContainer}, []inventory.SourceSummary{{Name: "ssh"}, {Name: "containers · docker", Dynamic: true}}, openssh.Client{}, time.Minute, 2)
	updated, _ := m.Update(dynamicSourcesMsg{hosts: []inventory.Host{newContainer}, sources: []inventory.SourceSummary{{Name: "containers · docker", Hosts: 1, Dynamic: true}}})
	got := updated.(Model)
	if len(got.hosts) != 2 || (got.hosts[0].ID != newContainer.ID && got.hosts[1].ID != newContainer.ID) {
		t.Fatalf("dynamic hosts = %#v", got.hosts)
	}
	for _, host := range got.hosts {
		if host.ID == oldContainer.ID {
			t.Fatalf("stale container survived refresh: %#v", got.hosts)
		}
	}
	if len(got.sources) != 2 || !got.sources[1].Dynamic {
		t.Fatalf("dynamic sources = %#v", got.sources)
	}
	m.active = 1
	blocked, _ := m.Update(dynamicSourcesMsg{hosts: []inventory.Host{newContainer}})
	if len(blocked.(Model).hosts) != 2 || blocked.(Model).hosts[1].ID != oldContainer.ID {
		t.Fatal("dynamic refresh mutated indices during an active probe sweep")
	}
}

func TestDynamicContainerRefreshPreservesLastKnownTargetsAsStaleOnRuntimeFailure(t *testing.T) {
	oldContainer := inventory.Host{ID: "container:docker:0123456789abcdef", Alias: "old", SourceName: "containers · docker", Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerID: "0123456789abcdef", ContainerState: "running"}
	failure := inventory.SourceSummary{Name: "containers · docker", Dynamic: true, State: inventory.SourceStateDenied, Err: fmt.Errorf("permission denied")}
	m := New([]inventory.Host{oldContainer}, []inventory.SourceSummary{{Name: "containers · docker", Hosts: 1, Dynamic: true, State: inventory.SourceStateLoaded}}, openssh.Client{}, time.Minute, 2)
	updated, _ := m.Update(dynamicSourcesMsg{sources: []inventory.SourceSummary{failure}})
	got := updated.(Model)
	if len(got.hosts) != 1 || got.hosts[0].ContainerDiscoveryState != string(inventory.SourceStateStale) || !strings.Contains(got.hosts[0].ContainerDiscoveryError, "denied") {
		t.Fatalf("stale target = %#v", got.hosts)
	}
	if len(got.sources) != 1 || got.sources[0].State != inventory.SourceStateStale || got.sources[0].Hosts != 1 {
		t.Fatalf("stale source = %#v", got.sources)
	}
	if !strings.Contains(got.message, "stale") {
		t.Fatalf("stale footer = %q", got.message)
	}
}

func TestUnavailableSourceExplainsFailureInPreview(t *testing.T) {
	m := New(nil, []inventory.SourceSummary{{
		Name: "containers · podman", Path: "podman", Dynamic: true,
		State: inventory.SourceStateDenied, Err: fmt.Errorf("permission denied connecting to runtime"),
	}}, openssh.Client{}, time.Minute, 1)
	m.source = 1
	view := ansi.Strip(m.renderPreview(80, 20))
	for _, want := range []string{"containers · podman", "state: denied", "origin: podman", "permission denied connecting to runtime"} {
		if !strings.Contains(view, want) {
			t.Fatalf("source preview %q does not contain %q", view, want)
		}
	}
}

func TestHostActionMenuContainerRefreshRunsSingleDynamicDiscovery(t *testing.T) {
	host := inventory.Host{ID: "container:docker:0123456789abcdef", Alias: "demo", Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerID: "0123456789abcdef"}
	calls := 0
	discover := func() ([]inventory.Host, []inventory.SourceSummary) {
		calls++
		return []inventory.Host{host}, []inventory.SourceSummary{{Name: "containers · docker", Hosts: 1, Dynamic: true}}
	}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Minute, 2, WithDynamicDiscovery(time.Second, discover))
	updated, cmd := m.executeHostAction(0, actionRefreshHost)
	busy := updated.(Model)
	if cmd == nil || !busy.dynamicBusy {
		t.Fatalf("container refresh = cmd:%v busy:%v", cmd != nil, busy.dynamicBusy)
	}
	if _, duplicate := busy.executeHostAction(0, actionRefreshHost); duplicate != nil {
		t.Fatal("second container discovery started concurrently")
	}
	message := cmd()
	finished, _ := busy.Update(message)
	if finished.(Model).dynamicBusy || calls != 1 {
		t.Fatalf("discovery completion = busy:%v calls:%d", finished.(Model).dynamicBusy, calls)
	}
}

func TestHostActionMenuExposesAndDispatchesBundledWorkspace(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Probe: true}
	m := New([]inventory.Host{host}, nil, openssh.Client{Binary: "true", WorkspaceBundle: "/tmp/bundle.tar.gz", WorkspaceCleanup: true}, time.Minute, 2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "operators"}}, nil),
	)
	m.width, m.height = 140, 30
	actions := m.availableHostActions(0)
	want := []string{
		"Open terminal tab (default)",
		"Open terminal in Preview",
		"Manage group membership",
		"Open SSH Fleet workspace",
		"Refresh host",
	}
	if len(actions) != len(want) {
		t.Fatalf("workspace actions = %#v", actions)
	}
	for index, label := range want {
		if actions[index].label != label {
			t.Fatalf("action[%d] = %q, want %q", index, actions[index].label, label)
		}
	}
	candidate := updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	for range 3 {
		candidate = updateWithKey(t, candidate, tea.Key{Code: tea.KeyDown})
	}
	updated, cmd := candidate.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	got := updated.(Model)
	if cmd == nil || !got.workspaceStarting || got.actionMenu {
		t.Fatalf("workspace row = cmd:%v starting:%v menu:%v", cmd != nil, got.workspaceStarting, got.actionMenu)
	}
	continued, session := got.Update(workspacePreparedMsg{index: 0, tool: workspace.ToolShell, remotePath: "/tmp/sshfleet-workspace.A1b2C3"})
	ready := continued.(Model)
	if session == nil || ready.workspaceStarting {
		t.Fatalf("prepared workspace = cmd:%v starting:%v", session != nil, ready.workspaceStarting)
	}
}

func TestHostActionMenuDispatchesEachContextPrimaryAction(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Probe: true}
	tests := []struct {
		name  string
		setup func(*Model)
		check func(Model, tea.Cmd) bool
	}{
		{"full-terminal", func(*Model) {}, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
		{"git-check", func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusGit} }, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
		{"host-key-repair", func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusHostKey} }, func(m Model, cmd tea.Cmd) bool { return m.hostKeyBusy && cmd != nil }},
		{"replacement-key", func(m *Model) { m.hostKeyPrompt[host.ID] = true }, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New([]inventory.Host{host}, nil, openssh.Client{Binary: "true"}, time.Minute, 2)
			m.width, m.height = 140, 30
			tt.setup(&m)
			m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
			updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			got := updated.(Model)
			if got.actionMenu || !tt.check(got, cmd) {
				t.Fatalf("primary action dispatch = menu:%v cmd:%v state:%#v", got.actionMenu, cmd != nil, got)
			}
		})
	}
}

func TestHostActionMenuDispatchesEverySelectableRowThroughKeys(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Probe: true}
	tests := []struct {
		name     string
		selected int
		setup    func(*Model)
		verify   func(Model, tea.Cmd) bool
	}{
		{"normal/full", 0, func(*Model) {}, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
		{"normal/preview", 1, func(*Model) {}, func(m Model, cmd tea.Cmd) bool { return m.embeddedStarting && cmd != nil }},
		{"normal/refresh", 2, func(*Model) {}, func(m Model, cmd tea.Cmd) bool { return m.active == 1 && m.polling[host.ID] && cmd != nil }},
		{"normal/edit", 3, func(*Model) {}, func(m Model, cmd tea.Cmd) bool { return m.editing && cmd != nil }},
		{"git/check", 0, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusGit} }, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
		{"git/refresh", 1, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusGit} }, func(m Model, cmd tea.Cmd) bool { return m.active == 1 && cmd != nil }},
		{"git/edit", 2, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusGit} }, func(m Model, cmd tea.Cmd) bool { return m.editing && cmd != nil }},
		{"host-key/repair", 0, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusHostKey} }, func(m Model, cmd tea.Cmd) bool { return m.hostKeyBusy && cmd != nil }},
		{"host-key/full", 1, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusHostKey} }, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
		{"host-key/refresh", 2, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusHostKey} }, func(m Model, cmd tea.Cmd) bool { return m.active == 1 && cmd != nil }},
		{"host-key/edit", 3, func(m *Model) { m.results[host.ID] = probe.Result{Status: probe.StatusHostKey} }, func(m Model, cmd tea.Cmd) bool { return m.editing && cmd != nil }},
		{"replacement/full", 0, func(m *Model) { m.hostKeyPrompt[host.ID] = true }, func(_ Model, cmd tea.Cmd) bool { return cmd != nil }},
		{"replacement/refresh", 1, func(m *Model) { m.hostKeyPrompt[host.ID] = true }, func(m Model, cmd tea.Cmd) bool { return m.active == 1 && cmd != nil }},
		{"replacement/edit", 2, func(m *Model) { m.hostKeyPrompt[host.ID] = true }, func(m Model, cmd tea.Cmd) bool { return m.editing && cmd != nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reload := func() ([]inventory.Host, []inventory.SourceSummary, error) { return []inventory.Host{host}, nil, nil }
			m := New([]inventory.Host{host}, nil, openssh.Client{Binary: "true"}, time.Minute, 2, WithHostEditor(t.TempDir(), "true", reload))
			m.width, m.height = 140, 30
			tt.setup(&m)
			m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
			for range tt.selected {
				m = updateWithKey(t, m, tea.Key{Text: "j", Code: 'j'})
			}
			if m.actionSelected != tt.selected {
				t.Fatalf("selected = %d, want %d", m.actionSelected, tt.selected)
			}
			updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			got := updated.(Model)
			if got.actionMenu || !tt.verify(got, cmd) {
				t.Fatalf("dispatch row %d = menu:%v cmd:%v message:%q", tt.selected, got.actionMenu, cmd != nil, got.message)
			}
		})
	}
}

func TestHostActionMenuAndActionsFailClosedOnInvalidStates(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Probe: true}
	m := New([]inventory.Host{host}, nil, openssh.Client{Binary: "true"}, time.Minute, 1)
	m.width, m.height = 140, 30

	m.actionMenu, m.actionHostIndex = true, 99
	updated, cmd := m.handleActionMenuKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if updated.(Model).actionMenu || cmd != nil {
		t.Fatal("menu with stale host index must close without a command")
	}
	m.actionMenu, m.actionHostIndex = true, 0
	updated, cmd = m.handleActionMenuKey(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	if !updated.(Model).actionMenu || cmd != nil {
		t.Fatal("unknown menu key must be ignored")
	}
	if _, cmd := m.executeHostAction(-1, actionFullTerminal); cmd != nil {
		t.Fatal("negative host index scheduled an action")
	}
	if _, cmd := m.executeHostAction(2, actionFullTerminal); cmd != nil {
		t.Fatal("past-end host index scheduled an action")
	}

	invalid := m
	invalid.hosts = []inventory.Host{{ID: "one:bad", Alias: "-unsafe"}}
	for _, action := range []hostActionKind{actionFullTerminal, actionPreviewTerminal, actionGitCheck} {
		updated, cmd := invalid.executeHostAction(0, action)
		if cmd != nil || updated.(Model).message == "" {
			t.Fatalf("unsafe alias action %v did not fail closed", action)
		}
	}

	m.polling[host.ID] = true
	updated, cmd = m.executeHostAction(0, actionRefreshHost)
	if cmd != nil || !strings.Contains(updated.(Model).message, "already running") {
		t.Fatal("duplicate refresh was not rejected")
	}
	delete(m.polling, host.ID)
	updated, cmd = m.executeHostAction(0, actionEditHost)
	if cmd != nil || !strings.Contains(updated.(Model).message, "not configured") {
		t.Fatal("edit without reload was not rejected")
	}
	updated, cmd = m.executeHostAction(0, actionRepairHostKey)
	if cmd != nil || !strings.Contains(updated.(Model).message, "No host-key error") {
		t.Fatal("repair without host-key result was not rejected")
	}
	m.actionMenu = false
	updated, cmd = m.executeHostAction(0, hostActionKind(999))
	if cmd != nil || updated.(Model).actionMenu {
		t.Fatal("unknown action must be a no-op")
	}
}

func TestManualRefreshRespectsProbeConcurrencyLimit(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "one:alpha", Alias: "alpha", Probe: true}},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	m.active = 2
	updated, cmd := m.executeHostAction(0, actionRefreshHost)
	got := updated.(Model)
	if cmd != nil || got.active != 2 || len(got.queue) != 1 || !got.polling["one:alpha"] {
		t.Fatalf("queued refresh = cmd:%v active:%d queue:%v polling:%v", cmd != nil, got.active, got.queue, got.polling)
	}
}

func TestExecuteHostActionsBuildsContextCommands(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Probe: true}
	reload := func() ([]inventory.Host, []inventory.SourceSummary, error) {
		return []inventory.Host{host}, nil, nil
	}
	m := New(
		[]inventory.Host{host},
		nil,
		openssh.Client{Binary: "true"},
		time.Minute,
		2,
		WithHostEditor(t.TempDir(), "true", reload),
	)
	m.width, m.height = 120, 24

	if _, cmd := m.executeHostAction(0, actionFullTerminal); cmd == nil {
		t.Fatal("full terminal action did not build a command")
	}
	m.hostKeyPrompt[host.ID] = true
	if _, cmd := m.executeHostAction(0, actionFullTerminal); cmd == nil {
		t.Fatal("replacement-key terminal action did not build a command")
	}
	delete(m.hostKeyPrompt, host.ID)
	if _, cmd := m.executeHostAction(0, actionGitCheck); cmd == nil {
		t.Fatal("Git action did not build a command")
	}

	updated, startCmd := m.executeHostAction(0, actionPreviewTerminal)
	preview := updated.(Model)
	if !preview.embeddedStarting || startCmd == nil {
		t.Fatal("Preview action did not schedule a terminal")
	}
	started := startCmd().(embeddedStartedMsg)
	if started.err != nil || started.session == nil {
		t.Fatalf("start Preview terminal: %v", started.err)
	}
	_ = started.session.Close()

	narrow := m
	narrow.width = 50
	updated, cmd := narrow.executeHostAction(0, actionPreviewTerminal)
	if cmd != nil || !strings.Contains(updated.(Model).message, "at least 62") {
		t.Fatal("narrow Preview action was not rejected")
	}

	updated, cmd = m.executeHostAction(0, actionRefreshHost)
	if cmd == nil || updated.(Model).active != 1 {
		t.Fatal("immediate refresh action was not scheduled")
	}
	updated, cmd = m.executeHostAction(0, actionEditHost)
	if cmd == nil || !updated.(Model).editing {
		t.Fatal("overlay edit action was not scheduled")
	}
	m.results[host.ID] = probe.Result{Status: probe.StatusHostKey}
	updated, cmd = m.executeHostAction(0, actionRepairHostKey)
	if cmd == nil || !updated.(Model).hostKeyBusy {
		t.Fatal("host-key inspection action was not scheduled")
	}
}

func TestPreviewTerminalDimensionsFollowResponsiveLayout(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 150, 40
	_, _, right := widePaneWidths(m.width, m.sourcesWidthPct, m.previewWidthPct)
	width, height, ok := m.previewTerminalDimensions()
	if !ok || width != right || height != 36 {
		t.Fatalf("wide preview dimensions = %dx%d, ok=%v, want %dx36", width, height, ok, right)
	}
	m.embeddedStarting = true
	expandedWidth, _, ok := m.previewTerminalDimensions()
	if !ok || expandedWidth <= width {
		t.Fatalf("expanded preview width = %d, normal = %d", expandedWidth, width)
	}
	m.embeddedStarting = false
	restoredWidth, _, _ := m.previewTerminalDimensions()
	if restoredWidth != width {
		t.Fatalf("restored preview width = %d, want %d", restoredWidth, width)
	}
	m.width = 80
	width, height, ok = m.previewTerminalDimensions()
	if !ok || width != 22 || height != 36 {
		t.Fatalf("medium preview dimensions = %dx%d, ok=%v", width, height, ok)
	}
	m.focus = focusSources
	if _, _, ok := m.previewTerminalDimensions(); ok {
		t.Fatal("Preview terminal must be unavailable while the medium layout shows Sources")
	}
	m.width = 50
	if _, _, ok := m.previewTerminalDimensions(); ok {
		t.Fatal("Preview terminal must be unavailable in the narrow layout")
	}
}

func TestTerminalTabsOpenSwitchRenderAndKeepFleet(t *testing.T) {
	hosts := []inventory.Host{
		{ID: "one:alpha", Alias: "alpha", SourceName: "one"},
		{ID: "one:beta", Alias: "beta", SourceName: "one"},
	}
	m := New(hosts, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 120, 24

	opened, start := m.openTerminalTab(exec.Command("sh", "-c", "printf 'alpha-ready\\r\\n'; IFS= read -r input; printf 'paste:%s\\r\\n' \"$input\"; sleep 30"), 0, "SSH · alpha", false)
	m = opened.(Model)
	if start == nil || m.activeTab != 1 || len(m.tabs) != 1 || m.tabs[0].state != terminalTabStarting {
		t.Fatalf("first tab = active:%d tabs:%#v cmd:%v", m.activeTab, m.tabs, start != nil)
	}
	started := start().(terminalTabStartedMsg)
	if started.err != nil {
		t.Fatal(started.err)
	}
	updated, readAlpha := m.Update(started)
	m = updated.(Model)
	defer func() {
		if m.tabs[0].session != nil {
			_ = m.tabs[0].session.Close()
		}
	}()
	if m.tabs[0].state != terminalTabRunning || readAlpha == nil {
		t.Fatalf("started tab = %#v read:%v", m.tabs[0], readAlpha != nil)
	}
	output := readAlpha().(terminalTabOutputMsg)
	updated, readAlpha = m.Update(output)
	m = updated.(Model)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "1:Fleet") || !strings.Contains(view, "2:SSH · alpha") || !strings.Contains(view, "alpha-ready") {
		t.Fatalf("terminal tab view:\n%s", view)
	}
	updated, qCmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}))
	m = updated.(Model)
	if qCmd != nil || m.activeTab != 1 || m.tabs[0].state != terminalTabRunning {
		t.Fatalf("q escaped terminal tab: cmd=%v active=%d state=%v", qCmd != nil, m.activeTab, m.tabs[0].state)
	}
	updated, _ = m.Update(tea.PasteMsg{Content: "clipboard-line\n"})
	m = updated.(Model)
	for attempt := 0; attempt < 4 && !strings.Contains(ansi.Strip(m.View().Content), "paste:qclipboard-line"); attempt++ {
		if readAlpha == nil {
			t.Fatal("terminal read command disappeared during paste")
		}
		pasted := readAlpha().(terminalTabOutputMsg)
		updated, readAlpha = m.Update(pasted)
		m = updated.(Model)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "paste:qclipboard-line") {
		t.Fatalf("bracketed paste did not reach terminal PTY after echo:\n%s", view)
	}

	m = updateWithKey(t, m, tea.Key{Code: 'g', Mod: tea.ModCtrl})
	if m.activeTab != 0 || !m.tabSelectMode || !strings.Contains(ansi.Strip(m.View().Content), "TAB SELECT  1…2 choose") {
		t.Fatalf("Ctrl+G did not enter Fleet tab selection: active=%d mode=%v\n%s", m.activeTab, m.tabSelectMode, ansi.Strip(m.View().Content))
	}
	m = updateWithKey(t, m, tea.Key{Text: "9", Code: '9'})
	if m.activeTab != 0 || !m.tabSelectMode || !strings.Contains(m.message, "Tab 9 is not open") {
		t.Fatalf("invalid tab slot escaped selection mode: active=%d mode=%v message=%q", m.activeTab, m.tabSelectMode, m.message)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEsc})
	if m.activeTab != 0 || m.tabSelectMode {
		t.Fatalf("Esc did not cancel tab selection: active=%d mode=%v", m.activeTab, m.tabSelectMode)
	}
	m = updateWithKey(t, m, tea.Key{Text: "2", Code: '2'})
	if m.activeTab != 0 {
		t.Fatalf("plain digit switched tabs outside selection mode: active=%d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: 'n', Mod: tea.ModCtrl})
	if m.activeTab != 1 {
		t.Fatalf("Ctrl+N active tab = %d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: 'g', Mod: tea.ModCtrl})
	m = updateWithKey(t, m, tea.Key{Text: "1", Code: '1'})
	if m.activeTab != 0 {
		t.Fatalf("Ctrl+G then 1 active tab = %d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: 'g', Mod: tea.ModCtrl})
	m = updateWithKey(t, m, tea.Key{Text: "2", Code: '2'})
	if m.activeTab != 1 || m.tabSelectMode {
		t.Fatalf("Ctrl+G then 2 active tab = %d mode=%v", m.activeTab, m.tabSelectMode)
	}
	m = updateWithKey(t, m, tea.Key{Code: 'p', Mod: tea.ModCtrl})
	if m.activeTab != 0 {
		t.Fatalf("Ctrl+P active tab = %d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: '2', Mod: tea.ModCtrl})
	if m.activeTab != 1 {
		t.Fatalf("Ctrl+2 active tab = %d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: '1', Mod: tea.ModCtrl})
	if m.activeTab != 0 {
		t.Fatalf("Ctrl+1 active tab = %d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: '2', Mod: tea.ModAlt})
	if m.activeTab != 1 {
		t.Fatalf("Alt+2 active tab = %d", m.activeTab)
	}
	m = updateWithKey(t, m, tea.Key{Code: '1', Mod: tea.ModAlt})
	if m.activeTab != 0 {
		t.Fatalf("Alt+1 active tab = %d", m.activeTab)
	}
}

func TestTerminalTabMouseWheelUsesBoundedLocalScrollback(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2, WithTerminalConfig(config.TerminalConfig{ScrollbackLines: 7}))
	m.width, m.height = 80, 10
	opened, start := m.openTerminalTab(exec.Command("sh", "-c", "sleep 30"), -1, "Local shell", false)
	m = opened.(Model)
	started := start().(terminalTabStartedMsg)
	if started.err != nil {
		t.Fatal(started.err)
	}
	defer func() { _ = started.session.Close() }()
	updated, _ := m.Update(started)
	m = updated.(Model)
	for i := 1; i <= 30; i++ {
		_, _ = m.tabs[0].session.Terminal.Write([]byte(fmt.Sprintf("line-%02d\r\n", i)))
	}
	if got := m.tabs[0].session.Terminal.Scrollback().MaxLines(); got != 7 {
		t.Fatalf("terminal scrollback limit = %d, want 7", got)
	}
	if got := m.tabs[0].session.Terminal.ScrollbackLen(); got != 7 {
		t.Fatalf("retained scrollback = %d, want 7", got)
	}
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("terminal mouse mode = %v", got)
	}

	updated, cmd := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = updated.(Model)
	if cmd != nil || m.tabs[0].scrollOffset != terminalWheelLines {
		t.Fatalf("wheel up = offset:%d cmd:%v", m.tabs[0].scrollOffset, cmd != nil)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "SCROLL 3/7") || !strings.Contains(view, "line-") {
		t.Fatalf("scrolled terminal view:\n%s", view)
	}

	updated, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = updated.(Model)
	if m.tabs[0].scrollOffset != 0 || strings.Contains(ansi.Strip(m.View().Content), "SCROLL ") {
		t.Fatalf("wheel down did not return live: offset=%d\n%s", m.tabs[0].scrollOffset, ansi.Strip(m.View().Content))
	}
}

func TestTerminalTabCloseRequiresConfirmationAndStopsPTY(t *testing.T) {
	m := New([]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}}, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 100, 20
	opened, start := m.openTerminalTab(exec.Command("sh", "-c", "sleep 30"), 0, "SSH · alpha", false)
	m = opened.(Model)
	started := start().(terminalTabStartedMsg)
	if started.err != nil {
		t.Fatal(started.err)
	}
	updated, _ := m.Update(started)
	m = updated.(Model)

	first, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: ']', Mod: tea.ModCtrl}))
	m = first.(Model)
	if cmd != nil || !m.tabs[0].closePrompt || !strings.Contains(ansi.Strip(m.View().Content), "Live foreground process") {
		t.Fatalf("first close = prompt:%v cmd:%v\n%s", m.tabs[0].closePrompt, cmd != nil, ansi.Strip(m.View().Content))
	}
	second, closeCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: ']', Mod: tea.ModCtrl}))
	m = second.(Model)
	if closeCmd == nil || m.tabs[0].state != terminalTabClosing {
		t.Fatalf("confirmed close = state:%v cmd:%v", m.tabs[0].state, closeCmd != nil)
	}
	closed := closeCmd().(terminalTabClosedMsg)
	finished, _ := m.Update(closed)
	m = finished.(Model)
	if len(m.tabs) != 0 || m.activeTab != 0 {
		t.Fatalf("closed tabs = %d active=%d", len(m.tabs), m.activeTab)
	}
}

func TestCtrlDClosesTerminalTabImmediatelyAndReturnsFleet(t *testing.T) {
	m := New([]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}}, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 100, 20
	opened, start := m.openTerminalTab(exec.Command("sh", "-c", "trap '' HUP; sleep 30"), 0, "SSH · alpha", false)
	m = opened.(Model)
	started := start().(terminalTabStartedMsg)
	if started.err != nil {
		t.Fatal(started.err)
	}
	updated, _ := m.Update(started)
	m = updated.(Model)

	closed, closeCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
	m = closed.(Model)
	if closeCmd == nil || m.activeTab != 0 || len(m.tabs) != 0 {
		t.Fatalf("Ctrl+D close: cmd=%v active=%d tabs=%d", closeCmd != nil, m.activeTab, len(m.tabs))
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "HOSTS") {
		t.Fatalf("Ctrl+D did not return Fleet immediately:\n%s", view)
	}
	if m.message != "Terminal tab closed" {
		t.Fatalf("Ctrl+D status = %q", m.message)
	}
	message := closeCmd()
	if _, ok := message.(terminalTabClosedMsg); !ok {
		t.Fatalf("Ctrl+D close message = %T", message)
	}
}

func TestCtrlDDuringTerminalStartupCleansLatePTYWithoutRestoringTab(t *testing.T) {
	m := New([]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}}, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 100, 20
	opened, start := m.openTerminalTab(exec.Command("sh", "-c", "trap '' HUP; sleep 30"), 0, "SSH · alpha", false)
	m = opened.(Model)
	if start == nil || m.tabs[0].state != terminalTabStarting {
		t.Fatalf("starting tab = %#v cmd=%v", m.tabs, start != nil)
	}

	closed, closeCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
	m = closed.(Model)
	if closeCmd != nil || m.activeTab != 0 || len(m.tabs) != 0 {
		t.Fatalf("Ctrl+D during start: cmd=%v active=%d tabs=%d", closeCmd != nil, m.activeTab, len(m.tabs))
	}

	lateStarted := start().(terminalTabStartedMsg)
	if lateStarted.err != nil {
		t.Fatal(lateStarted.err)
	}
	updated, cleanupCmd := m.Update(lateStarted)
	m = updated.(Model)
	if cleanupCmd == nil || m.activeTab != 0 || len(m.tabs) != 0 {
		t.Fatalf("late start cleanup: cmd=%v active=%d tabs=%d", cleanupCmd != nil, m.activeTab, len(m.tabs))
	}
	if message := cleanupCmd(); message == nil {
		t.Fatal("late-started PTY did not produce a close result")
	}
}

func TestCtrlDRemovesCompletedAndFailedTabsWithoutProcessCommand(t *testing.T) {
	for _, state := range []terminalTabState{terminalTabExited, terminalTabFailed} {
		t.Run(fmt.Sprintf("state-%d", state), func(t *testing.T) {
			m := New(nil, nil, openssh.Client{}, time.Minute, 2)
			m.width, m.height = 100, 20
			m.tabs = []terminalTab{{id: 1, label: "done", state: state, screen: "final"}}
			m.activeTab = 1
			updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
			m = updated.(Model)
			if cmd != nil || m.activeTab != 0 || len(m.tabs) != 0 || m.message != "Terminal tab closed" {
				t.Fatalf("completed Ctrl+D: cmd=%v active=%d tabs=%d message=%q", cmd != nil, m.activeTab, len(m.tabs), m.message)
			}
		})
	}
}

func TestCtrlDClosesFinishingAndClosingPTYStates(t *testing.T) {
	for _, state := range []terminalTabState{terminalTabFinishing, terminalTabClosing} {
		t.Run(fmt.Sprintf("state-%d", state), func(t *testing.T) {
			m := New(nil, nil, openssh.Client{}, time.Minute, 2)
			m.width, m.height = 100, 20
			opened, start := m.openTerminalTab(exec.Command("sh", "-c", "trap '' HUP; sleep 30"), -1, "SSH · alpha", false)
			m = opened.(Model)
			started := start().(terminalTabStartedMsg)
			if started.err != nil {
				t.Fatal(started.err)
			}
			updated, _ := m.Update(started)
			m = updated.(Model)
			m.tabs[0].state = state

			closed, closeCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
			m = closed.(Model)
			if closeCmd == nil || m.activeTab != 0 || len(m.tabs) != 0 {
				t.Fatalf("Ctrl+D: cmd=%v active=%d tabs=%d", closeCmd != nil, m.activeTab, len(m.tabs))
			}
			message := closeCmd()
			if _, ok := message.(terminalTabClosedMsg); !ok {
				t.Fatalf("close message = %T", message)
			}
		})
	}
}

func TestTerminalTabCompletionReturnsFleetAndKeepsFinalScreenAndSessionTail(t *testing.T) {
	m := New([]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}}, nil, openssh.Client{}, time.Minute, 2)
	m.width, m.height = 100, 20
	opened, start := m.openTerminalTab(exec.Command("sh", "-c", "printf 'final-screen\\r\\n'"), 0, "SSH · alpha", false)
	m = opened.(Model)
	started := start().(terminalTabStartedMsg)
	if started.err != nil {
		t.Fatal(started.err)
	}
	updated, read := m.Update(started)
	m = updated.(Model)
	for attempt := 0; attempt < 8 && m.tabs[0].state != terminalTabFinishing; attempt++ {
		message := read().(terminalTabOutputMsg)
		updated, read = m.Update(message)
		m = updated.(Model)
	}
	if m.tabs[0].state != terminalTabFinishing || read == nil {
		t.Fatalf("tab did not reach finishing: %#v", m.tabs[0])
	}
	finished := read().(terminalTabFinishedMsg)
	updated, _ = m.Update(finished)
	m = updated.(Model)
	if m.tabs[0].state != terminalTabExited || m.tabs[0].session != nil {
		t.Fatalf("completed tab = %#v", m.tabs[0])
	}
	if m.activeTab != 0 {
		t.Fatalf("completed active tab did not return to Fleet: active=%d", m.activeTab)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "HOSTS") || !strings.Contains(view, "2:SSH · alpha ✓") {
		t.Fatalf("Fleet view after terminal completion:\n%s", view)
	}
	if !strings.Contains(m.message, "Terminal tab finished") {
		t.Fatalf("completion status = %q", m.message)
	}
	m = updateWithKey(t, m, tea.Key{Code: '2', Mod: tea.ModAlt})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "final-screen") || !strings.Contains(view, "2:SSH · alpha ✓") {
		t.Fatalf("completed tab did not retain final screen:\n%s", view)
	}
	if tail := strings.Join(m.sessionTail["one:alpha"], "\n"); !strings.Contains(tail, "final-screen") {
		t.Fatalf("session tail = %q", tail)
	}
}

func TestBackgroundTerminalTabCompletionDoesNotStealActiveTab(t *testing.T) {
	first := &session.Embedded{}
	second := &session.Embedded{}
	m := New(nil, nil, openssh.Client{}, time.Minute, 2)
	m.tabs = []terminalTab{
		{id: 1, label: "first", state: terminalTabRunning, session: first},
		{id: 2, label: "second", state: terminalTabRunning, session: second},
	}
	m.activeTab = 2
	updated, cmd := m.applyTerminalTabFinished(terminalTabFinishedMsg{id: 1, session: first, screen: "done"})
	m = updated.(Model)
	if cmd != nil || m.activeTab != 2 || m.tabs[0].state != terminalTabExited || m.tabs[1].state != terminalTabRunning {
		t.Fatalf("background completion: cmd=%v active=%d tabs=%#v", cmd != nil, m.activeTab, m.tabs)
	}
}

func TestTerminalTabStripKeepsActiveLateTabVisible(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 2)
	m.width = 48
	for index := 1; index <= 10; index++ {
		m.tabs = append(m.tabs, terminalTab{id: uint64(index), label: fmt.Sprintf("long-host-%02d", index), state: terminalTabRunning})
	}
	m.activeTab = 10
	strip := ansi.Strip(m.renderTabStrip(m.width))
	if !strings.Contains(strip, "1:Fleet") || !strings.Contains(strip, "› 11:long-host-10") {
		t.Fatalf("active late tab is hidden: %q", strip)
	}
}

func TestEmbeddedPreviewSessionFlowsThroughUpdateViewAndCapture(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "one:alpha", Alias: "alpha", Name: "Alpha", SourceName: "one", Probe: false}},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	m.width, m.height = 120, 24
	width, height, ok := m.previewTerminalDimensions()
	if !ok {
		t.Fatal("expected Preview terminal dimensions")
	}
	start := startEmbeddedCmd(
		exec.Command("sh", "-c", `IFS= read -r line; printf 'embedded:%s\r\n' "$line"`),
		0,
		"alpha",
		width,
		height,
	)
	started := start().(embeddedStartedMsg)
	updated, next := m.Update(started)
	m = updated.(Model)
	if m.embedded == nil || next == nil || !strings.Contains(ansi.Strip(m.View().Content), "PREVIEW TERMINAL") {
		t.Fatal("embedded session did not become visible")
	}

	updated, _ = m.Update(tea.PasteMsg{Content: "hi\n"})
	m = updated.(Model)
	seenOutput := false
	for attempt := 0; attempt < 8 && m.embedded != nil; attempt++ {
		if next == nil {
			t.Fatal("embedded session stopped scheduling reads")
		}
		updated, next = m.Update(next())
		m = updated.(Model)
		if strings.Contains(ansi.Strip(m.View().Content), "embedded:hi") {
			seenOutput = true
		}
	}
	if m.embedded != nil || !seenOutput {
		t.Fatalf("embedded session completion = active:%v output:%v", m.embedded != nil, seenOutput)
	}
	if tail := strings.Join(m.sessionTail["one:alpha"], "\n"); !strings.Contains(tail, "embedded:hi") {
		t.Fatalf("embedded session tail = %q", tail)
	}
}

func TestEmbeddedPreviewCanBeClosedLocally(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	m.width, m.height = 120, 24
	width, height, _ := m.previewTerminalDimensions()
	started := startEmbeddedCmd(exec.Command("sh", "-c", "sleep 30"), 0, "alpha", width, height)().(embeddedStartedMsg)
	updated, _ := m.Update(started)
	m = updated.(Model)
	updated, closeCmd := m.Update(tea.KeyPressMsg(tea.Key{Code: ']', Mod: tea.ModCtrl}))
	m = updated.(Model)
	if !m.embeddedClosing || closeCmd == nil {
		t.Fatal("Ctrl+] did not start local embedded-session close")
	}
	updated, _ = m.Update(closeCmd())
	m = updated.(Model)
	if m.embedded != nil || !strings.Contains(m.message, "closed") {
		t.Fatalf("embedded close state = active:%v message:%q", m.embedded != nil, m.message)
	}
}

func TestKeyboardNavigationAndFiltering(t *testing.T) {
	m := New(
		[]inventory.Host{
			{ID: "one:alpha", Alias: "alpha", SourceName: "one"},
			{ID: "one:beta", Alias: "beta", SourceName: "one"},
			{ID: "two:gamma", Alias: "gamma", SourceName: "two"},
		},
		[]inventory.SourceSummary{{Name: "one", Hosts: 2}, {Name: "two", Hosts: 1}},
		openssh.Client{},
		time.Minute,
		2,
	)

	m = updateWithKey(t, m, tea.Key{Text: "h", Code: 'h'})
	if m.focus != focusSources {
		t.Fatal("h must focus Sources")
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyDown})
	if m.source != 1 {
		t.Fatalf("source = %d, want 1", m.source)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnd})
	if m.source != m.lastNavigationIndex() {
		t.Fatalf("source = %d, want last", m.source)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyHome})
	m = updateWithKey(t, m, tea.Key{Text: "l", Code: 'l'})
	if m.focus != focusHosts || m.source != 0 {
		t.Fatalf("focus/source = %v/%d", m.focus, m.source)
	}
	m = updateWithKey(t, m, tea.Key{Text: "G", Code: 'G'})
	if m.selected != 2 {
		t.Fatalf("selected = %d, want last", m.selected)
	}
	m = updateWithKey(t, m, tea.Key{Text: "g", Code: 'g'})
	if m.selected != 0 {
		t.Fatalf("selected = %d, want first", m.selected)
	}

	m = updateWithKey(t, m, tea.Key{Text: "/", Code: '/'})
	m = updateWithKey(t, m, tea.Key{Text: "\x1b[31m", Code: 'x'})
	if m.filter != "" {
		t.Fatalf("terminal control sequence leaked into filter: %q", m.filter)
	}
	for _, r := range "beta" {
		m = updateWithKey(t, m, tea.Key{Text: string(r), Code: r})
	}
	if !m.filtering || m.filter != "beta" || len(m.visibleIndices()) != 1 {
		t.Fatalf("filter state = active:%v query:%q visible:%v", m.filtering, m.filter, m.visibleIndices())
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyBackspace})
	if m.filter != "bet" {
		t.Fatalf("filter after backspace = %q", m.filter)
	}
	m = updateWithKey(t, m, tea.Key{Code: 'u', Mod: tea.ModCtrl})
	if m.filter != "" {
		t.Fatalf("filter after ctrl+u = %q", m.filter)
	}
	m = updateWithKey(t, m, tea.Key{Text: "gamma", Code: 'g'})
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	if m.filtering || m.filter != "gamma" || len(m.visibleIndices()) != 1 {
		t.Fatalf("applied filter = active:%v query:%q", m.filtering, m.filter)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEsc})
	if m.filter != "" || len(m.visibleIndices()) != 3 {
		t.Fatalf("Esc did not clear filter: %q", m.filter)
	}
}

func TestGlobalHostSearchCrossesSourcesAndRestoresContext(t *testing.T) {
	m := New(
		[]inventory.Host{
			{ID: "one:alpha", Alias: "alpha", Name: "Alpha API", SourceName: "one"},
			{ID: "one:beta", Alias: "beta", Name: "Beta worker", SourceName: "one"},
			{ID: "two:gamma", Alias: "gamma", Name: "Gamma Stand", SourceName: "two", Hostname: "192.0.2.20", User: "operator", Port: 2222, Tags: []string{"perf", "stand"}, Groups: []string{"Pregel"}},
		},
		[]inventory.SourceSummary{{Name: "one", Hosts: 2}, {Name: "two", Hosts: 1}},
		openssh.Client{},
		time.Minute,
		2,
	)
	m.source = 1
	m.selected = 1

	m = updateWithKey(t, m, tea.Key{Text: "/", Code: '/'})
	if !m.filtering || m.source != 0 || !m.searchReturnSet || m.searchReturnHostID != "one:beta" {
		t.Fatalf("search start = filtering:%v source:%d return:%v/%q", m.filtering, m.source, m.searchReturnSet, m.searchReturnHostID)
	}
	for _, r := range "operator perf 192.0.2.20" {
		m = updateWithKey(t, m, tea.Key{Text: string(r), Code: r})
	}
	visible := m.visibleIndices()
	if len(visible) != 1 || m.hosts[visible[0]].ID != "two:gamma" {
		t.Fatalf("global target search visible = %#v", visible)
	}
	view := ansi.Strip(m.renderHosts(120, 12))
	if !strings.Contains(view, "source: two") {
		t.Fatalf("global result does not expose source origin:\n%s", view)
	}

	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEsc})
	visible = m.visibleIndices()
	if m.filter != "" || m.filtering || m.source != 1 || m.selected != 1 || len(visible) != 2 || m.hosts[visible[m.selected]].ID != "one:beta" {
		t.Fatalf("restored search context = filter:%q active:%v source:%d selected:%d visible:%#v", m.filter, m.filtering, m.source, m.selected, visible)
	}
}

func TestGlobalHostSearchUsesResolvedTargetsGroupsAndContainerMetadata(t *testing.T) {
	m := New(
		[]inventory.Host{
			{ID: "user:ssh-alias", Alias: "ssh-alias", SourceName: "user", Groups: []string{"Production"}},
			{ID: "container:docker:abcdef", Alias: "web-box", SourceName: "containers · docker", Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerID: "abcdef", ContainerImage: "registry.example/web:v2", ContainerStatus: "Up 2 hours"},
		},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	m.effective["user:ssh-alias"] = openssh.Effective{Hostname: "srv.internal.example", User: "deploy", Port: "2202", ProxyJump: "bastion"}

	for query, wantID := range map[string]string{
		"deploy bastion 2202":        "user:ssh-alias",
		"production internal":        "user:ssh-alias",
		"docker registry.example v2": "container:docker:abcdef",
	} {
		m.filter = query
		visible := m.visibleIndices()
		if len(visible) != 1 || m.hosts[visible[0]].ID != wantID {
			t.Errorf("query %q visible = %#v, want %s", query, visible, wantID)
		}
	}

	m.filter = "нет-такого-хоста"
	if visible := m.visibleIndices(); len(visible) != 0 {
		t.Fatalf("no-match search visible = %#v", visible)
	}
	if view := ansi.Strip(m.renderHosts(100, 10)); !strings.Contains(view, "No hosts match global search") {
		t.Fatalf("no-match view = %q", view)
	}
}

func TestGlobalHostSearchRestoresGroupByNameAfterSourceRefresh(t *testing.T) {
	host := inventory.Host{ID: "one:alpha", Alias: "alpha", SourceName: "one", Groups: []string{"stands"}}
	m := New(
		[]inventory.Host{host, {ID: "two:needle", Alias: "needle", SourceName: "two"}},
		[]inventory.SourceSummary{{Name: "one", Hosts: 1}, {Name: "two", Hosts: 1}},
		openssh.Client{},
		time.Minute,
		2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "stands", Members: []string{"one:alpha"}}}, nil),
	)
	m.source = len(m.sources) + 1
	m.selected = 0
	m.beginGlobalSearch()
	m.filter = "needle"

	// Dynamic discovery may insert a Source while search is active. Restoration
	// must use the group name, not the obsolete numeric row index.
	m.sources = append([]inventory.SourceSummary{{Name: "dynamic", Hosts: 0}}, m.sources...)
	m.restoreSearchContext()
	visible := m.visibleIndices()
	if m.selectedGroup() != "stands" || len(visible) != 1 || m.hosts[visible[0]].ID != host.ID {
		t.Fatalf("restored group = %q, visible = %#v", m.selectedGroup(), visible)
	}
}

func TestResponsivePaneLayouts(t *testing.T) {
	m := New(
		[]inventory.Host{{ID: "one:alpha", Alias: "alpha", SourceName: "one"}},
		[]inventory.SourceSummary{{Name: "one", Hosts: 1}},
		openssh.Client{},
		time.Minute,
		2,
	)
	m.height = 20

	assertPaneTitles(t, m, 150, []string{"SOURCES", "HOSTS", "PREVIEW"}, nil)
	assertPaneTitles(t, m, 80, []string{"HOSTS", "PREVIEW"}, []string{"SOURCES"})
	assertPaneTitles(t, m, 50, []string{"HOSTS"}, []string{"SOURCES", "PREVIEW"})
	m.focus = focusSources
	assertPaneTitles(t, m, 80, []string{"SOURCES", "HOSTS"}, []string{"PREVIEW"})
	assertPaneTitles(t, m, 50, []string{"SOURCES"}, []string{"HOSTS", "PREVIEW"})
}

func TestPollSchedulerStoresResultsByStableID(t *testing.T) {
	m := New(
		[]inventory.Host{
			{ID: "one:a", Alias: "a", Probe: true},
			{ID: "one:b", Alias: "b", Probe: false},
			{ID: "one:c", Alias: "c", Probe: true},
			{ID: "one:d", Alias: "d", Probe: true},
		},
		nil,
		openssh.Client{},
		time.Minute,
		2,
	)
	updated, cmd := m.Update(startPollMsg{})
	got := updated.(Model)
	if cmd == nil || got.active != 2 || len(got.queue) != 1 || !got.polling["one:d"] {
		t.Fatalf("scheduler = active:%d queue:%v polling:%v", got.active, got.queue, got.polling)
	}
	result := probe.Result{Status: probe.StatusOnline, CPUTotal: 100, CPUIdle: 80}
	updated, _ = got.Update(probeMsg{id: "one:c", result: result})
	got = updated.(Model)
	if _, ok := got.results["one:c"]; !ok || got.active != 2 || len(got.queue) != 0 {
		t.Fatalf("probe result/scheduler = results:%v active:%d queue:%v", got.results, got.active, got.queue)
	}
	updated, _ = got.Update(resolveMsg{id: "one:a", effective: openssh.Effective{Hostname: "a.example", User: "root"}})
	got = updated.(Model)
	if got.effective["one:a"].Summary() != "root@a.example" {
		t.Fatalf("effective = %#v", got.effective["one:a"])
	}
}

func updateWithKey(t *testing.T, m Model, key tea.Key) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg(key))
	return updated.(Model)
}

func assertPaneTitles(t *testing.T, m Model, width int, present, absent []string) {
	t.Helper()
	m.width = width
	content := ansi.Strip(m.View().Content)
	for _, title := range present {
		if !strings.Contains(content, title) {
			t.Fatalf("width %d: missing pane %q\n%s", width, title, content)
		}
	}
	for _, title := range absent {
		if strings.Contains(content, title) {
			t.Fatalf("width %d: unexpected pane %q\n%s", width, title, content)
		}
	}
}
