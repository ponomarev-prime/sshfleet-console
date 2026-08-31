package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
version = 1

[terminal]
default_shell = "fish"
shell_args = ["-l", "значение с пробелом"]

[app]
refresh_interval = "12s"
connect_timeout = "3s"
max_concurrent = 4
probe_enabled = false
load_user_ssh_config = false
sources_dir = "/tmp/sshfleet-sources"
groups_dir = "/tmp/sshfleet-groups"
overrides_dir = "/tmp/sshfleet-hosts"
editor = "nano"
source_fetch_timeout = "8s"
source_max_bytes = 2097152
source_state_dir = "/tmp/sshfleet-source-state"

[app.ui]
sources_width_percent = 12
preview_width_percent = 20
host_column_percent = 28

[[sources]]
name = "lab"
kind = "ssh_config"
path = "/tmp/lab.conf"

[[hosts]]
alias = "lab-01"
name = "Lab One"
tags = ["lab", "linux"]
probe = false
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, used, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if used != path {
		t.Fatalf("used path = %q, want %q", used, path)
	}
	if cfg.App.RefreshInterval.Duration != 12*time.Second {
		t.Fatalf("refresh interval = %s", cfg.App.RefreshInterval.Duration)
	}
	if cfg.Terminal.DefaultShell != "fish" || len(cfg.Terminal.ShellArgs) != 2 || cfg.Terminal.ShellArgs[1] != "значение с пробелом" {
		t.Fatalf("terminal settings = %#v", cfg.Terminal)
	}
	if cfg.App.ProbeEnabled || cfg.App.LoadUserSSHConfig || cfg.App.SourcesDir != "/tmp/sshfleet-sources" || cfg.App.GroupsDir != "/tmp/sshfleet-groups" || cfg.App.OverridesDir != "/tmp/sshfleet-hosts" || cfg.App.Editor != "nano" {
		t.Fatalf("app settings = %#v", cfg.App)
	}
	if cfg.App.SourceFetchTimeout.Duration != 8*time.Second || cfg.App.SourceMaxBytes != 2097152 || cfg.App.SourceStateDir != "/tmp/sshfleet-source-state" {
		t.Fatalf("source app settings = %#v", cfg.App)
	}
	if cfg.App.UI.SourcesWidthPercent != 12 || cfg.App.UI.PreviewWidthPercent != 20 || cfg.App.UI.HostColumnPercent != 28 {
		t.Fatalf("UI settings = %#v", cfg.App.UI)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0].Name != "lab" {
		t.Fatalf("sources = %#v", cfg.Sources)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Probe == nil || *cfg.Hosts[0].Probe {
		t.Fatalf("hosts = %#v", cfg.Hosts)
	}
}

func TestRuntimeDefaultsFavorFastBoundedPolling(t *testing.T) {
	cfg := Defaults()
	if cfg.App.MaxConcurrent != runtime.GOMAXPROCS(0)*2 {
		t.Fatalf("max concurrent = %d", cfg.App.MaxConcurrent)
	}
	if cfg.App.RefreshInterval.Duration != 10*time.Second {
		t.Fatalf("refresh = %s", cfg.App.RefreshInterval.Duration)
	}
	if !cfg.App.ProbeEnabled || !cfg.App.LoadUserSSHConfig {
		t.Fatalf("default app booleans = %#v", cfg.App)
	}
	if !cfg.App.Containers.Enabled || cfg.App.Containers.RefreshInterval.Duration != 2*time.Second || len(cfg.App.Containers.Runtimes) != 2 || cfg.App.Containers.ShellPolicy != ContainerShellPolicyFirstAvailable {
		t.Fatalf("container defaults = %#v", cfg.App.Containers)
	}
	if cfg.App.Editor != DefaultEditor {
		t.Fatalf("default editor = %q", cfg.App.Editor)
	}
	if cfg.Terminal.DefaultShell != "auto" || len(cfg.Terminal.ShellArgs) != 0 {
		t.Fatalf("terminal defaults = %#v", cfg.Terminal)
	}
	if cfg.App.UI.SourcesWidthPercent != DefaultSourcesWidthPercent || cfg.App.UI.PreviewWidthPercent != DefaultPreviewWidthPercent || cfg.App.UI.HostColumnPercent != DefaultHostColumnPercent {
		t.Fatalf("default UI settings = %#v", cfg.App.UI)
	}
}

func TestContainerShellPolicyFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version=1\n[app.containers]\nshell_policy=\"guess\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "shell_policy") {
		t.Fatalf("invalid shell policy error = %v", err)
	}
}

