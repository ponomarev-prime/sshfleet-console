package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/pelletier/go-toml/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

const CurrentVersion = 1

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	Version     int            `toml:"version"`
	App         AppConfig      `toml:"app"`
	Terminal    TerminalConfig `toml:"terminal"`
	Sources     []Source       `toml:"sources"`
	Credentials []Credential   `toml:"credentials"`
	Identities  []Identity     `toml:"identities"`
	HostRules   []HostRule     `toml:"host_rules"`
	Hosts       []HostConfig   `toml:"hosts"`
	Groups      []HostGroup    `toml:"groups"`
	Commands    []Command      `toml:"command_presets"`
}

// TerminalConfig controls only commands started on the local workstation.
// Remote SSH shells and container shell discovery have separate policies.
type TerminalConfig struct {
	DefaultShell    string   `toml:"default_shell"`
	ShellArgs       []string `toml:"shell_args,omitempty"`
	EffectiveShell  string   `toml:"-"`
	EffectiveArgs   []string `toml:"-"`
	EffectiveOrigin string   `toml:"-"`
}

type AppConfig struct {
	RefreshInterval    Duration        `toml:"refresh_interval"`
	ConnectTimeout     Duration        `toml:"connect_timeout"`
	MaxConcurrent      int             `toml:"max_concurrent"`
	SSHBinary          string          `toml:"ssh_binary"`
	ProbeEnabled       bool            `toml:"probe_enabled"`
	LoadUserSSHConfig  bool            `toml:"load_user_ssh_config"`
	SourcesDir         string          `toml:"sources_dir,omitempty"`
	GroupsDir          string          `toml:"groups_dir,omitempty"`
	OverridesDir       string          `toml:"overrides_dir,omitempty"`
	Editor             string          `toml:"editor,omitempty"`
	EditorPriority     []string        `toml:"editor_priority,omitempty"`
	WorkspaceBundle    string          `toml:"workspace_bundle,omitempty"`
	WorkspaceCleanup   bool            `toml:"workspace_cleanup"`
	SourceFetchTimeout Duration        `toml:"source_fetch_timeout"`
	SourceMaxBytes     int64           `toml:"source_max_bytes"`
	SourceStateDir     string          `toml:"source_state_dir,omitempty"`
	Containers         ContainerConfig `toml:"containers"`
	UI                 UIConfig        `toml:"ui"`
}

type ContainerConfig struct {
	Enabled         bool     `toml:"enabled"`
	Runtimes        []string `toml:"runtimes,omitempty"`
	RefreshInterval Duration `toml:"refresh_interval,omitempty"`
	IncludeStopped  bool     `toml:"include_stopped"`
	ShellPolicy     string   `toml:"shell_policy,omitempty"`
	ShellPriority   []string `toml:"shell_priority,omitempty"`
}

const ContainerShellPolicyFirstAvailable = "first_available"

type UIConfig struct {
	SourcesWidthPercent int `toml:"sources_width_percent"`
	PreviewWidthPercent int `toml:"preview_width_percent"`
	HostColumnPercent   int `toml:"host_column_percent"`
}

const (
	DefaultSourcesWidthPercent = 10
	DefaultPreviewWidthPercent = 24
	DefaultHostColumnPercent   = 30
	DefaultEditor              = ""
)

type Source struct {
	Name           string `toml:"name"`
	Kind           string `toml:"kind"`
	Path           string `toml:"path,omitempty"`
	URL            string `toml:"url,omitempty"`
	AuthCredential string `toml:"auth_credential,omitempty"`
	SigningKey     string `toml:"signing_key,omitempty"`
	AgeIdentityRef string `toml:"age_identity_ref,omitempty"`
}

const (
	SourceSSHConfig          = "ssh_config"
	SourceInventory          = "inventory"
	SourceEncryptedInventory = "encrypted_inventory"
	SourceRemote             = "remote"
	SourceLocalConfig        = "local_config"
)

type Credential struct {
	Name     string `toml:"name"`
	Type     string `toml:"type,omitempty"`
	Provider string `toml:"provider"`
	Key      string `toml:"key"`
}

