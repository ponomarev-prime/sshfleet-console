package openssh

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/workspace"
)

func TestPrepareAndCommandWorkspaceUseClosedSSHFlow(t *testing.T) {
	dir := t.TempDir()
	bundle := writeWorkspaceFixture(t, dir)
	ssh := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' 'SSHF_WORKSPACE=/tmp/sshfleet-workspace.A1b2C3'\n"
	if err := os.WriteFile(ssh, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	client := Client{Binary: ssh, WorkspaceBundle: bundle, WorkspaceCleanup: true}
	host := inventory.Host{ID: "fixture:demo", Alias: "demo"}
	remotePath, err := client.PrepareWorkspace(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if remotePath != "/tmp/sshfleet-workspace.A1b2C3" {
		t.Fatalf("remote path = %q", remotePath)
	}
	command, err := client.WorkspaceCommand(host, remotePath, workspace.ToolLF)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, want := range []string{"-tt", "ForwardAgent=no", "ClearAllForwardings=yes", remotePath + "/run", "lf", "cleanup"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("workspace argv missing %q: %s", want, joined)
		}
	}
	if _, err := client.WorkspaceCommand(host, "/tmp/sshfleet-workspace.good;touch_bad", workspace.ToolLF); err == nil {
		t.Fatal("unsafe remote path accepted")
	}
}

func TestWorkspaceInstallFailsClosedBeforeExtractionWithoutSupportedGlibc(t *testing.T) {
	for _, required := range []string{"getconf GNU_LIBC_VERSION", "need glibc 2.34+", "directory=$(mktemp"} {
		if !strings.Contains(workspaceInstallScript, required) {
			t.Fatalf("workspace install script is missing %q", required)
		}
	}
	if strings.Index(workspaceInstallScript, "getconf GNU_LIBC_VERSION") > strings.Index(workspaceInstallScript, "directory=$(mktemp") {
		t.Fatal("libc capability check must happen before creating the remote workspace")
	}
}

func writeWorkspaceFixture(t *testing.T, dir string) string {
	t.Helper()
	filePath := filepath.Join(dir, "bundle.tar.gz")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	archive := tar.NewWriter(gz)
	for _, name := range []string{
		"run", "shell", "manifest.toml", "bin/lf", "bin/nvim", "bin/dtop", "bin/bat",
		"bin/sshfleet-open", "bin/sshfleet-editor",
	} {
		value := []byte(name)
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o700, Size: int64(len(value)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filePath+".sha256", []byte(fmt.Sprintf("%x  bundle.tar.gz\n", sum)), 0o600); err != nil {
		t.Fatal(err)
	}
	return filePath
}
