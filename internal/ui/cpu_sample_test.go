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

func TestCPUPairIsBoundedAndPreservesTruth(t *testing.T) {
	online := func(total, idle uint64) probe.Result {
		return probe.Result{Status: probe.StatusOnline, CPUTotal: total, CPUIdle: idle}
	}
	for _, tc := range []struct {
		name     string
		previous probe.Result
		results  []probe.Result
		calls    int
		valid    bool
		status   probe.Status
	}{
		{"first pair", probe.Result{}, []probe.Result{online(100, 50), online(200, 100)}, 2, true, probe.StatusOnline},
		{"existing counter", online(100, 50), []probe.Result{online(200, 100)}, 1, true, probe.StatusOnline},
		{"reset counter", online(500, 250), []probe.Result{online(100, 50), online(200, 100)}, 2, true, probe.StatusOnline},
		{"unreachable", probe.Result{}, []probe.Result{{Status: probe.StatusUnreachable}}, 1, false, probe.StatusUnreachable},
		{"no counters", probe.Result{}, []probe.Result{online(0, 0)}, 1, false, probe.StatusOnline},
		{"unchanged counter", probe.Result{}, []probe.Result{online(100, 50), online(100, 50)}, 2, false, probe.StatusOnline},
		{"second failure", probe.Result{}, []probe.Result{online(100, 50), {Status: probe.StatusAuth}}, 2, false, probe.StatusAuth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := probeWithCPUSample(context.Background(), tc.previous, time.Nanosecond, func() probe.Result { r := tc.results[calls]; calls++; return r })
			if calls != tc.calls || got.CPUValid != tc.valid || got.Status != tc.status {
				t.Fatalf("calls=%d result=%+v", calls, got)
			}
			if got.CPUValid && got.CPUAvailablePct != 50 {
				t.Fatalf("false CPU delta: %+v", got)
			}
		})
	}
}

func TestLoadAverageIsSeparateFromCPUAndShowsSampleAge(t *testing.T) {
	host := inventory.Host{ID: "one:a", Alias: "a"}
	m := New([]inventory.Host{host}, nil, openssh.Client{}, time.Hour, 1)
	m.results[host.ID] = probe.Result{Status: probe.StatusOnline, CPUCount: 4, Load: [3]float64{0.1, 0.2, 0.3}, CheckedAt: time.Now().Add(-30 * time.Second)}
	text := ansi.Strip(m.renderPreview(80, 150))
	for _, want := range []string{"load avg 1/5/15m: 0.10 0.20 0.30", "sample age: 30s ago"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if got := utilizationCell(0, false, 20); strings.Contains(ansi.Strip(got), "0%") {
		t.Fatal("unknown CPU shown as zero")
	}
}

func TestCPUPairCancellationDoesNotStartAnotherProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	got := probeWithCPUSample(ctx, probe.Result{}, time.Hour, func() probe.Result {
		calls++
		cancel()
		return probe.Result{Status: probe.StatusOnline, CPUTotal: 100, CPUIdle: 50}
	})
	if calls != 1 || got.CPUValid {
		t.Fatalf("calls=%d result=%+v", calls, got)
	}
}
