package toolcheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrefersSSHFleetThenSystem(t *testing.T) {
	root := t.TempDir()
	own := filepath.Join(root, "own")
	system := filepath.Join(root, "system")
	for _, dir := range []string{own, system} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nvim"), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SSHF_COMPANION_DIRS", own)
	t.Setenv("PATH", system)
	result := Resolve("nvim")
	if result.Origin != OriginSSHFleet || result.Path != filepath.Join(own, "nvim") {
		t.Fatalf("own resolution = %#v", result)
	}
	if err := os.Remove(filepath.Join(own, "nvim")); err != nil {
		t.Fatal(err)
	}
	result = Resolve("nvim")
	if result.Origin != OriginSystem || result.Path != filepath.Join(system, "nvim") {
		t.Fatalf("system fallback = %#v", result)
	}
}

func TestResolveEditorUsesGlobalPriority(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nano"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHF_COMPANION_DIRS", "")
	t.Setenv("PATH", dir)
	result := ResolveEditor([]string{"nvim", "vim", "nano"})
	if result.Name != "nano" || result.Origin != OriginSystem {
		t.Fatalf("editor fallback = %#v", result)
	}
}
