package localtarget

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func TestLocalCommandsUseSeparateArgvAndConfiguredDirectory(t *testing.T) {
	dir := t.TempDir()
	host := inventory.Host{ID: "local:here", Alias: "here", Transport: inventory.TransportLocal, Shell: "/bin/sh", ShellArgs: []string{"-l"}, WorkingDirectory: dir}
	interactive, err := InteractiveCommand(host)
	if err != nil {
		t.Fatal(err)
	}
	if interactive.Dir != dir || len(interactive.Args) != 2 || interactive.Args[1] != "-l" {
		t.Fatalf("interactive command = %#v dir=%q", interactive.Args, interactive.Dir)
	}
	command, err := Command(context.Background(), host, []string{"printf", "%s", "hello;not-a-shell"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Dir != dir || command.Args[len(command.Args)-1] != "hello;not-a-shell" {
		t.Fatalf("argv command = %#v dir=%q", command.Args, command.Dir)
	}
	if _, err := os.Stat(filepath.Clean(command.Dir)); err != nil {
		t.Fatal(err)
	}
}

func TestLocalProbeCollectsCurrentMachine(t *testing.T) {
	host := inventory.Host{ID: "local:here", Alias: "here", Transport: inventory.TransportLocal, Shell: "/bin/sh"}
	result := Probe(context.Background(), host)
	if result.Status != probe.StatusOnline || result.CPUCount < 1 || result.MemoryTotal == 0 {
		t.Fatalf("local probe = %#v", result)
	}
}