const (
	CredentialPassword      = "password"
	CredentialBearer        = "bearer"
	CredentialKeyPassphrase = "key-passphrase"
)

// Identity binds a shareable logical name to one local encrypted OpenSSH key.
// Restricted and remote inventories may reference Name, but cannot declare or
// override Path, keeping workstation paths outside the shared trust boundary.
type Identity struct {
	Name       string `toml:"name"`
	Path       string `toml:"path"`
	Credential string `toml:"credential"`
}

type HostRule struct {
	Source     string `toml:"source,omitempty"`
	Match      string `toml:"match"`
	Credential string `toml:"credential,omitempty"`
}

type GroupConfig struct {
	Name       string   `toml:"name"`
	Match      string   `toml:"match"`
	Tags       []string `toml:"tags,omitempty"`
	Probe      *bool    `toml:"probe,omitempty"`
	Credential string   `toml:"credential,omitempty"`
}

type HostConfig struct {
	Alias      string   `toml:"alias"`
	Name       string   `toml:"name,omitempty"`
	Source     string   `toml:"source,omitempty"`
	Hostname   string   `toml:"hostname,omitempty"`
	User       string   `toml:"user,omitempty"`
	Port       int      `toml:"port,omitempty"`
	ProxyJump  string   `toml:"proxy_jump,omitempty"`
	Tags       []string `toml:"tags,omitempty"`
	Probe      *bool    `toml:"probe,omitempty"`
	Credential string   `toml:"credential,omitempty"`
	Identity   string   `toml:"identity,omitempty"`
}

// HostGroup is a private application-layer grouping. Members are stable
// source:alias IDs or aliases; Match entries are filepath patterns applied to
// both forms. Groups never modify their underlying sources.
type HostGroup struct {
	Name    string   `toml:"name"`
	Members []string `toml:"members,omitempty"`
	Match   []string `toml:"match,omitempty"`
}

// Command is a locally trusted, non-secret argv preset for bounded execution
// over a selected group. Restricted and remote inventories cannot define it.
type Command struct {
	Name          string   `toml:"name"`
	Argv          []string `toml:"argv"`
	Timeout       Duration `toml:"timeout,omitempty"`
	MaxConcurrent int      `toml:"max_concurrent,omitempty"`
}

func Defaults() Config {
	return Config{
		Version:  CurrentVersion,
		Terminal: TerminalConfig{DefaultShell: platform.ShellAuto},
		App: AppConfig{
			RefreshInterval:    Duration{Duration: 10 * time.Second},
			ConnectTimeout:     Duration{Duration: 6 * time.Second},
			MaxConcurrent:      DefaultMaxConcurrent(),
			SSHBinary:          "ssh",
			ProbeEnabled:       true,
			LoadUserSSHConfig:  true,
			SourceFetchTimeout: Duration{Duration: 10 * time.Second},
			SourceMaxBytes:     4 << 20,
			Editor:             DefaultEditor,
			EditorPriority:     []string{"nvim", "vim", "nano"},
			WorkspaceCleanup:   true,
			Containers: ContainerConfig{
				Enabled:         true,
				RefreshInterval: Duration{Duration: 2 * time.Second},
				Runtimes:        []string{"docker", "podman"},
				ShellPolicy:     ContainerShellPolicyFirstAvailable,
				ShellPriority:   []string{"/bin/bash", "/bin/ash", "/bin/sh"},
			},
			UI: UIConfig{
				SourcesWidthPercent: DefaultSourcesWidthPercent,
				PreviewWidthPercent: DefaultPreviewWidthPercent,
				HostColumnPercent:   DefaultHostColumnPercent,
			},
		},
	}
}

func DefaultMaxConcurrent() int { return max(1, runtime.GOMAXPROCS(0)*2) }

func DefaultPath() (string, error) {
	dir, err := platform.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "sshfleet", "config.toml"), nil
}

