package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
)

func TestLaunchSourcesAddsAndRemovesDefault(t *testing.T) {
	existing := []config.Source{{Name: "default", Kind: "ssh_config", Path: "~/.ssh/config"}}
	got := launchSources(existing, nil, []string{"lab=/tmp/lab.conf", "/tmp/work.conf"}, nil, nil, true)
	if len(got) != 2 {
		t.Fatalf("sources = %#v", got)
	}
	if got[0].Name != "lab" || got[0].Path != "/tmp/lab.conf" {
		t.Fatalf("first source = %#v", got[0])
	}
	if got[1].Name != "work" {
		t.Fatalf("second source = %#v", got[1])
	}
}

func TestLaunchSourcesAddsTrustedLocalConfig(t *testing.T) {
	got := launchSources(nil, nil, nil, nil, []string{"workstation=/tmp/local.toml"}, true)
	if len(got) != 1 || got[0].Name != "workstation" || got[0].Kind != config.SourceLocalConfig || got[0].Path != "/tmp/local.toml" {
		t.Fatalf("local source = %#v", got)
	}
}

func TestSourceAddFlagsPersistStrictInventory(t *testing.T) {
	dir := t.TempDir()
	inventoryPath := filepath.Join(dir, "inventory.toml")
	if err := os.WriteFile(inventoryPath, []byte("version = 1\n[[hosts]]\nalias = \"demo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	outputPath := filepath.Join(dir, "output")
	output, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	err = runSourceCommand([]string{"add", "--type", "inventory", "--name", "demo", "--path", inventoryPath, "--sources-dir", filepath.Join(dir, "sources.d")}, input, output)
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	sources, err := config.LoadSourceFragments(filepath.Join(dir, "sources.d"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Kind != "inventory" || !strings.HasSuffix(sources[0].Path, "inventory.toml") {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestSourceAddPersistsAndValidatesTrustedLocalConfig(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "local.toml")
	if err := os.WriteFile(localPath, []byte("version = 1\n[[local_hosts]]\nalias = \"here\"\nmode = \"direct\"\nshell = \"/bin/sh\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	err = runSourceCommand([]string{"add", "--type", "local", "--name", "workstation", "--path", localPath, "--sources-dir", filepath.Join(dir, "sources.d")}, input, output)
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	fragments, err := config.LoadSourceFragments(filepath.Join(dir, "sources.d"))
	if err != nil || len(fragments) != 1 || fragments[0].Kind != config.SourceLocalConfig {
		t.Fatalf("local fragments = %#v err=%v", fragments, err)
	}
}

func TestSourceAddUsesSourcesDirFromMainAppConfig(t *testing.T) {
	dir := t.TempDir()
	sshConfig := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshConfig, []byte("Host demo\n  HostName 192.0.2.10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcesDir := filepath.Join(dir, "configured-sources.d")
	appConfig := filepath.Join(dir, "app.toml")
	data := "version = 1\n[app]\nsources_dir = " + fmt.Sprintf("%q", sourcesDir) + "\n"
	if err := os.WriteFile(appConfig, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	err = runSourceCommand([]string{"add", "--type", "openssh", "--name", "demo", "--path", sshConfig, "--config", appConfig}, input, output)
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sourcesDir, "demo.toml")); err != nil {
		t.Fatalf("source fragment was not written to configured directory: %v", err)
	}
}

func TestLaunchSourcesDeduplicatesNames(t *testing.T) {
	got := launchSources(nil, nil, []string{"lab=/tmp/a", "lab=/tmp/b"}, nil, nil, true)
	if got[0].Name != "lab" || got[1].Name != "lab-2" {
		t.Fatalf("sources = %#v", got)
	}
}

func TestLaunchSourcesLoadsUserConfigByDefaultAndDeduplicatesIt(t *testing.T) {
	got := launchSources([]config.Source{{Name: "old-default", Kind: "ssh_config", Path: "~/.ssh/config"}}, nil, nil, nil, nil, false)
	if len(got) != 1 || got[0].Name != "user" {
		t.Fatalf("sources = %#v", got)
	}
	without := launchSources(nil, nil, nil, nil, nil, true)
	if len(without) != 0 {
		t.Fatalf("opt-out sources = %#v", without)
	}
}

func TestSourceAddPersistsEncryptedAndRemoteDeclarations(t *testing.T) {
	dir := t.TempDir()
	bundleDir := filepath.Join(dir, "bundle")
	if err := os.Mkdir(bundleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("lab ssh-ed25519 AAAATEST\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcesDir := filepath.Join(dir, "sources.d")
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runSourceCommand([]string{"add", "--type", "encrypted", "--name", "sealed", "--path", bundleDir, "--signing-key", allowed, "--age-identity-ref", "secret-service:sshfleet/age/sealed", "--sources-dir", sourcesDir}, input, output); err != nil {
		t.Fatal(err)
	}
	if err := runSourceCommand([]string{"add", "--type", "remote", "--name", "central", "--url", "https://inventory.example/fleet/", "--signing-key", allowed, "--age-identity-ref", "secret-service:sshfleet/age/central", "--auth-credential", "remote-token", "--sources-dir", sourcesDir}, input, output); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	sources, err := config.LoadSourceFragments(sourcesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Kind != config.SourceRemote || sources[1].Kind != config.SourceEncryptedInventory {
		t.Fatalf("sources = %#v", sources)
	}
	if sources[0].AuthCredential != "remote-token" || sources[1].AgeIdentityRef == "" {
		t.Fatalf("source security references = %#v", sources)
	}
}

func TestSourcePackCLIWritesOnlyBundleArtifacts(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"age":        "#!/bin/sh\ncat >/dev/null\nprintf 'encrypted-fixture'\n",
		"ssh-keygen": "#!/bin/sh\ncat >/dev/null\nprintf 'signed-fixture'\n",
	} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	inventoryPath := filepath.Join(dir, "inventory.toml")
	if err := os.WriteFile(inventoryPath, []byte("version = 1\n[[hosts]]\nalias = \"pack-secret-host\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	signingKey := filepath.Join(dir, "signing")
	if err := os.WriteFile(signingKey, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.Create(filepath.Join(dir, "output.log"))
	if err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(dir, "bundle")
	err = runSourceCommand([]string{"pack", "--source-id", "packed", "--revision", "3", "--inventory", inventoryPath, "--output", bundleDir, "--recipient", "age1fixture", "--signing-key", signingKey, "--expires", "1h"}, input, output)
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.toml", "manifest.sig", "inventory.toml.age"} {
		data, err := os.ReadFile(filepath.Join(bundleDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "pack-secret-host") {
			t.Fatalf("plaintext leaked into %s", name)
		}
	}
}

func TestResolveWorkspaceBundleTreatsLauncherDefaultAsOptional(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.tar.gz")
	resolved, err := resolveWorkspaceBundle("", missing)
	if err != nil || resolved != "" {
		t.Fatalf("optional launcher bundle = %q, %v", resolved, err)
	}
	if _, err := resolveWorkspaceBundle(missing, ""); err == nil {
		t.Fatal("explicit missing workspace bundle accepted")
	}
	filePath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(filePath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolveWorkspaceBundle(filePath, "")
	if err != nil || resolved != filePath {
		t.Fatalf("explicit workspace bundle = %q, %v", resolved, err)
	}
}
