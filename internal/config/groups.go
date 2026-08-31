package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

type groupFragment struct {
	Version int         `toml:"version"`
	Groups  []HostGroup `toml:"groups"`
}

func DefaultGroupsDir() (string, error) {
	dir, err := platform.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "sshfleet", "groups.d"), nil
}

func resolveGroupsDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		var err error
		dir, err = DefaultGroupsDir()
		if err != nil {
			return "", err
		}
	}
	return ExpandPath(dir)
}

// LoadGroupFragments reads the private, locally managed group overlay. Each
// file owns exactly one group so CRUD never has to rewrite unrelated groups.
func LoadGroupFragments(dir string) ([]HostGroup, error) {
	resolved, err := resolveGroupsDir(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read group fragments %s: %w", resolved, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	groups := make([]HostGroup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(resolved, entry.Name())
		if err := requireRegularGroupFragment(path); err != nil {
			return nil, err
		}
		fragment, err := readGroupFragment(path)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fragment.Groups[0])
	}
	if err := normalizeGroups(groups); err != nil {
		return nil, fmt.Errorf("group fragments: %w", err)
	}
	return groups, nil
}

// MergeHostGroups combines configuration layers without allowing an overlay to
// silently shadow another group with the same name.
func MergeHostGroups(layers ...[]HostGroup) ([]HostGroup, error) {
	var merged []HostGroup
	for _, layer := range layers {
		for _, group := range layer {
			group.Members = append([]string(nil), group.Members...)
			group.Match = append([]string(nil), group.Match...)
			merged = append(merged, group)
		}
	}
	if err := normalizeGroups(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func readGroupFragment(path string) (groupFragment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return groupFragment{}, fmt.Errorf("read group fragment %s: %w", path, err)
	}
	var fragment groupFragment
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&fragment); err != nil {
		return groupFragment{}, fmt.Errorf("parse group fragment %s: %w", path, err)
	}
	if fragment.Version != CurrentVersion {
		return groupFragment{}, fmt.Errorf("group fragment %s: unsupported version %d", path, fragment.Version)
	}
	if len(fragment.Groups) != 1 {
		return groupFragment{}, fmt.Errorf("group fragment %s: exactly one [[groups]] entry is required", path)
	}
	if err := normalizeGroups(fragment.Groups); err != nil {
		return groupFragment{}, fmt.Errorf("group fragment %s: %w", path, err)
	}
	return fragment, nil
}

func groupFragmentFilename(name string) string {
	digest := sha256.Sum256([]byte(name))
	return fmt.Sprintf("group-%x.toml", digest[:8])
}

func findGroupFragment(dir, name string) (string, HostGroup, error) {
	resolved, err := resolveGroupsDir(dir)
	if err != nil {
		return "", HostGroup{}, err
	}
	entries, err := os.ReadDir(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return "", HostGroup{}, os.ErrNotExist
	}
	if err != nil {
		return "", HostGroup{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(resolved, entry.Name())
		if err := requireRegularGroupFragment(path); err != nil {
			return "", HostGroup{}, err
		}
		fragment, readErr := readGroupFragment(path)
		if readErr != nil {
			return "", HostGroup{}, readErr
		}
		if fragment.Groups[0].Name == name {
			return path, fragment.Groups[0], nil
		}
	}
	return "", HostGroup{}, os.ErrNotExist
}

func requireRegularGroupFragment(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat group fragment %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("group fragment is not a regular file: %s", path)
	}
	return nil
}

func GroupFragmentPath(dir, name string) (string, error) {
	path, _, err := findGroupFragment(dir, name)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("group %q is not managed by groups.d; edit the main config instead", name)
	}
	return path, err
}

