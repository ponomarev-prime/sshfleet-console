package ui

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
)

type groupDialogMode uint8

const (
	groupDialogNone groupDialogMode = iota
	groupDialogCreate
	groupDialogRename
	groupDialogDelete
	groupDialogMembership
)

type groupDialogState struct {
	mode      groupDialogMode
	input     string
	original  string
	selected  int
	hostID    string
	hostLabel string
}

type groupChangedMsg struct {
	hosts       []inventory.Host
	sources     []inventory.SourceSummary
	groups      []config.HostGroup
	action      string
	path        string
	selectGroup string
	hostID      string
	navIndex    int
	err         error
}

func (m *Model) setGroupDefinitions(groups []config.HostGroup) {
	m.groupDefinitions = make([]config.HostGroup, len(groups))
	seen := make(map[string]struct{})
	for i, group := range groups {
		group.Members = append([]string(nil), group.Members...)
		group.Match = append([]string(nil), group.Match...)
		m.groupDefinitions[i] = group
		seen[group.Name] = struct{}{}
	}
	for _, host := range m.hosts {
		for _, group := range host.Groups {
			seen[group] = struct{}{}
		}
	}
	m.groups = m.groups[:0]
	for group := range seen {
		m.groups = append(m.groups, group)
	}
	sort.Strings(m.groups)
}

func (m Model) groupDefinition(name string) (config.HostGroup, bool) {
	for _, group := range m.groupDefinitions {
		if group.Name == name {
			return group, true
		}
	}
	return config.HostGroup{}, false
}

func (m Model) openCreateGroup() (tea.Model, tea.Cmd) {
	if m.groupReload == nil {
		m.message = "Group editing is unavailable; configure groups_dir and restart"
		return m, nil
	}
	m.groupDialog = groupDialogState{mode: groupDialogCreate}
	return m, nil
}

func (m Model) openRenameGroup() (tea.Model, tea.Cmd) {
	group := m.selectedGroup()
	if group == "" {
		m.message = "Select a group first"
		return m, nil
	}
	m.groupDialog = groupDialogState{mode: groupDialogRename, input: group, original: group}
	return m, nil
}

func (m Model) openDeleteGroup() (tea.Model, tea.Cmd) {
	group := m.selectedGroup()
	if group == "" {
		m.message = "Select a group first"
		return m, nil
	}
	m.groupDialog = groupDialogState{mode: groupDialogDelete, original: group}
	return m, nil
}

func (m Model) openMembershipGroup() (tea.Model, tea.Cmd) {
	index := m.selectedHostIndex()
	return m.openMembershipGroupAt(index)
}

func (m Model) openMembershipGroupAt(index int) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.hosts) {
		m.message = "Select a host first"
		return m, nil
	}
	if len(m.groups) == 0 {
		m.message = "No groups yet; focus SOURCES and press n"
		return m, nil
	}
	host := m.hosts[index]
	m.groupDialog = groupDialogState{mode: groupDialogMembership, hostID: host.ID, hostLabel: host.DisplayName()}
	return m, nil
}

