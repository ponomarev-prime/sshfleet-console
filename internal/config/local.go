package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	LocalModeDirect = "direct"
	LocalModeSSH    = "ssh"
)

// LocalConfig is trusted local configuration. It is deliberately distinct
// from the restricted inventory schema: a remote source must never be able to
// select a workstation executable or working directory.
type LocalConfig struct {
	Version int               `toml:"version"`
	Hosts   []LocalHostConfig `toml:"local_hosts"`
}

type LocalHostConfig struct {
	Alias            string   `toml:"alias"`
	Name             string   `toml:"name,omitempty"`
	Mode             string   `toml:"mode"`
	Shell            string   `toml:"shell,omitempty"`
	ShellArgs        []string `toml:"shell_args,omitempty"`
	WorkingDirectory string   `toml:"working_directory,omitempty"`
	Host             string   `toml:"host,omitempty"`
	Port             int      `toml:"port,omitempty"`
	User             string   `toml:"user,omitempty"`
	Identity         string   `toml:"identity,omitempty"`
	Credential       string   `toml:"credential,omitempty"`
	Tags             []string `toml:"tags,omitempty"`
	Probe            *bool    `toml:"probe,omitempty"`
}

func LoadLocalConfig(path string) (LocalConfig, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return LocalConfig{}, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return LocalConfig{}, fmt.Errorf("read local config %s: %w", expanded, err)
	}
	return ParseLocalConfig(data, expanded)
}

func ParseLocalConfig(data []byte, label string) (LocalConfig, error) {
	var document LocalConfig
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&document); err != nil {
		return LocalConfig{}, fmt.Errorf("parse trusted local config %s: %w", label, err)
	}
	if document.Version == 0 {
		document.Version = CurrentVersion
	}
	if document.Version != CurrentVersion {
		return LocalConfig{}, fmt.Errorf("unsupported local config version %d", document.Version)
	}
	seen := make(map[string]struct{}, len(document.Hosts))
	for i := range document.Hosts {
		host := &document.Hosts[i]
		host.Alias = strings.TrimSpace(host.Alias)
		host.Mode = strings.TrimSpace(host.Mode)
		if host.Alias == "" || strings.HasPrefix(host.Alias, "-") || containsControl(host.Alias) {
			return LocalConfig{}, fmt.Errorf("local_hosts[%d]: safe alias is required", i)
		}
		if _, exists := seen[host.Alias]; exists {
			return LocalConfig{}, fmt.Errorf("duplicate local host alias %q", host.Alias)
		}
		seen[host.Alias] = struct{}{}
		if host.Mode == "" {
			host.Mode = LocalModeDirect
		}
		switch host.Mode {
		case LocalModeDirect:
			if strings.TrimSpace(host.Shell) != "" && !safeExecutable(strings.TrimSpace(host.Shell)) {
				return LocalConfig{}, fmt.Errorf("local host %q: shell must be one executable; omit it to inherit [terminal]", host.Alias)
			}
			host.Shell = strings.TrimSpace(host.Shell)
			if host.Host != "" || host.Port != 0 || host.User != "" || host.Identity != "" || host.Credential != "" {
				return LocalConfig{}, fmt.Errorf("local host %q: SSH routing and credentials are forbidden in direct mode", host.Alias)
			}
		case LocalModeSSH:
			if strings.TrimSpace(host.Host) == "" {
				return LocalConfig{}, fmt.Errorf("local host %q: ssh mode requires host", host.Alias)
			}
			if host.Port == 0 {
				host.Port = 22
			}
			if host.Port < 1 || host.Port > 65535 {
				return LocalConfig{}, fmt.Errorf("local host %q: invalid port %d", host.Alias, host.Port)
			}
		default:
			return LocalConfig{}, fmt.Errorf("local host %q: mode must be direct or ssh", host.Alias)
		}
		for argIndex, arg := range host.ShellArgs {
			if arg == "" || containsControl(arg) {
				return LocalConfig{}, fmt.Errorf("local host %q shell_args[%d] is empty or contains control characters", host.Alias, argIndex)
			}
		}
		if containsControl(host.WorkingDirectory) || containsControl(host.Name) {
			return LocalConfig{}, fmt.Errorf("local host %q contains control characters", host.Alias)
		}
	}
	return document, nil
}
