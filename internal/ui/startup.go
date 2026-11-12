package ui

import (
	"context"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/runlog"
	"github.com/ponomarev-prime/sshfleet-console/internal/toolcheck"
)

type StartupUpdate struct {
	Hosts    []inventory.Host
	Sources  []inventory.SourceSummary
	Client   *openssh.Client
	Health   []toolcheck.Result
	Editor   *toolcheck.Result
	Complete bool
}
type startupUpdateMsg struct{ value StartupUpdate }
type StartupLoader func(context.Context, func(StartupUpdate))

func WithRunLog(log *runlog.Logger) Option { return func(m *Model) { m.log = log } }

func WithStartup(ctx context.Context, loader StartupLoader) Option {
	return func(m *Model) {
		ctx, cancel := context.WithCancel(ctx)
		m.startupCancel = cancel
		m.startupLoading = true
		events := make(chan StartupUpdate, 1)
		m.startupEvents = events
		m.startupTask = func() tea.Msg {
			emit := func(value StartupUpdate) {
				select {
				case events <- value:
				case <-ctx.Done():
				}
			}
			loader(ctx, emit)
			emit(StartupUpdate{Complete: true})
			return nil
		}
		m.startupContext = ctx
	}
}

func (m Model) cancelBackground() {
	if m.startupCancel != nil {
		m.startupCancel()
	}
}

func (m Model) awaitStartup() tea.Cmd {
	if m.startupEvents == nil || !m.startupLoading {
		return nil
	}
	return func() tea.Msg {
		select {
		case value := <-m.startupEvents:
			return startupUpdateMsg{value: value}
		case <-m.startupContext.Done():
			return nil
		}
	}
}

func (m *Model) applyStartup(value StartupUpdate) tea.Cmd {
	if value.Client != nil {
		m.client = *value.Client
	}
	if value.Health != nil {
		m.health = value.Health
	}
	if value.Editor != nil {
		m.editorHealth = *value.Editor
		m.editor = value.Editor.Path
		m.groupEditor = value.Editor.Path
	}
	for _, source := range value.Sources {
		if source.State == "" && source.Err == nil {
			source.State = inventory.SourceStateLoaded
			if source.Hosts == 0 {
				source.State = inventory.SourceStateEmpty
			}
		}
		found := false
		for i := range m.sources {
			if m.sources[i].Name == source.Name {
				m.sources[i] = source
				found = true
				break
			}
		}
		if !found {
			m.sources = append(m.sources, source)
		}
	}
	// Append while work is running: existing indices remain valid for owned
	// commands and the bounded probe queue. Never rebind selection to a row.
	for _, host := range value.Hosts {
		found := false
		for i := range m.hosts {
			if m.hosts[i].ID == host.ID {
				m.hosts[i] = host
				found = true
				break
			}
		}
		if found {
			continue
		}
		m.hosts = append(m.hosts, host)
		if host.Probe && !m.polling[host.ID] {
			m.polling[host.ID] = true
			m.queue = append(m.queue, host.ID)
		}
	}
	if value.Complete {
		m.startupLoading = false
		m.startupSortPending = true
		m.log.Record(runlog.Event{Event: "startup.ready", Count: len(m.hosts), Total: len(m.sources)})
	}
	m.sortStartupIfIdle()
	return tea.Batch(m.awaitStartup(), m.fillResolveSlots(), m.fillPollSlots())
}

func (m Model) operationContext() context.Context {
	if m.startupContext != nil {
		return m.startupContext
	}
	return context.Background()
}

func (m *Model) sortStartupIfIdle() {
	if !m.startupSortPending || m.active > 0 || len(m.queue) > 0 || m.actionMenu || m.embedded != nil || m.embeddedStarting || m.targetStarting || m.workspaceStarting || m.groupCommandRunning || m.hostKeyBusy {
		return
	}
	id := ""
	if index := m.selectedHostIndex(); index >= 0 {
		id = m.hosts[index].ID
	}
	sort.SliceStable(m.hosts, func(i, j int) bool {
		left, right := strings.ToLower(m.hosts[i].DisplayName()), strings.ToLower(m.hosts[j].DisplayName())
		if left == right {
			return m.hosts[i].ID < m.hosts[j].ID
		}
		return left < right
	})
	for pos, index := range m.visibleIndices() {
		if m.hosts[index].ID == id {
			m.selected = pos
			break
		}
	}
	m.startupSortPending = false
}
