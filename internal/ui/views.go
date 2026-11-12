package ui

import (
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

type navigationKind uint8

const (
	navigationAll navigationKind = iota
	navigationSource
	navigationGroup
	navigationView
)

type navigationRef struct {
	kind navigationKind
	name string
}

type fleetView struct {
	name        string
	description string
	match       func(Model, inventory.Host) bool
}

func defaultFleetViews() []fleetView {
	return []fleetView{
		{
			name:        "Offline",
			description: "Hosts whose latest probe could not reach the target.",
			match: func(m Model, host inventory.Host) bool {
				return m.results[host.ID].Status == probe.StatusUnreachable
			},
		},
		{
			name:        "Errors",
			description: "Authentication, host-key, and probe errors that need attention.",
			match: func(m Model, host inventory.Host) bool {
				switch m.results[host.ID].Status {
				case probe.StatusAuth, probe.StatusHostKey, probe.StatusError:
					return true
				default:
					return false
				}
			},
		},
		{
			name:        "CPU ≥ 80%",
			description: "Online hosts with at least 80% CPU utilization in the latest valid sample.",
			match: func(m Model, host inventory.Host) bool {
				result := m.results[host.ID]
				return result.Status == probe.StatusOnline && result.CPUValid && 100-result.CPUAvailablePct >= 80
			},
		},
		{
			name:        "MEM ≤ 20%",
			description: "Online hosts with 20% or less memory available.",
			match: func(m Model, host inventory.Host) bool {
				result := m.results[host.ID]
				return result.Status == probe.StatusOnline && result.MemoryTotal > 0 && result.MemoryAvailablePct <= 20
			},
		},
		{
			name:        "Stale",
			description: "Targets retained from the last successful dynamic-source discovery.",
			match: func(m Model, host inventory.Host) bool {
				if strings.EqualFold(host.ContainerDiscoveryState, string(inventory.SourceStateStale)) {
					return true
				}
				for _, source := range m.sources {
					if source.Name == host.SourceName && source.State == inventory.SourceStateStale {
						return true
					}
				}
				return false
			},
		},
	}
}

func (m Model) lastNavigationIndex() int {
	return len(m.sources) + len(m.groups) + len(m.views)
}

func (m Model) selectedNavigation() navigationRef {
	if m.source <= 0 {
		return navigationRef{kind: navigationAll}
	}
	if index := m.source - 1; index >= 0 && index < len(m.sources) {
		return navigationRef{kind: navigationSource, name: m.sources[index].Name}
	}
	if index := m.source - len(m.sources) - 1; index >= 0 && index < len(m.groups) {
		return navigationRef{kind: navigationGroup, name: m.groups[index]}
	}
	if index := m.source - len(m.sources) - len(m.groups) - 1; index >= 0 && index < len(m.views) {
		return navigationRef{kind: navigationView, name: m.views[index].name}
	}
	return navigationRef{kind: navigationAll}
}

func (m *Model) restoreNavigation(ref navigationRef) {
	m.source = 0
	switch ref.kind {
	case navigationSource:
		for index, source := range m.sources {
			if source.Name == ref.name {
				m.source = index + 1
				return
			}
		}
	case navigationGroup:
		for index, group := range m.groups {
			if group == ref.name {
				m.source = len(m.sources) + index + 1
				return
			}
		}
	case navigationView:
		for index, view := range m.views {
			if view.name == ref.name {
				m.source = len(m.sources) + len(m.groups) + index + 1
				return
			}
		}
	}
}

func (m Model) selectedView() (fleetView, bool) {
	index := m.source - len(m.sources) - len(m.groups) - 1
	if index < 0 || index >= len(m.views) {
		return fleetView{}, false
	}
	return m.views[index], true
}

func (m Model) viewCount(view fleetView) int {
	count := 0
	for _, host := range m.hosts {
		if view.match(m, host) {
			count++
		}
	}
	return count
}
