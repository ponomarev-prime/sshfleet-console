package knownhosts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectBackupAndRemove(t *testing.T) {
	directory := t.TempDir()
	knownHosts := fixtureKnownHosts(t, directory, "example.test")
	original, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	oldBackup := knownHosts + ".old"
	if err := os.WriteFile(oldBackup, []byte("pre-existing backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := Inspect(
		"example.test",
		[]string{knownHosts},
		knownHosts,
		7,
		"SHA256:presented-but-out-of-band-verification-required",
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Line != 7 || len(plan.StoredFingerprints) != 1 {
		t.Fatalf("plan = %#v", plan)
	}

	applied, err := Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(applied.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatal("backup does not match original known_hosts")
	}
	oldContents, err := os.ReadFile(oldBackup)
	if err != nil || string(oldContents) != "pre-existing backup\n" {
		t.Fatalf("pre-existing .old backup was changed: %q, %v", oldContents, err)
	}
	found, _, err := find(knownHosts, "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("host entry still present after Apply")
	}
}

func TestApplyRefusesChangedFile(t *testing.T) {
	directory := t.TempDir()
	knownHosts := fixtureKnownHosts(t, directory, "example.test")
	plan, err := Inspect("example.test", []string{knownHosts}, knownHosts, 1, "SHA256:presented")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(knownHosts, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("# concurrent change\n")
	_ = file.Close()
	if _, err := Apply(plan); err == nil || !strings.Contains(err.Error(), "changed after inspection") {
		t.Fatalf("Apply() error = %v", err)
	}
	if matches, _ := filepath.Glob(knownHosts + ".sshfleet-backup-*"); len(matches) != 0 {
		t.Fatalf("backup created before change detection: %#v", matches)
	}
}

func TestInspectRefusesReportedFileOutsideEffectiveConfig(t *testing.T) {
	directory := t.TempDir()
	allowed := fixtureKnownHosts(t, filepath.Join(directory, "allowed"), "example.test")
	reported := fixtureKnownHosts(t, filepath.Join(directory, "reported"), "example.test")
	_, err := Inspect("example.test", []string{allowed}, reported, 1, "SHA256:presented")
	if err == nil || !strings.Contains(err.Error(), "outside effective UserKnownHostsFile") {
		t.Fatalf("Inspect() error = %v", err)
	}
}

func fixtureKnownHosts(t *testing.T, directory, hostname string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(directory, "fixture_key")
	cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", keyPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture key: %v: %s", err, output)
	}
	publicKey, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(publicKey))
	if len(fields) < 2 {
		t.Fatalf("invalid generated public key: %q", publicKey)
	}
	knownHosts := filepath.Join(directory, "known_hosts")
	line := hostname + " " + fields[0] + " " + fields[1] + "\n"
	if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return knownHosts
}
