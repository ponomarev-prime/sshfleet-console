//go:build !windows

package tooling

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFullInstallFailurePreservesBuildDiagnostics(t *testing.T) {
	root := t.TempDir()
	tools := filepath.Join(root, "tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(repositoryRoot(t), "tools", "test-install-full.sh"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tools, "test-install-full.sh")
	if err = os.WriteFile(path, script, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "install.sh"), []byte("#!/bin/sh\nprintf 'fixture build diagnostic\\n'\nprintf 'fixture stderr diagnostic\\n' >&2\nexit 23\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("sh", path).CombinedOutput()
	if err == nil {
		t.Fatal("failed installer was accepted")
	}
	for _, want := range []string{"fixture build diagnostic", "fixture stderr diagnostic"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
}
