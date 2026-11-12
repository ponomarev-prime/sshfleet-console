package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const initialAppConfig = `version = 1

[terminal]
# Local workstation shell only. CLI --shell/--shell-arg wins for one run.
# "auto" uses the OS login/default shell and reports the effective executable.
default_shell = "auto"
shell_args = []
# Per-tab in-memory VT history. Allowed: 1..100000 lines.
scrollback_lines = 10000

[app]
# SSH fleet polling; omit max_concurrent to use 2 x available CPU cores.
refresh_interval = "10s"
connect_timeout = "6s"
probe_enabled = true
load_user_ssh_config = true
# Private application groups live separately from source inventories.
# groups_dir = "~/.config/sshfleet/groups.d"

[app.containers]
# Dynamic local targets. No sudo, key copying, or automatic agent forwarding.
enabled = true
runtimes = ["docker", "podman"]
refresh_interval = "2s"
include_stopped = false
shell_policy = "first_available"
shell_priority = ["/bin/bash", "/bin/ash", "/bin/sh"]

[app.ui]
# Main panes at terminal width >= 100 columns.
# HOSTS gets the remainder: 100 - SOURCES - PREVIEW.
# Allowed: SOURCES 8..30, PREVIEW 12..35; together no more than 48.
sources_width_percent = 10
preview_width_percent = 24

# Nested column inside HOSTS: alias/name share (15..60).
# A smaller value leaves more space for the CPU% and MEM% bars.
host_column_percent = 30

# Trusted localhost/local-sshd source example:
# [[sources]]
# name = "workstation"
# kind = "local_config"
# path = "~/.config/sshfleet/local.toml"
`

// EnsureAppConfig returns an editable main-config path, creating a private
// minimal TOML file only when the default config did not exist yet.
func EnsureAppConfig(path string) (string, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(expanded)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("app config is not a regular file: %s", expanded)
		}
		return expanded, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat app config %s: %w", expanded, err)
	}
	if err := os.MkdirAll(filepath.Dir(expanded), 0o700); err != nil {
		return "", fmt.Errorf("create app config directory: %w", err)
	}
	file, err := os.OpenFile(expanded, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create app config %s: %w", expanded, err)
	}
	if _, err := file.WriteString(initialAppConfig); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("initialize app config %s: %w", expanded, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close app config %s: %w", expanded, err)
	}
	return expanded, nil
}