func (m Model) handleGroupDialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	dialog := m.groupDialog
	if msg.String() == "ctrl+c" {
		return m.requestQuit()
	}
	if msg.String() == "esc" {
		m.groupDialog = groupDialogState{}
		m.message = "Group action cancelled"
		return m, nil
	}
	switch dialog.mode {
	case groupDialogCreate, groupDialogRename:
		switch msg.String() {
		case "backspace":
			runes := []rune(dialog.input)
			if len(runes) > 0 {
				dialog.input = string(runes[:len(runes)-1])
			}
			m.groupDialog = dialog
			return m, nil
		case "ctrl+u":
			dialog.input = ""
			m.groupDialog = dialog
			return m, nil
		case "enter":
			name := strings.TrimSpace(dialog.input)
			if name == "" {
				m.message = "Group name cannot be empty"
				return m, nil
			}
			for _, existing := range m.groups {
				if existing == name && existing != dialog.original {
					m.message = "Group already exists: " + name
					return m, nil
				}
			}
			m.groupDialog = groupDialogState{}
			if dialog.mode == groupDialogCreate {
				return m, m.groupMutationCmd("created", name, "", func() (string, error) {
					return config.CreateGroupFragment(m.groupsDir, config.HostGroup{Name: name})
				})
			}
			definition, ok := m.groupDefinition(dialog.original)
			if !ok {
				m.message = "Group definition disappeared"
				return m, nil
			}
			definition.Name = name
			return m, m.groupMutationCmd("renamed", name, "", func() (string, error) {
				return config.UpdateGroupFragment(m.groupsDir, dialog.original, definition)
			})
		default:
			text := strings.ReplaceAll(session.CleanText(msg.Text), "\n", "")
			if text != "" && len([]rune(dialog.input))+len([]rune(text)) <= 80 {
				dialog.input += text
				m.groupDialog = dialog
			}
			return m, nil
		}
	case groupDialogDelete:
		if msg.String() != "enter" {
			return m, nil
		}
		m.groupDialog = groupDialogState{}
		return m, m.groupMutationCmd("deleted", "", "", func() (string, error) {
			return "", config.DeleteGroupFragment(m.groupsDir, dialog.original)
		})
	case groupDialogMembership:
		switch msg.String() {
		case "j", "down":
			if dialog.selected < len(m.groups)-1 {
				dialog.selected++
			}
			m.groupDialog = dialog
			return m, nil
		case "k", "up":
			if dialog.selected > 0 {
				dialog.selected--
			}
			m.groupDialog = dialog
			return m, nil
		case "enter", " ":
			if len(m.groups) == 0 {
				return m, nil
			}
			group := m.groups[min(dialog.selected, len(m.groups)-1)]
			present := false
			for _, host := range m.hosts {
				if host.ID == dialog.hostID {
					present = contains(host.Groups, group)
					break
				}
			}
			m.groupDialog = groupDialogState{}
			action := "added to"
			if present {
				action = "removed from"
			}
			return m, m.groupMutationCmd(dialog.hostLabel+" "+action+" "+group, "", dialog.hostID, func() (string, error) {
				return config.SetGroupMember(m.groupsDir, group, dialog.hostID, !present)
			})
		}
	}
	return m, nil
}

func (m Model) groupMutationCmd(action, selectGroup, hostID string, mutate func() (string, error)) tea.Cmd {
	reload := m.groupReload
	navIndex := m.source
	return func() tea.Msg {
		if reload == nil {
			return groupChangedMsg{err: fmt.Errorf("group reload is unavailable")}
		}
		path, err := mutate()
		if err != nil {
			return groupChangedMsg{err: err}
		}
		hosts, sources, groups, err := reload()
		return groupChangedMsg{hosts: hosts, sources: sources, groups: groups, action: action, path: path, selectGroup: selectGroup, hostID: hostID, navIndex: navIndex, err: err}
	}
}

func (m Model) applyGroupChanged(msg groupChangedMsg) (tea.Model, tea.Cmd) {
	m.editing = false
	if msg.err != nil {
		m.message = "Group: " + session.CleanText(msg.err.Error())
		return m, nil
	}
	m.hosts, m.sources = msg.hosts, msg.sources
	m.setGroupDefinitions(msg.groups)
	m.queue = nil
	m.source = min(msg.navIndex, len(m.sources)+len(m.groups))
	if msg.selectGroup != "" {
		m.source = 0
		for index, group := range m.groups {
			if group == msg.selectGroup {
				m.source = len(m.sources) + index + 1
				break
			}
		}
		m.focus = focusSources
	}
	m.selected = 0
	if msg.hostID != "" {
		for position, index := range m.visibleIndices() {
			if m.hosts[index].ID == msg.hostID {
				m.selected = position
				break
			}
		}
	}
	m.message = "Group " + msg.action
	if msg.path != "" {
		m.message += ": " + msg.path
	}
	cmds := []tea.Cmd{m.resolveSelectedCmd()}
	if m.active == 0 {
		cmds = append(cmds, func() tea.Msg { return startPollMsg{} })
	} else {
		m.refreshAfter = true
	}
	return m, tea.Batch(cmds...)
}