func TestTerminalConfigRejectsShellSyntaxAndUnsafeArgv(t *testing.T) {
	for name, body := range map[string]string{
		"shell-syntax": "version=1\n[terminal]\ndefault_shell=\"sh -c\"\n",
		"empty-arg":    "version=1\n[terminal]\ndefault_shell=\"sh\"\nshell_args=[\"\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "terminal") {
				t.Fatalf("unsafe terminal config error = %v", err)
			}
		})
	}
}

func TestTrustedLocalConfigSeparatesDirectAndSSHLocalhost(t *testing.T) {
	document, err := ParseLocalConfig([]byte(`version = 1
[[local_hosts]]
alias = "this-machine"
mode = "direct"
shell = "/bin/sh"
shell_args = ["-l"]
working_directory = "~/my_code"

[[local_hosts]]
alias = "local-sshd"
mode = "ssh"
host = "127.0.0.1"
port = 2222
user = "operator"
shell = "/bin/bash"
`), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Hosts) != 2 || document.Hosts[0].Mode != LocalModeDirect || document.Hosts[1].Port != 2222 {
		t.Fatalf("local config = %#v", document)
	}
	for name, data := range map[string]string{
		"unknown-field": `version=1
[[local_hosts]]
alias="bad"
mode="direct"
shell="/bin/sh"
command="oops"`,
		"direct-secret": `version=1
[[local_hosts]]
alias="bad"
mode="direct"
shell="/bin/sh"
credential="secret"`,
		"unsafe-shell": `version=1
[[local_hosts]]
alias="bad"
mode="direct"
shell="sh -c"`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseLocalConfig([]byte(data), name); err == nil {
				t.Fatal("unsafe local config was accepted")
			}
		})
	}
}

func TestTrustedDirectLocalConfigMayInheritGlobalTerminal(t *testing.T) {
	document, err := ParseLocalConfig([]byte(`version = 1
[[local_hosts]]
alias = "inherited-shell"
mode = "direct"
working_directory = "/tmp"
`), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Hosts) != 1 || document.Hosts[0].Shell != "" {
		t.Fatalf("local config = %#v", document)
	}
}

func TestEnsureAppConfigCreatesPrivateMinimalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	created, err := EnsureAppConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if created != path {
		t.Fatalf("created path = %q", created)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != initialAppConfig || !strings.Contains(string(data), "HOSTS gets the remainder") || !strings.Contains(string(data), "host_column_percent = 30") || !strings.Contains(string(data), "default_shell = \"auto\"") {
		t.Fatalf("contents = %q", data)
	}
	if _, err := EnsureAppConfig(path); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestUIWidthsRejectUnsafeLayouts(t *testing.T) {
	tests := []struct {
		name, settings, want string
	}{
		{"sources-too-small", "sources_width_percent = 4\npreview_width_percent = 18", "sources_width_percent"},
		{"preview-too-large", "sources_width_percent = 10\npreview_width_percent = 40", "preview_width_percent"},
		{"host-column-too-small", "host_column_percent = 10", "host_column_percent"},
		{"side-panes-dominate", "sources_width_percent = 20\npreview_width_percent = 30", "at most 48"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			data := "version = 1\n[app.ui]\n" + tt.settings + "\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMainConfigRejectsUnknownSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 1\n[app]\nmax_concurent = 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Fatalf("unknown setting error = %v", err)
	}
}

func TestLoadPreservesTrueAppDefaultsWhenFieldsAreOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 1\n[app]\nmax_concurrent = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.App.ProbeEnabled || !cfg.App.LoadUserSSHConfig {
		t.Fatalf("omitted app booleans lost defaults: %#v", cfg.App)
	}
}

func TestExplicitMissingConfigFails(t *testing.T) {
	_, _, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatal("expected error for explicitly requested missing config")
	}
}

func TestHostOverrideIsSeparateAndReloadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hosts.d")
	path, err := EnsureHostOverride(dir, "default", "password-node-1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("override mode = %o, want 600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `alias = 'password-node-1'`) || !strings.Contains(string(data), `source = 'default'`) {
		t.Fatalf("unexpected template:\n%s", data)
	}
	data = append(data, []byte("name = 'Password test node'\nprobe = false\n")...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	overrides, err := LoadHostOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 || overrides[0].Name != "Password test node" || overrides[0].Probe == nil || *overrides[0].Probe {
		t.Fatalf("overrides = %#v", overrides)
	}
	again, err := EnsureHostOverride(dir, "default", "password-node-1")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("second path = %q, want %q", again, path)
	}
}