// Load returns defaults when the default config file does not exist. An explicitly
// requested path must exist so typos do not silently change the active inventory.
func Load(path string) (Config, string, error) {
	explicit := path != ""
	if !explicit {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, "", err
		}
	}

	expanded, err := ExpandPath(path)
	if err != nil {
		return Config{}, "", err
	}
	data, err := os.ReadFile(expanded)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		cfg := Defaults()
		return cfg, expanded, nil
	}
	if err != nil {
		return Config{}, expanded, fmt.Errorf("read TOML config %s: %w", expanded, err)
	}

	cfg := Defaults()
	// Slices must not inherit default entries when the file declares its own.
	cfg.Sources = nil
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&cfg); err != nil {
		return Config{}, expanded, fmt.Errorf("parse TOML config %s: %w", expanded, err)
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, expanded, err
	}
	return cfg, expanded, nil
}

func (c *Config) normalize() error {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported inventory version %d; expected %d", c.Version, CurrentVersion)
	}
	if c.App.RefreshInterval.Duration <= 0 {
		c.App.RefreshInterval.Duration = 10 * time.Second
	}
	if c.App.ConnectTimeout.Duration <= 0 {
		c.App.ConnectTimeout.Duration = 6 * time.Second
	}
	if c.App.MaxConcurrent <= 0 {
		c.App.MaxConcurrent = DefaultMaxConcurrent()
	}
	if err := c.Terminal.normalize(); err != nil {
		return err
	}
	if strings.TrimSpace(c.App.SSHBinary) == "" {
		c.App.SSHBinary = "ssh"
	}
	if c.App.SourceFetchTimeout.Duration <= 0 {
		c.App.SourceFetchTimeout.Duration = 10 * time.Second
	}
	if c.App.SourceMaxBytes <= 0 {
		c.App.SourceMaxBytes = 4 << 20
	}
	if len(c.App.EditorPriority) == 0 {
		c.App.EditorPriority = []string{"nvim", "vim", "nano"}
	}
	if c.App.Containers.RefreshInterval.Duration <= 0 {
		c.App.Containers.RefreshInterval.Duration = 2 * time.Second
	}
	if len(c.App.Containers.Runtimes) == 0 {
		c.App.Containers.Runtimes = []string{"docker", "podman"}
	}
	for i, runtimeName := range c.App.Containers.Runtimes {
		runtimeName = strings.TrimSpace(runtimeName)
		if runtimeName != "docker" && runtimeName != "podman" {
			return fmt.Errorf("app.containers.runtimes[%d] must be docker or podman", i)
		}
		c.App.Containers.Runtimes[i] = runtimeName
	}
	if len(c.App.Containers.ShellPriority) == 0 {
		c.App.Containers.ShellPriority = []string{"/bin/bash", "/bin/ash", "/bin/sh"}
	}
	c.App.Containers.ShellPolicy = strings.TrimSpace(c.App.Containers.ShellPolicy)
	if c.App.Containers.ShellPolicy == "" {
		c.App.Containers.ShellPolicy = ContainerShellPolicyFirstAvailable
	}
	if c.App.Containers.ShellPolicy != ContainerShellPolicyFirstAvailable {
		return fmt.Errorf("app.containers.shell_policy must be %q", ContainerShellPolicyFirstAvailable)
	}
	for i, shell := range c.App.Containers.ShellPriority {
		shell = strings.TrimSpace(shell)
		if !safeExecutable(shell) || !strings.HasPrefix(shell, "/") {
			return fmt.Errorf("app.containers.shell_priority[%d] must be an absolute executable path", i)
		}
		c.App.Containers.ShellPriority[i] = shell
	}
	c.App.EditorPriority = prependUnique(c.App.Editor, c.App.EditorPriority)
	for i, editor := range c.App.EditorPriority {
		if !safeExecutable(editor) {
			return fmt.Errorf("app.editor_priority[%d] must be one executable without whitespace or control characters", i)
		}
	}
	if err := c.App.UI.normalize(); err != nil {
		return err
	}
	if err := normalizeGroups(c.Groups); err != nil {
		return err
	}
	commandNames := make(map[string]struct{}, len(c.Commands))
	for i := range c.Commands {
		command := &c.Commands[i]
		command.Name = strings.TrimSpace(command.Name)
		if command.Name == "" || containsControl(command.Name) || len(command.Argv) == 0 {
			return fmt.Errorf("command_presets[%d]: name and argv are required", i)
		}
		if _, exists := commandNames[command.Name]; exists {
			return fmt.Errorf("duplicate command preset %q", command.Name)
		}
		commandNames[command.Name] = struct{}{}
		for argIndex, arg := range command.Argv {
			if arg == "" || containsControl(arg) {
				return fmt.Errorf("command preset %q argv[%d]: empty or control-bearing arguments are forbidden", command.Name, argIndex)
			}
		}
		if command.Timeout.Duration <= 0 {
			command.Timeout.Duration = 30 * time.Second
		}
		if command.MaxConcurrent < 0 {
			return fmt.Errorf("command preset %q: max_concurrent cannot be negative", command.Name)
		}
	}
	seen := make(map[string]struct{}, len(c.Sources))
	for i := range c.Sources {
		s := &c.Sources[i]
		s.Name = strings.TrimSpace(s.Name)
		s.Kind = strings.TrimSpace(s.Kind)
		if s.Name == "" {
			s.Name = fmt.Sprintf("source-%d", i+1)
		}
		if _, ok := seen[s.Name]; ok {
			return fmt.Errorf("duplicate source name %q", s.Name)
		}
		seen[s.Name] = struct{}{}
		if s.Kind == "openssh" {
			s.Kind = SourceSSHConfig
		}
		if s.Kind == "" {
			s.Kind = SourceSSHConfig
		}
		if s.Kind == "encrypted" {
			s.Kind = SourceEncryptedInventory
		}
		if s.Kind == "local" {
			s.Kind = SourceLocalConfig
		}
		if s.Kind != SourceSSHConfig && s.Kind != SourceInventory && s.Kind != SourceEncryptedInventory && s.Kind != SourceRemote && s.Kind != SourceLocalConfig {
			return fmt.Errorf("source %q: unsupported kind %q", s.Name, s.Kind)
		}
		if s.Kind == SourceRemote {
			if s.Path != "" {
				return fmt.Errorf("source %q: remote ssh_config is forbidden", s.Name)
			}
			if !strings.HasPrefix(s.URL, "https://") || s.SigningKey == "" || s.AgeIdentityRef == "" {
				return fmt.Errorf("source %q: remote inventory requires https url, signing_key, and age_identity_ref", s.Name)
			}
			if !strings.HasPrefix(s.AgeIdentityRef, "secret-service:") && !strings.HasPrefix(s.AgeIdentityRef, "age-plugin:") {
				return fmt.Errorf("source %q: age_identity_ref must use secret-service: or age-plugin:", s.Name)
			}
			continue
		}
		if s.Path == "" {
			return fmt.Errorf("source %q: path is required", s.Name)
		}
		if s.Kind == SourceEncryptedInventory {
			if s.SigningKey == "" || s.AgeIdentityRef == "" {
				return fmt.Errorf("source %q: encrypted inventory requires signing_key and age_identity_ref", s.Name)
			}
			if !validAgeIdentityRef(s.AgeIdentityRef) {
				return fmt.Errorf("source %q: age_identity_ref must use secret-service: or age-plugin:", s.Name)
			}
		}
	}
	credentialNames := make(map[string]Credential, len(c.Credentials))
	for i := range c.Credentials {
		credential := &c.Credentials[i]
		credential.Name = strings.TrimSpace(credential.Name)
		credential.Provider = strings.TrimSpace(credential.Provider)
		credential.Key = strings.TrimSpace(credential.Key)
		if credential.Type == "" {
			credential.Type = CredentialPassword
		}
		if credential.Name == "" || credential.Provider != "secret-service" || credential.Key == "" ||
			(credential.Type != CredentialPassword && credential.Type != CredentialBearer && credential.Type != CredentialKeyPassphrase) {
			return fmt.Errorf("credentials[%d]: only named password, key-passphrase, or bearer credentials from secret-service with a key are supported", i)
		}
		if _, exists := credentialNames[credential.Name]; exists {
			return fmt.Errorf("duplicate credential %q", credential.Name)
		}
		credentialNames[credential.Name] = *credential
	}
	identityNames := make(map[string]struct{}, len(c.Identities))
	for i := range c.Identities {
		identity := &c.Identities[i]
		identity.Name = strings.TrimSpace(identity.Name)
		identity.Path = strings.TrimSpace(identity.Path)
		identity.Credential = strings.TrimSpace(identity.Credential)
		if identity.Name == "" || identity.Path == "" || identity.Credential == "" {
			return fmt.Errorf("identities[%d]: name, path, and credential are required", i)
		}
		if _, exists := identityNames[identity.Name]; exists {
			return fmt.Errorf("duplicate identity %q", identity.Name)
		}
		credential, ok := credentialNames[identity.Credential]
		if !ok || credential.Type != CredentialKeyPassphrase {
			return fmt.Errorf("identity %q: credential %q must have type key-passphrase", identity.Name, identity.Credential)
		}
		identityNames[identity.Name] = struct{}{}
	}
	for _, source := range c.Sources {
		if source.AuthCredential == "" {
			continue
		}
		if source.Kind != SourceRemote {
			return fmt.Errorf("source %q: auth_credential is only valid for remote inventory", source.Name)
		}
		credential, ok := credentialNames[source.AuthCredential]
		if !ok {
			return fmt.Errorf("source %q: unknown auth credential %q", source.Name, source.AuthCredential)
		}
		if credential.Type != CredentialBearer {
			return fmt.Errorf("source %q: auth credential %q must have type bearer", source.Name, source.AuthCredential)
		}
	}
	for i, rule := range c.HostRules {
		if strings.TrimSpace(rule.Match) == "" {
			return fmt.Errorf("host_rules[%d]: match is required", i)
		}
		if rule.Credential != "" {
			credential, ok := credentialNames[rule.Credential]
			if !ok {
				return fmt.Errorf("host_rules[%d]: unknown credential %q", i, rule.Credential)
			}
			if credential.Type != CredentialPassword && credential.Type != CredentialKeyPassphrase {
				return fmt.Errorf("host_rules[%d]: credential %q must have type password or key-passphrase", i, rule.Credential)
			}
		}
	}
	if err := normalizeHosts(c.Hosts); err != nil {
		return err
	}
	for _, host := range c.Hosts {
		if host.Credential != "" {
			credential, ok := credentialNames[host.Credential]
			if !ok {
				return fmt.Errorf("host %q: unknown credential %q", host.Alias, host.Credential)
			}
			if credential.Type != CredentialPassword && credential.Type != CredentialKeyPassphrase {
				return fmt.Errorf("host %q: credential %q must have type password or key-passphrase", host.Alias, host.Credential)
			}
		}
		if host.Identity != "" {
			if _, ok := identityNames[host.Identity]; !ok {
				return fmt.Errorf("host %q: unknown identity %q", host.Alias, host.Identity)
			}
		}
	}
	return nil
}