func (m Model) editSelectedGroup() (tea.Model, tea.Cmd) {
	if m.groupReload == nil || strings.TrimSpace(m.groupEditor) == "" {
		m.message = "Group editor is unavailable"
		return m, nil
	}
	name := m.selectedGroup()
	path, err := config.GroupFragmentPath(m.groupsDir, name)
	if err != nil {
		m.message = "Group: " + err.Error()
		return m, nil
	}
	cmd, err := editorCommand(m.groupEditor, path)
	if err != nil {
		m.message = "Group editor: " + err.Error()
		return m, nil
	}
	m.editing = true
	m.queue = nil
	m.polling = make(map[string]bool)
	m.refreshAfter = m.active > 0
	m.message = "Editing group: " + path
	return m, editGroupFragmentCmd(cmd, path, name, m.groupReload, m.source)
}

func editGroupFragmentCmd(cmd *exec.Cmd, path, name string, reload func() ([]inventory.Host, []inventory.SourceSummary, []config.HostGroup, error), navIndex int) tea.Cmd {
	if reload == nil {
		return func() tea.Msg { return groupChangedMsg{err: fmt.Errorf("group reload is unavailable")} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return groupChangedMsg{err: fmt.Errorf("editor: %w", err)}
		}
		hosts, sources, groups, err := reload()
		return groupChangedMsg{hosts: hosts, sources: sources, groups: groups, action: "edited", path: path, selectGroup: name, navIndex: navIndex, err: err}
	})
}

func (m Model) overlayGroupDialog(base string, width, height int) string {
	dialog := m.groupDialog
	menuWidth := min(72, max(44, width/2))
	innerWidth := menuWidth - 4
	var lines []string
	switch dialog.mode {
	case groupDialogCreate:
		lines = []string{titleStyle.Render("CREATE GROUP"), "", "name: " + dialog.input + "█", "", mutedStyle.Render("Enter create · Esc cancel")}
	case groupDialogRename:
		lines = []string{titleStyle.Render("RENAME GROUP · " + dialog.original), "", "name: " + dialog.input + "█", "", mutedStyle.Render("Enter rename · Esc cancel")}
	case groupDialogDelete:
		lines = []string{titleStyle.Render("DELETE GROUP"), "", "Delete local group “" + dialog.original + "”?", mutedStyle.Render("Sources and hosts are not modified."), "", lipgloss.NewStyle().Foreground(colorYellow).Render("Enter confirm · Esc cancel")}
	case groupDialogMembership:
		lines = []string{titleStyle.Render(truncate("GROUP MEMBERSHIP · "+dialog.hostLabel, innerWidth)), ""}
		start := max(0, dialog.selected-5)
		end := min(len(m.groups), start+11)
		for index := start; index < end; index++ {
			group := m.groups[index]
			mark := "○"
			for _, host := range m.hosts {
				if host.ID == dialog.hostID && contains(host.Groups, group) {
					mark = "●"
					break
				}
			}
			prefix := "  "
			if index == dialog.selected {
				prefix = "› "
			}
			line := padRight(truncate(prefix+mark+" "+group, innerWidth), innerWidth)
			if index == dialog.selected {
				line = selected.Render(ansi.Strip(line))
			}
			lines = append(lines, line)
		}
		lines = append(lines, "", mutedStyle.Render("Enter/Space add or remove · Esc cancel"))
	}
	for index := range lines {
		lines[index] = truncate(lines[index], innerWidth)
	}
	popup := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorPurple).Padding(0, 1).Width(menuWidth - 2).Render(strings.Join(lines, "\n"))
	return centeredOverlay(base, popup, width, height)
}
