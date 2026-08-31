package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func TestBuiltInViewsFilterLatestFleetState(t *testing.T) {
	hosts := []inventory.Host{
		{ID: "user:offline", Alias: "offline", SourceName: "user"},
		{ID: "user:auth", Alias: "auth", SourceName: "user"},
		{ID: "user:host-key", Alias: "host-key", SourceName: "user"},
		{ID: "user:probe", Alias: "probe", SourceName: "user"},
		{ID: "user:busy", Alias: "busy", SourceName: "user"},
		{ID: "containers:stale", Alias: "stale", SourceName: "containers · docker", Transport: inventory.TransportContainer, ContainerDiscoveryState: string(inventory.SourceStateStale)},
	}
	m := New(hosts, []inventory.SourceSummary{
		{Name: "user", Hosts: 5, State: inventory.SourceStateLoaded},
		{Name: "containers · docker", Hosts: 1, State: inventory.SourceStateStale},
	}, openssh.Client{}, time.Minute, 2)
	m.results = map[string]probe.Result{
		"user:offline":  {Status: probe.StatusUnreachable},
		"user:auth":     {Status: probe.StatusAuth},
		"user:host-key": {Status: probe.StatusHostKey},
		"user:probe":    {Status: probe.StatusError},
		"user:busy": {
			Status: probe.StatusOnline, CPUValid: true, CPUAvailablePct: 10,
			MemoryTotal: 16 << 30, MemoryAvailablePct: 10,
		},
	}

	want := map[string][]string{
		"Offline":   {"offline"},
		"Errors":    {"auth", "host-key", "probe"},
		"CPU ≥ 80%": {"busy"},
		"MEM ≤ 20%": {"busy"},
		"Stale":     {"stale"},
	}
	for index, view := range m.views {
		m.source = len(m.sources) + len(m.groups) + index + 1
		visible := m.visibleIndices()
		aliases := make([]string, 0, len(visible))
		for _, hostIndex := range visible {
			aliases = append(aliases, m.hosts[hostIndex].Alias)
		}
		if fmt.Sprint(aliases) != fmt.Sprint(want[view.name]) {
			t.Errorf("view %q = %v, want %v", view.name, aliases, want[view.name])
		}
		if m.viewCount(view) != len(want[view.name]) {
			t.Errorf("view %q count = %d, want %d", view.name, m.viewCount(view), len(want[view.name]))
		}
	}
}

func TestViewNavigationRendersSectionsAndEmptyExplanation(t *testing.T) {
	m := New(nil, []inventory.SourceSummary{{Name: "user", State: inventory.SourceStateEmpty}}, openssh.Client{}, time.Minute, 2,
		WithGroupsAndCommands([]config.HostGroup{{Name: "stands"}}, nil),
	)
	m.width, m.height, m.focus = 120, 30, focusSources
	m.source = m.lastNavigationIndex()

	view, ok := m.selectedView()
	if !ok || view.name != "Stale" {
		t.Fatalf("selected view = %#v, %v", view, ok)
	}
	rendered := ansi.Strip(m.View().Content)
	for _, want := range []string{"SOURCES", "GROUPS", "VIEWS", "Offline", "Errors", "Stale", "Targets retained", "No hosts in this view"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered view does not contain %q:\n%s", want, rendered)
		}
	}
}

func TestViewGlobalSearchRestoresSelectionByNameAfterSourceRefresh(t *testing.T) {
	host := inventory.Host{ID: "one:busy", Alias: "busy", SourceName: "one"}
	m := New([]inventory.Host{host}, []inventory.SourceSummary{{Name: "one", Hosts: 1}}, openssh.Client{}, time.Minute, 2)
	m.results[host.ID] = probe.Result{Status: probe.StatusOnline, MemoryTotal: 8 << 30, MemoryAvailablePct: 10}
	m.source = len(m.sources) + 4 // MEM ≤ 20%
	m.beginGlobalSearch()
	m.sources = append([]inventory.SourceSummary{{Name: "dynamic", Dynamic: true}}, m.sources...)
	m.restoreSearchContext()

	view, ok := m.selectedView()
	visible := m.visibleIndices()
	if !ok || view.name != "MEM ≤ 20%" || len(visible) != 1 || m.hosts[visible[0]].ID != host.ID {
		t.Fatalf("restored view = %#v/%v, visible = %#v", view, ok, visible)
	}
}

func TestViewDynamicDiscoveryPreservesSelectionByName(t *testing.T) {
	host := inventory.Host{ID: "one:offline", Alias: "offline", SourceName: "one"}
	m := New([]inventory.Host{host}, []inventory.SourceSummary{{Name: "one", Hosts: 1}}, openssh.Client{}, time.Minute, 2)
	m.results[host.ID] = probe.Result{Status: probe.StatusUnreachable}
	m.source = len(m.sources) + 1 // Offline

	updated, _ := m.Update(dynamicSourcesMsg{sources: []inventory.SourceSummary{{Name: "containers · docker", Dynamic: true, State: inventory.SourceStateEmpty}}})
	m = updated.(Model)
	view, ok := m.selectedView()
	if !ok || view.name != "Offline" || len(m.visibleIndices()) != 1 {
		t.Fatalf("dynamic restoration = %#v/%v at %d", view, ok, m.source)
	}
}
