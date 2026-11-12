package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func TestStartupShowsLoadingAndAddsReadySourceWithoutWaiting(t *testing.T) {
	m := New(nil, []inventory.SourceSummary{{Name: "one", State: inventory.SourceStateLoading}, {Name: "slow", State: inventory.SourceStateLoading}}, openssh.Client{}, time.Hour, 1)
	m.startupLoading = true
	if text := ansi.Strip(m.renderHosts(80, 20)); !strings.Contains(text, "Loading sources") {
		t.Fatal(text)
	}
	host := inventory.Host{ID: "one:api", Alias: "api", SourceName: "one", Probe: true}
	updated, cmd := m.Update(startupUpdateMsg{value: StartupUpdate{Hosts: []inventory.Host{host}, Sources: []inventory.SourceSummary{{Name: "one", Hosts: 1}}}})
	m = updated.(Model)
	if cmd == nil || len(m.hosts) != 1 || m.active != 1 || !m.startupLoading || m.sources[1].State != inventory.SourceStateLoading {
		t.Fatalf("partial startup: %+v", m.sources)
	}
	text := ansi.Strip(m.renderHosts(80, 20))
	if !strings.Contains(text, "api") || !strings.Contains(text, "probing...") {
		t.Fatal(text)
	}
}

func TestProbeQueueUsesIdentityAcrossReordering(t *testing.T) {
	hosts := []inventory.Host{{ID: "one:a", Alias: "a", Probe: true}, {ID: "one:b", Alias: "b", Probe: true}}
	m := New(hosts, nil, openssh.Client{}, time.Hour, 1)
	m.beginPoll()
	m.fillPollSlots()
	m.hosts[0], m.hosts[1] = m.hosts[1], m.hosts[0]
	updated, _ := m.Update(probeMsg{id: "one:a", result: probe.Result{Status: probe.StatusOnline}})
	m = updated.(Model)
	if !m.probeRunning["one:b"] || m.probeRunning["one:a"] || m.active != 1 {
		t.Fatal("queue followed a row index instead of identity")
	}
}

func TestStartupTaskStopsWhenCancelledWithFullChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := New(nil, nil, openssh.Client{}, time.Hour, 1, WithStartup(ctx, func(_ context.Context, emit func(StartupUpdate)) { emit(StartupUpdate{}); emit(StartupUpdate{}) }))
	done := make(chan struct{})
	go func() { m.startupTask(); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("startup task stayed blocked after cancellation")
	}
}

func TestDisabledProbeDoesNotLookLikePendingWork(t *testing.T) {
	if got := processText(inventory.Host{Alias: "api", Probe: false}, probe.Result{}, false); got != "probe disabled" {
		t.Fatal(got)
	}
}
