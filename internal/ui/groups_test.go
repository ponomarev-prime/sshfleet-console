package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
)

func TestGroupsCRUDAndMembershipThroughKeyboard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "groups.d")
	baseHosts := []inventory.Host{
		{ID: "user:202", Alias: "202", SourceName: "user"},
		{ID: "user:203", Alias: "203", SourceName: "user"},
	}
	sources := []inventory.SourceSummary{{Name: "user", Hosts: 2}}
	reload := func() ([]inventory.Host, []inventory.SourceSummary, []config.HostGroup, error) {
		groups, err := config.LoadGroupFragments(dir)
		if err != nil {
			return nil, nil, nil, err
		}
		hosts := append([]inventory.Host(nil), baseHosts...)
		inventory.AssignGroups(hosts, groups)
		return hosts, append([]inventory.SourceSummary(nil), sources...), groups, nil
	}
	m := New(baseHosts, sources, openssh.Client{}, time.Hour, 2,
		WithGroupsAndCommands(nil, nil),
		WithGroupEditor(dir, "true", reload),
	)
	m.width, m.height, m.focus = 120, 30, focusSources

	// n → type a Unicode name → Enter creates one private fragment and selects it.
	m = updateWithKey(t, m, tea.Key{Text: "n", Code: 'n'})
	if m.groupDialog.mode != groupDialogCreate {
		t.Fatalf("create dialog = %#v", m.groupDialog)
	}
	m = updateWithKey(t, m, tea.Key{Text: "Стенды 202 203"})
	updated, command := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if command == nil {
		t.Fatal("create did not return a command")
	}
	updated, _ = m.Update(command())
	m = updated.(Model)
	if m.selectedGroup() != "Стенды 202 203" || len(m.groups) != 1 {
		t.Fatalf("created group = source:%d groups:%#v", m.source, m.groups)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "SOURCES") || !strings.Contains(view, "GROUPS") || !strings.Contains(view, "VIEWS") || !strings.Contains(view, "Стенды") || !strings.Contains(view, "PRIVATE GROUP") {
		t.Fatalf("separate navigation sections missing:\n%s", view)
	}

	// From All available, Enter exposes membership in the ordinary action menu.
	m.source, m.focus, m.selected = 0, focusHosts, 0
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	actions := m.availableHostActions(0)
	membershipIndex := -1
	for index, action := range actions {
		if action.kind == actionGroupMembership && action.label == "Manage group membership" {
			membershipIndex = index
			break
		}
	}
	if !m.actionMenu || membershipIndex < 0 {
		t.Fatalf("membership action missing: menu=%v actions=%#v", m.actionMenu, actions)
	}
	m.actionSelected = membershipIndex
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEnter})
	if m.groupDialog.mode != groupDialogMembership || !strings.Contains(ansi.Strip(m.View().Content), "GROUP MEMBERSHIP · 202") {
		t.Fatalf("membership dialog = %#v", m.groupDialog)
	}
	updated, command = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	updated, _ = m.Update(command())
	m = updated.(Model)
	if len(m.hosts[0].Groups) != 1 || m.hosts[0].Groups[0] != "Стенды 202 203" {
		t.Fatalf("host groups = %#v", m.hosts[0].Groups)
	}

	// The direct m shortcut reaches the same dialog and remains available.
	m = updateWithKey(t, m, tea.Key{Text: "m", Code: 'm'})
	if m.groupDialog.mode != groupDialogMembership {
		t.Fatalf("membership shortcut dialog = %#v", m.groupDialog)
	}
	m = updateWithKey(t, m, tea.Key{Code: tea.KeyEsc})

	// R renames the group without changing membership; D removes only overlay.
	m.focus, m.source = focusSources, len(m.sources)+1
	m = updateWithKey(t, m, tea.Key{Text: "R", Code: 'R'})
	m = updateWithKey(t, m, tea.Key{Code: 'u', Mod: tea.ModCtrl})
	m = updateWithKey(t, m, tea.Key{Text: "Demo Fleet"})
	updated, command = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	updated, _ = m.Update(command())
	m = updated.(Model)
	if m.selectedGroup() != "Demo Fleet" || !contains(m.hosts[0].Groups, "Demo Fleet") {
		t.Fatalf("rename = selected:%q host:%#v", m.selectedGroup(), m.hosts[0].Groups)
	}
	m = updateWithKey(t, m, tea.Key{Text: "D", Code: 'D'})
	updated, command = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	updated, _ = m.Update(command())
	m = updated.(Model)
	if len(m.groups) != 0 || len(m.hosts[0].Groups) != 0 {
		t.Fatalf("delete left groups = %#v / %#v", m.groups, m.hosts[0].Groups)
	}
}

func TestMainConfigGroupCRUDFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "groups.d")
	host := inventory.Host{ID: "user:202", Alias: "202", SourceName: "user", Groups: []string{"main-group"}}
	reload := func() ([]inventory.Host, []inventory.SourceSummary, []config.HostGroup, error) {
		return []inventory.Host{host}, nil, []config.HostGroup{{Name: "main-group", Members: []string{"user:202"}}}, nil
	}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Hour, 2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "main-group", Members: []string{"user:202"}}}, nil),
		WithGroupEditor(dir, "true", reload),
	)
	m.width, m.height, m.focus, m.source = 120, 30, focusSources, 1
	m = updateWithKey(t, m, tea.Key{Text: "D", Code: 'D'})
	updated, command := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	updated, _ = m.Update(command())
	m = updated.(Model)
	if !strings.Contains(m.message, "main config") || len(m.groups) != 1 {
		t.Fatalf("main group mutation did not fail closed: %q / %#v", m.message, m.groups)
	}
}
