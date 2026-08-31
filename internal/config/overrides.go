package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

type hostOverrideFile struct {
	Version int          `toml:"version"`
	Hosts   []HostConfig `toml:"hosts"`
}

func DefaultOverridesDir() (string, error) {
	dir, err := platform.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "sshfleet", "hosts.d"), nil
}

// LoadHostOverrides reads the independently editable host layers in lexical
// filename order. Missing directories are an empty layer, not an error.
func LoadHostOverrides(dir string) ([]HostConfig, error) {
	if strings.TrimSpace(dir) == "" {
		var err error
		dir, err = DefaultOverridesDir()
		if err != nil {
			return nil, err
		}
	}
	expanded, err := ExpandPath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(expanded)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read host overrides directory %s: %w", expanded, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var hosts []HostConfig
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".toml" {
			continue
		}
		path := filepath.Join(expanded, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect host override %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("host override %s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read host override %s: %w", path, err)
		}
		var layer hostOverrideFile
		if err := toml.Unmarshal(data, &layer); err != nil {
			return nil, fmt.Errorf("parse host override %s: %w", path, err)
		}
		if layer.Version == 0 {
			layer.Version = CurrentVersion
		}
		if layer.Version != CurrentVersion {
			return nil, fmt.Errorf("host override %s: unsupported version %d", path, layer.Version)
		}
		if err := normalizeHosts(layer.Hosts); err != nil {
			return nil, fmt.Errorf("host override %s: %w", path, err)
		}
		hosts = append(hosts, layer.Hosts...)
	}
	return hosts, nil
}

// EnsureHostOverride creates one isolated TOML layer for a selected imported
// host and never rewrites an existing layer.
func EnsureHostOverride(dir, source, alias string) (string, error) {
	if err := normalizeHosts([]HostConfig{{Alias: alias, Source: source}}); err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		var err error
		dir, err = DefaultOverridesDir()
		if err != nil {
			return "", err
		}
	}
	expanded, err := ExpandPath(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(expanded, 0o700); err != nil {
		return "", fmt.Errorf("create host overrides directory %s: %w", expanded, err)
	}
	path := filepath.Join(expanded, overrideFilename(source, alias))
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("host override %s is not a regular file", path)
		}
		return path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect host override %s: %w", path, err)
	}

	layer := hostOverrideFile{
		Version: CurrentVersion,
		Hosts:   []HostConfig{{Alias: alias, Source: source}},
	}
	data, err := toml.Marshal(layer)
	if err != nil {
		return "", fmt.Errorf("encode host override: %w", err)
	}
	header := "# SSH Fleet Console host overlay. The imported OpenSSH config is never edited.\n" +
		"# alias/source select the base host. Optional keys: name, hostname, user,\n" +
		"# port, proxy_jump, tags, probe. Use name to change only the visible label.\n\n"
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create host override %s: %w", path, err)
	}
	if _, err := file.Write(append([]byte(header), data...)); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write host override %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close host override %s: %w", path, err)
	}
	return path, nil
}

func overrideFilename(source, alias string) string {
	id := source + "\x00" + alias
	digest := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%s--%s--%x.toml", safeFilenamePart(source), safeFilenamePart(alias), digest[:4])
}

func safeFilenamePart(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r) {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
		if out.Len() >= 48 {
			break
		}
	}
	if out.Len() == 0 {
		return "host"
	}
	return out.String()
}