func (c *TerminalConfig) normalize() error {
	c.DefaultShell = strings.TrimSpace(c.DefaultShell)
	if c.DefaultShell == "" {
		c.DefaultShell = platform.ShellAuto
	}
	if containsControl(c.DefaultShell) {
		return fmt.Errorf("terminal.default_shell must not contain control characters")
	}
	if strings.ContainsAny(c.DefaultShell, " \t") && !looksLikeExecutablePath(c.DefaultShell) {
		return fmt.Errorf("terminal.default_shell with whitespace must be an executable path, not shell syntax")
	}
	for i, arg := range c.ShellArgs {
		if arg == "" || containsControl(arg) {
			return fmt.Errorf("terminal.shell_args[%d] is empty or contains control characters", i)
		}
	}
	return nil
}

func looksLikeExecutablePath(value string) bool {
	if filepath.IsAbs(value) || strings.ContainsAny(value, `/\\`) {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func (c *UIConfig) normalize() error {
	if c.SourcesWidthPercent == 0 {
		c.SourcesWidthPercent = DefaultSourcesWidthPercent
	}
	if c.PreviewWidthPercent == 0 {
		c.PreviewWidthPercent = DefaultPreviewWidthPercent
	}
	if c.HostColumnPercent == 0 {
		c.HostColumnPercent = DefaultHostColumnPercent
	}
	if c.SourcesWidthPercent < 8 || c.SourcesWidthPercent > 30 {
		return fmt.Errorf("app.ui.sources_width_percent must be between 8 and 30")
	}
	if c.PreviewWidthPercent < 12 || c.PreviewWidthPercent > 35 {
		return fmt.Errorf("app.ui.preview_width_percent must be between 12 and 35")
	}
	if c.HostColumnPercent < 15 || c.HostColumnPercent > 60 {
		return fmt.Errorf("app.ui.host_column_percent must be between 15 and 60")
	}
	if c.SourcesWidthPercent+c.PreviewWidthPercent > 48 {
		return fmt.Errorf("app.ui side panes must use at most 48 percent in total")
	}
	return nil
}

// Normalize validates a Config after CLI and source-fragment layers are added.
func (c *Config) Normalize() error { return c.normalize() }

type Inventory struct {
	Version int           `toml:"version"`
	Groups  []GroupConfig `toml:"groups"`
	Hosts   []HostConfig  `toml:"hosts"`
}

// LoadInventory strictly decodes the non-executable SSH Fleet inventory schema shared by the product family.
// Unknown fields are rejected, so passwords and OpenSSH directives cannot be
// smuggled into a trusted inventory document.
func LoadInventory(path string) (Inventory, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return Inventory{}, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return Inventory{}, fmt.Errorf("read inventory %s: %w", expanded, err)
	}
	return ParseInventory(data, expanded)
}

// ParseInventory validates a restricted inventory already held in memory.
// It is used after an encrypted bundle has been authenticated and decrypted.
func ParseInventory(data []byte, label string) (Inventory, error) {
	var inv Inventory
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&inv); err != nil {
		return Inventory{}, fmt.Errorf("parse restricted inventory %s: %w", label, err)
	}
	if inv.Version == 0 {
		inv.Version = CurrentVersion
	}
	if inv.Version != CurrentVersion {
		return Inventory{}, fmt.Errorf("unsupported inventory version %d", inv.Version)
	}
	if err := normalizeHosts(inv.Hosts); err != nil {
		return Inventory{}, err
	}
	for i := range inv.Groups {
		inv.Groups[i].Name = strings.TrimSpace(inv.Groups[i].Name)
		inv.Groups[i].Match = strings.TrimSpace(inv.Groups[i].Match)
		if inv.Groups[i].Name == "" || inv.Groups[i].Match == "" {
			return Inventory{}, fmt.Errorf("groups[%d]: name and match are required", i)
		}
	}
	return inv, nil
}

func validAgeIdentityRef(ref string) bool {
	return strings.HasPrefix(ref, "secret-service:") || strings.HasPrefix(ref, "age-plugin:")
}

func prependUnique(first string, values []string) []string {
	all := append([]string{strings.TrimSpace(first)}, values...)
	seen := make(map[string]struct{}, len(all))
	out := make([]string, 0, len(all))
	for _, value := range all {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func safeExecutable(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n\x00")
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func normalizeHosts(hosts []HostConfig) error {
	for i := range hosts {
		h := &hosts[i]
		h.Alias = strings.TrimSpace(h.Alias)
		h.Source = strings.TrimSpace(h.Source)
		if h.Alias == "" {
			return fmt.Errorf("hosts[%d]: alias is required", i)
		}
		if strings.HasPrefix(h.Alias, "-") || strings.ContainsAny(h.Alias, "\x00\r\n") {
			return fmt.Errorf("host %q: unsafe alias", h.Alias)
		}
		if h.Port < 0 || h.Port > 65535 {
			return fmt.Errorf("host %q: invalid port %d", h.Alias, h.Port)
		}
	}
	return nil
}

func ExpandPath(path string) (string, error) {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := platform.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", path, err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Clean(path), nil
}
