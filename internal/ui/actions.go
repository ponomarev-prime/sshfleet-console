package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/containers"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/localtarget"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
	"github.com/ponomarev-prime/sshfleet-console/internal/workspace"
)

type hostActionKind int

const (
	actionFullTerminal hostActionKind = iota
	actionPreviewTerminal
	actionGitCheck
	actionRefreshHost
	actionEditHost
	actionEditSource
	actionRepairHostKey
	actionWorkspace
	actionContainerLogs
	actionGroupMembership
)

type hostAction struct {
	kind        hostActionKind
	label       string
	description string
}

func (m Model) availableHostActions(index int) []hostAction {
	if index < 0 || index >= len(m.hosts) {
		return nil
	}
	host := m.hosts[index]
	previewAvailable := platform.Current().EmbeddedTerminalAvailable
	if host.TargetTransport() == inventory.TransportContainer {
		actions := make([]hostAction, 0, 4)
		if strings.EqualFold(host.ContainerState, "running") {
			description := "Shell policy " + valueOrDefault(host.ContainerShellPolicy, config.ContainerShellPolicyFirstAvailable) + ": " + strings.Join(host.ContainerShells, ", ")
			actions = append(actions, hostAction{actionFullTerminal, "Open container tab (default)", description})
			if _, _, ok := m.previewTerminalDimensions(); previewAvailable && ok {
				actions = append(actions, hostAction{actionPreviewTerminal, "Open container shell in Preview", "Keep the inventory visible while the resolved container shell gets a PTY"})
			}
		}
		actions = m.appendGroupMembershipAction(actions)
		return append(actions,
			hostAction{actionContainerLogs, "Follow container logs", "Show the latest 200 lines and continue following output"},
			hostAction{actionRefreshHost, "Refresh container", "Refresh runtime state now"},
		)
	}
	if host.TargetTransport() == inventory.TransportLocal {
		actions := []hostAction{{actionFullTerminal, "Open local shell tab (default)", "Keep Fleet available while the configured shell runs in an isolated PTY tab"}}
		if _, _, ok := m.previewTerminalDimensions(); previewAvailable && ok {
			actions = append(actions, hostAction{actionPreviewTerminal, "Open local shell in Preview", "Keep the inventory visible and run the configured local shell in the right pane"})
		}
		actions = m.appendGroupMembershipAction(actions)
		actions = append(actions, hostAction{actionRefreshHost, "Refresh local host", "Collect local host data immediately"})
		if m.reload != nil && host.ConfigPath != "" && host.ConfigPath != os.DevNull {
			actions = append(actions, hostAction{actionEditSource, "Edit trusted local config", host.ConfigPath})
		}
		return actions
	}
	result, hasResult := m.results[host.ID]
	if m.hostKeyPrompt[host.ID] {
		actions := []hostAction{
			{actionFullTerminal, "Verify host key in terminal tab", "Open an isolated PTY with StrictHostKeyChecking=ask"},
			{actionRefreshHost, "Refresh host", "Probe this host again after accepting the key"},
		}
		actions = m.appendGroupMembershipAction(actions)
		if m.reload != nil {
			actions = append(actions, hostAction{actionEditHost, "Edit host overlay", "Change the private SSH Fleet Console metadata layer"})
		}
		return actions
	}
	if hasResult && result.Status == probe.StatusGit {
		actions := []hostAction{{actionGitCheck, "Check Git access", "Run the standard non-shell ssh -T handshake"}, {actionRefreshHost, "Refresh host", "Probe this endpoint now"}}
		actions = m.appendGroupMembershipAction(actions)
		if m.reload != nil {
			actions = append(actions, hostAction{actionEditHost, "Edit host overlay", "Change the private SSH Fleet Console metadata layer"})
		}
		return actions
	}
	if hasResult && result.Status == probe.StatusHostKey {
		actions := []hostAction{{actionRepairHostKey, "Inspect host-key repair", "Verify fingerprint, then backup and remove the old entry"}, {actionFullTerminal, "Open terminal tab", "Show the native OpenSSH warning in an isolated PTY tab"}, {actionRefreshHost, "Refresh host", "Probe this host again"}}
		actions = m.appendGroupMembershipAction(actions)
		if m.reload != nil {
			actions = append(actions, hostAction{actionEditHost, "Edit host overlay", "Change the private SSH Fleet Console metadata layer"})
		}
		return actions
	}
	actions := []hostAction{{actionFullTerminal, "Open terminal tab (default)", "Keep Fleet available while OpenSSH runs in an isolated PTY tab"}}
	if _, _, ok := m.previewTerminalDimensions(); previewAvailable && ok {
		actions = append(actions, hostAction{actionPreviewTerminal, "Open terminal in Preview", "Keep the fleet visible and run SSH in the right pane"})
	}
	actions = m.appendGroupMembershipAction(actions)
	if m.client.WorkspaceBundle != "" {
		actions = append(actions, hostAction{
			actionWorkspace,
			"Open SSH Fleet workspace",
			"Temporary shell with lf, nvim, dtop and bat; removed on exit",
		})
	}
	actions = append(actions, hostAction{actionRefreshHost, "Refresh host", "Run one probe immediately"})
	if m.reload != nil {
		actions = append(actions, hostAction{actionEditHost, "Edit host overlay", "Change the private SSH Fleet Console metadata layer"})
		if host.ConfigPath != "" && host.ConfigPath != os.DevNull {
			actions = append(actions, hostAction{actionEditSource, "Edit source SSH config", host.ConfigPath})
		}
	}
	return actions
}

