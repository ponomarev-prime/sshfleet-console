package sshconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAliasesIncludesAndSkipsPatterns(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "included.conf")
	if err := os.WriteFile(included, []byte("Host jump web-02\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	data := "Include \"" + included + "\"\nHost web-01 prod-* !blocked\nHost=database\n"
	if err := os.WriteFile(main, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Aliases(main)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"database", "jump", "web-01", "web-02"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aliases = %#v, want %#v", got, want)
	}
}

func TestAliasesMissingIsEmpty(t *testing.T) {
	got, err := Aliases(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("aliases = %#v", got)
	}
}

func TestSystemSSHConfigSnapshot(t *testing.T) {
	path := os.Getenv("SSHF_SYSTEM_CONFIG_FIXTURE")
	if path == "" {
		t.Skip("set SSHF_SYSTEM_CONFIG_FIXTURE to an isolated copy of ~/.ssh/config")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("system SSH config fixture %s: %v", path, err)
	}
	aliases, err := Aliases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) == 0 {
		t.Fatal("snapshot yielded no literal aliases")
	}
}
