package ui

import (
	"net"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
)

// Local configuration resolution is independent of remote probes, including
// --no-probe targets. Keep at most four ssh -G children, prioritizing selection.
func (m *Model) fillResolveSlots() tea.Cmd {
	if m.editing {
		return nil
	}
	var cmds []tea.Cmd
	limit := max(1, min(4, m.maxConcurrent))
	schedule := func(index int) {
		if index < 0 || index >= len(m.hosts) || len(m.resolveRunning) >= limit {
			return
		}
		host := m.hosts[index]
		if host.TargetTransport() != inventory.TransportSSH {
			return
		}
		if _, cached := m.effective[host.ID]; cached {
			return
		}
		if _, running := m.resolveRunning[host.ID]; running {
			return
		}
		m.resolveRunning[host.ID] = m.resolveEpoch
		cmds = append(cmds, m.resolveCmd(index))
	}
	schedule(m.selectedHostIndex())
	for index := range m.hosts {
		schedule(index)
		if len(m.resolveRunning) >= limit {
			break
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) invalidateResolutions() {
	m.resolveEpoch++
	clear(m.effective)
	// Old work still occupies its slots until completion/timeout. Its epoch
	// prevents a late response from replacing a reloaded target's endpoint.
}

func (m Model) connectionLabel(host inventory.Host) string {
	clean := func(value string) string {
		return strings.TrimSpace(session.CleanText(value))
	}
	unknown := func(value string) string {
		if value = clean(value); value != "" {
			return value
		}
		return "?"
	}
	switch host.TargetTransport() {
	case inventory.TransportLocal:
		return "[local]"
	case inventory.TransportContainer:
		id := clean(host.ContainerID)
		if len(id) > 12 {
			id = id[:12]
		}
		label := unknown(host.ContainerRuntime) + " / " + unknown(host.ContainerContext) + " / " + unknown(id)
		if host.ContainerDiscoveryState == string(inventory.SourceStateStale) {
			label += "; stale"
		}
		return "[" + label + "]"
	default:
		effective, ok := m.effective[host.ID]
		if !ok {
			return "[resolving...]"
		}
		if effective.Error != "" {
			return "[resolve failed]"
		}
		address := unknown(effective.Hostname)
		port := clean(effective.Port)
		if port != "" && port != "22" {
			address = net.JoinHostPort(strings.Trim(address, "[]"), port)
		} else if strings.Contains(address, ":") {
			address = "[" + strings.Trim(address, "[]") + "]"
		}
		label := unknown(effective.User) + " @ " + address
		if jump := clean(effective.ProxyJump); jump != "" {
			parts := strings.Split(jump, ",")
			for index := range parts {
				parts[index] = strings.TrimSpace(parts[index])
			}
			label += "; via " + strings.Join(parts, " -> ")
		} else if effective.CustomProxy {
			label += "; custom proxy"
		}
		return "[" + label + "]"
	}
}