func TestMissingHostOverridesDirectoryIsEmpty(t *testing.T) {
	overrides, err := LoadHostOverrides(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 0 {
		t.Fatalf("overrides = %#v", overrides)
	}
}

func TestRestrictedInventoryRejectsSecretsAndCommands(t *testing.T) {
	for _, field := range []string{`password = "secret"`, `proxy_command = "curl evil"`} {
		t.Run(field, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "inventory.toml")
			data := "version = 1\n[[hosts]]\nalias = \"demo\"\n" + field + "\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadInventory(path); err == nil {
				t.Fatalf("expected %q to be rejected", field)
			}
		})
	}
}

func TestSourceFragmentRoundTripIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sources.d")
	path, err := SaveSourceFragment(dir, Source{Name: "lab", Kind: "inventory", Path: "/tmp/lab.toml"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	sources, err := LoadSourceFragments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "lab" || sources[0].Kind != "inventory" {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestRemoteSourceRejectsLocalPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `version = 1
[[sources]]
name = "remote"
kind = "remote"
url = "https://inventory.example/fleet.age"
signing_key = "age-signature-key-reference"
age_identity_ref = "secret-service:sshfleet/age"
path = "/tmp/remote-ssh-config"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "remote ssh_config is forbidden") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeAcceptsEverySourceKindAndSeparatesBearerCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `version = 1
[[credentials]]
name = "remote-token"
type = "bearer"
provider = "secret-service"
key = "sshfleet/http/central"

[[sources]]
name = "openssh"
kind = "ssh_config"
path = "/tmp/ssh_config"

[[sources]]
name = "plain"
kind = "inventory"
path = "/tmp/inventory.toml"

[[sources]]
name = "sealed"
kind = "encrypted"
path = "/tmp/sealed-bundle"
signing_key = "/tmp/allowed_signers"
age_identity_ref = "secret-service:ssfleet/age/sealed"

[[sources]]
name = "central"
kind = "remote"
url = "https://inventory.example/fleet/"
auth_credential = "remote-token"
signing_key = "/tmp/allowed_signers"
age_identity_ref = "age-plugin:/tmp/age-plugin-identity"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 4 || cfg.Sources[2].Kind != SourceEncryptedInventory || cfg.Sources[3].Kind != SourceRemote {
		t.Fatalf("sources = %#v", cfg.Sources)
	}

	cfg.HostRules = []HostRule{{Match: "*", Credential: "remote-token"}}
	if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "must have type password") {
		t.Fatalf("bearer host rule error = %v", err)
	}
}

func TestLogicalIdentityRequiresKeyPassphraseCredential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := `version = 1
[[credentials]]
name = "secure-key"
type = "key-passphrase"
provider = "secret-service"
key = "sshfleet/keys/secure"

[[identities]]
name = "secure-identity"
path = "~/.ssh/secure-key"
credential = "secure-key"

[[hosts]]
alias = "secure-202"
hostname = "192.0.2.202"
identity = "secure-identity"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Identities) != 1 || cfg.Identities[0].Credential != "secure-key" || cfg.Hosts[0].Identity != "secure-identity" {
		t.Fatalf("identity config = %#v / %#v", cfg.Identities, cfg.Hosts)
	}
}

func TestGroupsCommandsAndEditorPriorityAreStrictlyParsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	data := `version = 1
[app]
editor_priority = ["nvim", "vim", "nano"]

[[groups]]
name = "mixed"
members = ["user:alpha", "lab:beta"]
match = ["remote:prod-*"]

[[command_presets]]
name = "uptime"
argv = ["uptime", "--pretty"]
timeout = "12s"
max_concurrent = 3
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups) != 1 || len(cfg.Commands) != 1 || cfg.Commands[0].Timeout.Duration != 12*time.Second {
		t.Fatalf("groups/commands = %#v / %#v", cfg.Groups, cfg.Commands)
	}
	if got := strings.Join(cfg.App.EditorPriority, ","); got != "nvim,vim,nano" {
		t.Fatalf("editor priority = %q", got)
	}

	cfg.Commands[0].Argv = []string{"printf", "bad\nargument"}
	if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "control-bearing") {
		t.Fatalf("unsafe command error = %v", err)
	}
}

func TestLoadRejectsControlCharactersInGroupsAndCommands(t *testing.T) {
	for _, body := range []string{
		"version = 1\n[[groups]]\nname = \"bad\\u001bgroup\"\nmembers = [\"user:ok\"]\n",
		"version = 1\n[[groups]]\nname = \"ok\"\nmembers = [\"user:bad\\u0007\"]\n",
		"version = 1\n[[command_presets]]\nname = \"bad\\u001bcommand\"\nargv = [\"uptime\"]\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(path); err == nil {
			t.Fatalf("expected control-bearing config to fail: %q", body)
		}
	}
}
