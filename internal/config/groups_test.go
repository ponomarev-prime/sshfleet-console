package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGroupFragmentsCRUDMembershipAndPrivateMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "groups.d")
	path, err := CreateGroupFragment(dir, HostGroup{Name: "Стенды 202 203"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("group fragment mode = %o", info.Mode().Perm())
	}
	groups, err := LoadGroupFragments(dir)
	if err != nil || len(groups) != 1 || groups[0].Name != "Стенды 202 203" {
		t.Fatalf("loaded groups = %#v, %v", groups, err)
	}
	if _, err := SetGroupMember(dir, groups[0].Name, "user:203", true); err != nil {
		t.Fatal(err)
	}
	if _, err := SetGroupMember(dir, groups[0].Name, "user:202", true); err != nil {
		t.Fatal(err)
	}
	groups, err = LoadGroupFragments(dir)
	if err != nil || !reflect.DeepEqual(groups[0].Members, []string{"user:202", "user:203"}) {
		t.Fatalf("members = %#v, %v", groups, err)
	}
	if _, err := SetGroupMember(dir, groups[0].Name, "user:203", false); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateGroupFragment(dir, groups[0].Name, HostGroup{Name: "Demo Fleet", Members: []string{"user:demo-01"}}); err != nil {
		t.Fatal(err)
	}
	groups, err = LoadGroupFragments(dir)
	if err != nil || len(groups) != 1 || groups[0].Name != "Demo Fleet" || !reflect.DeepEqual(groups[0].Members, []string{"user:demo-01"}) {
		t.Fatalf("renamed groups = %#v, %v", groups, err)
	}
	if err := DeleteGroupFragment(dir, "Demo Fleet"); err != nil {
		t.Fatal(err)
	}
	groups, err = LoadGroupFragments(dir)
	if err != nil || len(groups) != 0 {
		t.Fatalf("groups after delete = %#v, %v", groups, err)
	}
}

func TestGroupFragmentsRejectDuplicatesUnknownFieldsAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateGroupFragment(dir, HostGroup{Name: "same"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateGroupFragment(dir, HostGroup{Name: "same"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate create = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "invalid.toml"), []byte("version = 1\nunknown = true\n[[groups]]\nname = \"invalid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGroupFragments(dir); err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Fatalf("unknown field accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "invalid.toml")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.toml")
	if err := os.WriteFile(target, []byte("version = 1\n[[groups]]\nname = \"outside\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.toml")); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skip("symlinks unavailable")
		}
		t.Fatal(err)
	}
	if _, err := LoadGroupFragments(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink accepted: %v", err)
	}
	if _, err := GroupFragmentPath(dir, "outside"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink accepted by editor path lookup: %v", err)
	}
}

func TestMainConfigAllowsEmptyPrivateGroup(t *testing.T) {
	cfg := Defaults()
	cfg.Groups = []HostGroup{{Name: "empty"}}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("empty private group rejected: %v", err)
	}
}

func TestMainConfigGroupIsNotWritableThroughGroupFragments(t *testing.T) {
	dir := t.TempDir()
	if _, err := GroupFragmentPath(dir, "main-only"); err == nil || !strings.Contains(err.Error(), "main config") {
		t.Fatalf("main-only group error = %v", err)
	}
	if _, err := SetGroupMember(dir, "main-only", "user:202", true); err == nil || !strings.Contains(err.Error(), "main config") {
		t.Fatalf("main-only membership error = %v", err)
	}
}

func TestMergeHostGroupsRejectsShadowingAcrossLayers(t *testing.T) {
	if _, err := MergeHostGroups(
		[]HostGroup{{Name: "shared"}},
		[]HostGroup{{Name: "shared", Members: []string{"user:202"}}},
	); err == nil || !strings.Contains(err.Error(), "duplicate group") {
		t.Fatalf("group shadowing accepted: %v", err)
	}
	merged, err := MergeHostGroups(
		[]HostGroup{{Name: "main"}},
		[]HostGroup{{Name: "private", Members: []string{"user:203"}}},
	)
	if err != nil || len(merged) != 2 || merged[0].Name != "main" || merged[1].Name != "private" {
		t.Fatalf("merged groups = %#v, %v", merged, err)
	}
}
