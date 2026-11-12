package inventory

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
)

func TestProgressPreservesFullInventoryAndSourceBoundaries(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	os.WriteFile(first, []byte("Host alpha shared\n"), 0o600)
	os.WriteFile(second, []byte("Host beta shared\n"), 0o600)
	cfg := config.Config{Sources: []config.Source{{Name: "one", Kind: config.SourceSSHConfig, Path: first}, {Name: "two", Kind: config.SourceSSHConfig, Path: second}}, Hosts: []config.HostConfig{{Alias: "shared", User: "override"}, {Alias: "later", Source: "two", Hostname: "192.0.2.21"}, {Alias: "direct", Hostname: "192.0.2.22"}}, Groups: []config.HostGroup{{Name: "both", Members: []string{"one:shared", "two:shared"}}}}
	wantHosts, wantSources := BuildWithOptions(cfg, BuildOptions{})
	var snapshots [][]Host
	gotHosts, gotSources := BuildWithOptions(cfg, BuildOptions{OnSource: func(hosts []Host, _ []SourceSummary) { snapshots = append(snapshots, hosts) }})
	if !reflect.DeepEqual(wantHosts, gotHosts) || !reflect.DeepEqual(wantSources, gotSources) {
		t.Fatal("progress changed final inventory")
	}
	if len(snapshots) != 2 {
		t.Fatal(len(snapshots))
	}
	for _, host := range snapshots[0] {
		if host.SourceName != "one" || host.Alias == "direct" {
			t.Fatalf("pending source target appeared early: %+v", host)
		}
		if host.Alias == "shared" && host.User != "override" {
			t.Fatal("unscoped override missing")
		}
	}
	if len(snapshots[0]) != 2 {
		t.Fatalf("first snapshot mutated: %d", len(snapshots[0]))
	}
}

func TestProgressDoesNotEmitAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	BuildWithOptions(config.Config{Sources: []config.Source{{Name: "pending", Kind: config.SourceSSHConfig, Path: "unused"}}}, BuildOptions{Context: ctx, OnSource: func([]Host, []SourceSummary) { called = true }})
	if called {
		t.Fatal("emitted after cancellation")
	}
}