func CreateGroupFragment(dir string, group HostGroup) (string, error) {
	group.Name = strings.TrimSpace(group.Name)
	if err := normalizeGroups([]HostGroup{group}); err != nil {
		return "", err
	}
	existing, err := LoadGroupFragments(dir)
	if err != nil {
		return "", err
	}
	for _, candidate := range existing {
		if candidate.Name == group.Name {
			return "", fmt.Errorf("group %q already exists", group.Name)
		}
	}
	resolved, err := resolveGroupsDir(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(resolved, 0o700); err != nil {
		return "", fmt.Errorf("create groups directory: %w", err)
	}
	path := filepath.Join(resolved, groupFragmentFilename(group.Name))
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("group fragment path already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := writeGroupFragment(path, group); err != nil {
		return "", err
	}
	return path, nil
}

func UpdateGroupFragment(dir, currentName string, group HostGroup) (string, error) {
	currentPath, _, err := findGroupFragment(dir, currentName)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("group %q is not managed by groups.d; edit the main config instead", currentName)
	}
	if err != nil {
		return "", err
	}
	group.Name = strings.TrimSpace(group.Name)
	if err := normalizeGroups([]HostGroup{group}); err != nil {
		return "", err
	}
	groups, err := LoadGroupFragments(dir)
	if err != nil {
		return "", err
	}
	for _, candidate := range groups {
		if candidate.Name == group.Name && candidate.Name != currentName {
			return "", fmt.Errorf("group %q already exists", group.Name)
		}
	}
	newPath := filepath.Join(filepath.Dir(currentPath), groupFragmentFilename(group.Name))
	if newPath != currentPath {
		if _, err := os.Lstat(newPath); err == nil {
			return "", fmt.Errorf("group fragment path already exists: %s", newPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err := writeGroupFragment(newPath, group); err != nil {
		return "", err
	}
	if newPath != currentPath {
		if err := os.Remove(currentPath); err != nil {
			return "", fmt.Errorf("remove renamed group fragment %s: %w", currentPath, err)
		}
	}
	return newPath, nil
}

func DeleteGroupFragment(dir, name string) error {
	path, _, err := findGroupFragment(dir, name)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("group %q is not managed by groups.d; edit the main config instead", name)
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete group fragment %s: %w", path, err)
	}
	return nil
}

func SetGroupMember(dir, name, hostID string, present bool) (string, error) {
	_, group, err := findGroupFragment(dir, name)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("group %q is not managed by groups.d; edit the main config instead", name)
	}
	if err != nil {
		return "", err
	}
	hostID = strings.TrimSpace(hostID)
	if hostID == "" || containsControl(hostID) {
		return "", fmt.Errorf("safe host ID is required")
	}
	members := make([]string, 0, len(group.Members)+1)
	for _, member := range group.Members {
		if member != hostID {
			members = append(members, member)
		}
	}
	if present {
		members = append(members, hostID)
	}
	sort.Strings(members)
	group.Members = members
	return UpdateGroupFragment(dir, name, group)
}

func writeGroupFragment(path string, group HostGroup) error {
	data, err := toml.Marshal(groupFragment{Version: CurrentVersion, Groups: []HostGroup{group}})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".group-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace group fragment %s: %w", path, err)
	}
	return nil
}

func normalizeGroups(groups []HostGroup) error {
	groupNames := make(map[string]struct{}, len(groups))
	for i := range groups {
		group := &groups[i]
		group.Name = strings.TrimSpace(group.Name)
		if group.Name == "" || containsControl(group.Name) {
			return fmt.Errorf("groups[%d]: safe name is required", i)
		}
		if _, exists := groupNames[group.Name]; exists {
			return fmt.Errorf("duplicate group %q", group.Name)
		}
		groupNames[group.Name] = struct{}{}
		for memberIndex, member := range group.Members {
			group.Members[memberIndex] = strings.TrimSpace(member)
			if group.Members[memberIndex] == "" || containsControl(group.Members[memberIndex]) {
				return fmt.Errorf("group %q: member[%d] must be non-empty and contain no control characters", group.Name, memberIndex)
			}
		}
		for patternIndex, pattern := range group.Match {
			group.Match[patternIndex] = strings.TrimSpace(pattern)
			if group.Match[patternIndex] == "" || containsControl(group.Match[patternIndex]) {
				return fmt.Errorf("group %q: match[%d] must be non-empty and contain no control characters", group.Name, patternIndex)
			}
			if _, err := filepath.Match(group.Match[patternIndex], "validation"); err != nil {
				return fmt.Errorf("group %q: invalid match %q: %w", group.Name, group.Match[patternIndex], err)
			}
		}
	}
	return nil
}
