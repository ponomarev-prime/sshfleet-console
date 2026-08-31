package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

type sourceFragment struct {
	Source Source `toml:"source"`
}

func DefaultSourcesDir() (string, error) {
	dir, err := platform.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "sshfleet", "sources.d"), nil
}

func LoadSourceFragments(dir string) ([]Source, error) {
	if dir == "" {
		var err error
		dir, err = DefaultSourcesDir()
		if err != nil {
			return nil, err
		}
	}
	dir, err := ExpandPath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read source fragments %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var sources []Source
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read source fragment %s: %w", path, err)
		}
		var fragment sourceFragment
		if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&fragment); err != nil {
			return nil, fmt.Errorf("parse source fragment %s: %w", path, err)
		}
		if strings.TrimSpace(fragment.Source.Name) == "" {
			return nil, fmt.Errorf("source fragment %s: name is required", path)
		}
		sources = append(sources, fragment.Source)
	}
	return sources, nil
}

var safeSourceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// SaveSourceFragment atomically writes one non-secret source declaration.
func SaveSourceFragment(dir string, source Source) (string, error) {
	if !safeSourceName.MatchString(source.Name) {
		return "", fmt.Errorf("unsafe source name %q", source.Name)
	}
	if source.Kind != SourceSSHConfig && source.Kind != SourceLocalConfig && source.Kind != SourceInventory && source.Kind != SourceEncryptedInventory && source.Kind != SourceRemote {
		return "", fmt.Errorf("unsupported source kind %q", source.Kind)
	}
	if source.Kind != SourceRemote && strings.TrimSpace(source.Path) == "" {
		return "", fmt.Errorf("source path is required")
	}
	if source.Kind == SourceRemote && strings.TrimSpace(source.URL) == "" {
		return "", fmt.Errorf("remote source URL is required")
	}
	if dir == "" {
		var err error
		dir, err = DefaultSourcesDir()
		if err != nil {
			return "", err
		}
	}
	dir, err := ExpandPath(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create source directory: %w", err)
	}
	data, err := toml.Marshal(sourceFragment{Source: source})
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".source-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	path := filepath.Join(dir, source.Name+".toml")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("source %q already exists", source.Name)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}