func (m Model) appendGroupMembershipAction(actions []hostAction) []hostAction {
	if len(m.groups) == 0 {
		return actions
	}
	return append(actions, hostAction{
		kind:        actionGroupMembership,
		label:       "Manage group membership",
		description: "Add or remove this host from private cross-source groups (shortcut: m)",
	})
}

func (m Model) handleActionMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	actions := m.availableHostActions(m.actionHostIndex)
	if len(actions) == 0 {
		m.actionMenu = false
		return m, nil
	}
	switch msg.String() {
	case "esc", "left", "h":
		m.actionMenu = false
		m.actionSelected = 0
		return m, nil
	case "up", "k":
		if m.actionSelected > 0 {
			m.actionSelected--
		}
		return m, nil
	case "down", "j":
		if m.actionSelected < len(actions)-1 {
			m.actionSelected++
		}
		return m, nil
	case "enter":
		action := actions[min(m.actionSelected, len(actions)-1)]
		m.actionMenu = false
		m.actionSelected = 0
		return m.executeHostAction(m.actionHostIndex, action.kind)
	case "q", "ctrl+c":
		m.actionMenu = false
		return m, nil
	}
	return m, nil
}

func (m Model) executeHostAction(index int, action hostActionKind) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.hosts) {
		return m, nil
	}
	host := m.hosts[index]
	switch action {
	case actionFullTerminal:
		if host.TargetTransport() == inventory.TransportLocal {
			cmd, err := localtarget.InteractiveCommand(host)
			if err != nil {
				m.message = err.Error()
				return m, nil
			}
			return m.openTerminalTab(cmd, index, "Local shell · "+host.Alias, false)
		}
		if host.TargetTransport() == inventory.TransportContainer {
			m.targetStarting = true
			m.message = "Finding an available container shell…"
			return m, prepareContainerCommand(host, index, false, false)
		}
		var cmd *exec.Cmd
		var err error
		if m.hostKeyPrompt[host.ID] {
			cmd, err = m.client.InteractiveCommandWithHostKeyPrompt(host)
		} else {
			cmd, err = m.client.InteractiveCommand(host)
		}
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		return m.openTerminalTab(cmd, index, "SSH · "+host.Alias, false)
	case actionPreviewTerminal:
		width, height, ok := m.previewTerminalDimensions()
		if !ok {
			m.message = "Preview terminal needs at least 62 columns"
			return m, nil
		}
		if host.TargetTransport() == inventory.TransportLocal {
			cmd, err := localtarget.InteractiveCommand(host)
			if err != nil {
				m.message = err.Error()
				return m, nil
			}
			m.embeddedStarting = true
			m.message = "Starting local shell in Preview…"
			return m, startEmbeddedCmd(cmd, index, "Local shell · "+host.Alias, width, height)
		}
		if host.TargetTransport() == inventory.TransportContainer {
			m.targetStarting = true
			m.message = "Finding an available container shell…"
			return m, prepareContainerCommand(host, index, true, false)
		}
		m.embeddedStarting = true
		cmd, err := m.client.InteractiveCommand(host)
		if err != nil {
			m.embeddedStarting = false
			m.message = err.Error()
			return m, nil
		}
		m.message = "Starting terminal in Preview…"
		return m, startEmbeddedCmd(cmd, index, host.Alias, width, height)
	case actionGroupMembership:
		return m.openMembershipGroupAt(index)
	case actionGitCheck:
		cmd, err := m.client.GitAccessCommand(host)
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		return m, gitSessionCmd(cmd, index, host.Alias)
	case actionRefreshHost:
		if host.TargetTransport() == inventory.TransportContainer && m.dynamicDiscover != nil {
			if m.dynamicBusy {
				m.message = "Container discovery is already running"
				return m, nil
			}
			m.dynamicBusy = true
			m.message = "Refreshing local containers…"
			discover := m.dynamicDiscover
			return m, func() tea.Msg {
				hosts, sources := discover()
				return dynamicSourcesMsg{hosts: hosts, sources: sources}
			}
		}
		if m.polling[host.ID] {
			m.message = "Host probe is already running"
			return m, nil
		}
		m.polling[host.ID] = true
		if m.active >= m.maxConcurrent {
			m.queue = append(m.queue, index)
			m.message = "Refresh queued for " + host.Alias
			return m, nil
		}
		m.active++
		m.message = "Refreshing " + host.Alias + "…"
		return m, m.probeCmd(index)
	case actionEditHost:
		if m.reload == nil {
			m.message = "Host overlay editor is not configured"
			return m, nil
		}
		path, err := config.EnsureHostOverride(m.overridesDir, host.SourceName, host.Alias)
		if err != nil {
			m.message = "Host overlay: " + err.Error()
			return m, nil
		}
		cmd, err := editorCommand(m.editor, path)
		if err != nil {
			m.message = "Host overlay: " + err.Error()
			return m, nil
		}
		m.editing = true
		m.queue = nil
		m.polling = make(map[string]bool)
		m.refreshAfter = m.active > 0
		m.message = "Editing overlay: " + path
		return m, editHostCmd(cmd, host.ID, path, m.reload)
	case actionEditSource:
		if m.reload == nil || host.ConfigPath == "" || host.ConfigPath == os.DevNull {
			m.message = "Selected host has no editable local SSH config"
			return m, nil
		}
		cmd, err := editorCommand(m.editor, host.ConfigPath)
		if err != nil {
			m.message = "Source config: " + err.Error()
			return m, nil
		}
		m.editing = true
		m.queue = nil
		m.polling = make(map[string]bool)
		m.refreshAfter = m.active > 0
		m.message = "Editing source config: " + host.ConfigPath
		return m, editSourceCmd(cmd, host.ID, host.ConfigPath, m.reload)
	case actionRepairHostKey:
		result, ok := m.results[host.ID]
		if !ok || result.Status != probe.StatusHostKey {
			m.message = "No host-key error for the selected host"
			return m, nil
		}
		m.hostKeyBusy = true
		m.message = "Inspecting known_hosts and saved fingerprint…"
		return m, m.inspectHostKeyCmd(index, result)
	case actionWorkspace:
		tool := workspace.ToolShell
		m.workspaceStarting = true
		m.message = "Validating and uploading bundled " + string(tool) + "…"
		return m, prepareWorkspaceCmd(m.client, host, index, tool)
	case actionContainerLogs:
		if host.TargetTransport() != inventory.TransportContainer {
			m.message = "Logs are available only for local containers"
			return m, nil
		}
		m.targetStarting = true
		m.message = "Preparing container logs…"
		return m, prepareContainerCommand(host, index, false, true)
	}
	return m, nil
}

func prepareContainerCommand(host inventory.Host, index int, preview, logs bool) tea.Cmd {
	return func() tea.Msg {
		if logs {
			cmd, err := containers.LogsCommand(host)
			return targetCommandPreparedMsg{index: index, preview: false, label: "Container logs · " + host.Alias, exitHint: "Ctrl+C", interruptOK: true, cmd: cmd, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cmd, shell, err := containers.InteractiveCommand(ctx, host)
		label := "Container shell · " + host.Alias
		if shell != "" {
			label += " · " + shell
		}
		return targetCommandPreparedMsg{index: index, preview: preview, label: label, shell: shell, cmd: cmd, err: err}
	}
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func prepareWorkspaceCmd(client openssh.Client, host inventory.Host, index int, tool workspace.Tool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		remotePath, err := client.PrepareWorkspace(ctx, host)
		return workspacePreparedMsg{index: index, tool: tool, remotePath: remotePath, err: err}
	}
}

func (m Model) handleEmbeddedKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.embedded == nil {
		return m, nil
	}
	if msg.String() == "ctrl+]" {
		m.embeddedClosing = true
		m.message = "Closing Preview terminal…"
		return m, closeEmbeddedCmd(m.embedded, m.embeddedIndex)
	}
	key := msg.Key()
	m.embedded.Terminal.SendKey(uv.KeyPressEvent(uv.Key{
		Text: key.Text, Mod: uv.KeyMod(key.Mod), Code: key.Code,
		ShiftedCode: key.ShiftedCode, BaseCode: key.BaseCode, IsRepeat: key.IsRepeat,
	}))
	return m, nil
}

type embeddedStartedMsg struct {
	session *session.Embedded
	index   int
	alias   string
	err     error
}
type embeddedOutputMsg struct {
	session *session.Embedded
	data    []byte
	err     error
}
type embeddedFinishedMsg struct {
	session *session.Embedded
	index   int
	lines   []string
	forced  bool
	err     error
}

func startEmbeddedCmd(cmd *exec.Cmd, index int, alias string, width, height int) tea.Cmd {
	return func() tea.Msg {
		embedded, err := session.StartEmbedded(cmd, width, height)
		return embeddedStartedMsg{session: embedded, index: index, alias: alias, err: err}
	}
}

func readEmbeddedCmd(embedded *session.Embedded) tea.Cmd {
	return func() tea.Msg {
		data, err := embedded.ReadChunk()
		return embeddedOutputMsg{session: embedded, data: data, err: err}
	}
}

func finishEmbeddedCmd(embedded *session.Embedded, index int) tea.Cmd {
	return func() tea.Msg {
		err := embedded.Finish()
		return embeddedFinishedMsg{session: embedded, index: index, lines: embedded.Capture.LastLines(12), err: err}
	}
}

func closeEmbeddedCmd(embedded *session.Embedded, index int) tea.Cmd {
	return func() tea.Msg {
		err := embedded.Close()
		return embeddedFinishedMsg{session: embedded, index: index, lines: embedded.Capture.LastLines(12), forced: true, err: err}
	}
}

func embeddedError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "signal: killed" {
		return ""
	}
	return fmt.Sprintf(": %s", text)
}
