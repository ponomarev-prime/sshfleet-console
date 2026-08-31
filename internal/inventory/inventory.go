package inventory

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/sourcebundle"
	"github.com/ponomarev-prime/sshfleet-console/internal/sshconfig"
)

type Host struct {
	ID                      string
	Alias                   string
	Name                    string
	SourceName              string
	ConfigPath              string
	Hostname                string
	User                    string
	Port                    int
	ProxyJump               string
	Tags                    []string
	Probe                   bool
	CredentialName          string
	CredentialType          string
	CredentialProvider      string
	CredentialKey           string
	IdentityName            string
	IdentityFile            string
	Groups                  []string
	Transport               Transport
	Shell                   string
	ShellArgs               []string
	ShellOrigin             string
	WorkingDirectory        string
	ContainerRuntime        string
	ContainerID             string
	ContainerImage          string
	ContainerState          string
	ContainerStatus         string
	ContainerPorts          string
	ContainerContext        string
	ContainerEndpoint       string
	ContainerHealth         string
	ContainerEntrypoint     string
	ContainerCommand        string
	ContainerMounts         string
	ContainerNetworks       string
	ContainerRestart        string
	ContainerPlatform       string
	ContainerShell          string
	ContainerShellPolicy    string
	ContainerInspectError   string
	ContainerDiscoveryState string
	ContainerDiscoveryError string
	ContainerShells         []string
}

type Transport string

const (
	TransportSSH       Transport = "ssh"
	TransportLocal     Transport = "local"
	TransportContainer Transport = "container"
)

func (h Host) TargetTransport() Transport {
	if h.Transport == "" {
		return TransportSSH
	}
	return h.Transport
}

func (h Host) DisplayName() string {
	if h.Name != "" {
		return h.Name
	}
	return h.Alias
}

type SourceSummary struct {
	Name    string
	Path    string
	Hosts   int
	Err     error
	Dynamic bool
	State   SourceState
	Detail  string
}

type SourceState string

const (
	SourceStateLoaded      SourceState = "loaded"
	SourceStateEmpty       SourceState = "empty"
	SourceStatePartial     SourceState = "partial"
	SourceStateStale       SourceState = "stale"
	SourceStateDenied      SourceState = "denied"
	SourceStateUnavailable SourceState = "unavailable"
)

func Build(cfg config.Config) ([]Host, []SourceSummary) {
	stateDir := cfg.App.SourceStateDir
	if stateDir != "" {
		stateDir, _ = config.ExpandPath(stateDir)
	}
	loader := sourcebundle.Loader{
		HTTPClient: &http.Client{Timeout: cfg.App.SourceFetchTimeout.Duration},
		StateDir:   stateDir,
		MaxBytes:   cfg.App.SourceMaxBytes,
	}
	return BuildWithOptions(cfg, BuildOptions{Context: context.Background(), Bundles: loader})
}

type BundleLoader interface {
	Load(context.Context, config.Source, string) (config.Inventory, error)
}

type BuildOptions struct {
	Context context.Context
	Bundles BundleLoader
}

func BuildWithOptions(cfg config.Config, options BuildOptions) ([]Host, []SourceSummary) {
	if options.Context == nil {
		options.Context = context.Background()
	}
	var hosts []Host
	summaries := make([]SourceSummary, 0, len(cfg.Sources))
	sshConfigPaths := make(map[string]string, len(cfg.Sources))
	credentials := make(map[string]config.Credential, len(cfg.Credentials))
	for _, credential := range cfg.Credentials {
		credentials[credential.Name] = credential
	}
	// Early builds called ~/.ssh/config "default". Keep those overlays working
	// without rewriting user files, unless a real source named default exists.
	hasUserSource, hasDefaultSource := false, false
	sourceNames := make(map[string]struct{}, len(cfg.Sources))
	for _, source := range cfg.Sources {
		sourceNames[source.Name] = struct{}{}
		hasUserSource = hasUserSource || source.Name == "user"
		hasDefaultSource = hasDefaultSource || source.Name == "default"
	}
	if hasUserSource && !hasDefaultSource {
		for i := range cfg.Hosts {
			if cfg.Hosts[i].Source == "default" {
				cfg.Hosts[i].Source = "user"
			}
		}
	}
	identities := make(map[string]config.Identity, len(cfg.Identities))
	for _, identity := range cfg.Identities {
		identities[identity.Name] = identity
	}

	for _, source := range cfg.Sources {
		displayPath := source.Path
		if source.Kind == config.SourceRemote {
			displayPath = source.URL
		}
		path := displayPath
		var err error
		if source.Kind != config.SourceRemote {
			path, err = config.ExpandPath(displayPath)
		}
		if err != nil {
			summaries = append(summaries, SourceSummary{Name: source.Name, Path: displayPath, Err: err})
			continue
		}
		var aliases []string
		var inventoryHosts []config.HostConfig
		var localHosts []config.LocalHostConfig
		var groups []config.GroupConfig
		switch source.Kind {
		case config.SourceSSHConfig:
			sshConfigPaths[source.Name] = path
			aliases, err = sshconfig.Aliases(path)
		case config.SourceInventory:
			var document config.Inventory
			document, err = config.LoadInventory(path)
			if err == nil {
				inventoryHosts, groups = document.Hosts, document.Groups
			}
		case config.SourceLocalConfig:
			var document config.LocalConfig
			document, err = config.LoadLocalConfig(path)
			if err == nil {
				localHosts = document.Hosts
			}
		case config.SourceEncryptedInventory, config.SourceRemote:
			if options.Bundles == nil {
				err = fmt.Errorf("encrypted source loader is not configured")
				break
			}
			authKey := ""
			if credential, ok := credentials[source.AuthCredential]; ok {
				authKey = credential.Key
			}
			ctx, cancel := context.WithTimeout(options.Context, cfg.App.SourceFetchTimeout.Duration)
			var document config.Inventory
			document, err = options.Bundles.Load(ctx, source, authKey)
			cancel()
			if err == nil {
				inventoryHosts, groups = document.Hosts, document.Groups
			}
		default:
			err = fmt.Errorf("unsupported source kind %q", source.Kind)
		}
		summary := SourceSummary{Name: source.Name, Path: path, Hosts: len(aliases) + len(inventoryHosts) + len(localHosts), Err: err}
		summaries = append(summaries, summary)
		if err != nil {
			continue
		}
		for _, alias := range aliases {
			hosts = append(hosts, Host{
				ID:         source.Name + ":" + alias,
				Alias:      alias,
				SourceName: source.Name,
				ConfigPath: path,
				Probe:      true,
			})
		}
		for _, hostConfig := range inventoryHosts {
			// Restricted inventory is intentionally isolated from ~/.ssh/config.
			// All routing data must come from its closed schema.
			host := Host{ID: source.Name + ":" + hostConfig.Alias, Alias: hostConfig.Alias, SourceName: source.Name, ConfigPath: os.DevNull, Probe: true}
			apply(&host, hostConfig)
			for _, group := range groups {
				if matched, _ := filepath.Match(group.Match, host.Alias); matched {
					applyGroup(&host, group)
				}
			}
			hosts = append(hosts, host)
		}
		for _, localConfig := range localHosts {
			shell := localConfig.Shell
			shellArgs := append([]string(nil), localConfig.ShellArgs...)
			shellOrigin := "host"
			if localConfig.Mode == config.LocalModeDirect && shell == "" {
				shell = cfg.Terminal.EffectiveShell
				shellOrigin = cfg.Terminal.EffectiveOrigin
				if len(shellArgs) == 0 {
					shellArgs = append([]string(nil), cfg.Terminal.EffectiveArgs...)
				}
			}
			host := Host{
				ID:               source.Name + ":" + localConfig.Alias,
				Alias:            localConfig.Alias,
				Name:             localConfig.Name,
				SourceName:       source.Name,
				ConfigPath:       path,
				Tags:             append([]string(nil), localConfig.Tags...),
				Probe:            true,
				Shell:            shell,
				ShellArgs:        shellArgs,
				ShellOrigin:      shellOrigin,
				WorkingDirectory: localConfig.WorkingDirectory,
			}
			if localConfig.Probe != nil {
				host.Probe = *localConfig.Probe
			}
			if localConfig.Mode == config.LocalModeDirect {
				host.Transport = TransportLocal
			} else {
				host.Transport = TransportSSH
				host.Hostname = localConfig.Host
				host.User = localConfig.User
				host.Port = localConfig.Port
				host.IdentityName = localConfig.Identity
				host.CredentialName = localConfig.Credential
			}
			hosts = append(hosts, host)
		}
	}

	for i := range hosts {
		assigned := hosts[i].CredentialName
		for _, rule := range cfg.HostRules {
			if rule.Source != "" && rule.Source != hosts[i].SourceName {
				continue
			}
			matched, err := filepath.Match(rule.Match, hosts[i].Alias)
			if err != nil {
				markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("invalid host rule %q: %w", rule.Match, err))
				hosts[i].Probe = false
				continue
			}
			if !matched || rule.Credential == "" {
				continue
			}
			if assigned != "" && assigned != rule.Credential {
				markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("host %q matches conflicting credentials %q and %q", hosts[i].Alias, assigned, rule.Credential))
				hosts[i].Probe = false
				continue
			}
			assigned = rule.Credential
		}
		hosts[i].CredentialName = assigned
	}

	for _, override := range cfg.Hosts {
		matched := false
		for i := range hosts {
			if hosts[i].Alias != override.Alias {
				continue
			}
			if override.Source != "" && hosts[i].SourceName != override.Source {
				continue
			}
			apply(&hosts[i], override)
			matched = true
		}
		if matched {
			continue
		}
		if override.Source != "" {
			if _, exists := sourceNames[override.Source]; !exists {
				// An overlay belongs to its source and must disappear with it. In
				// particular, --no-user-ssh-config must not turn user overlays
				// into direct hosts isolated behind /dev/null.
				continue
			}
		}

		sourceName := override.Source
		if sourceName == "" && len(cfg.Sources) > 0 {
			sourceName = cfg.Sources[0].Name
		}
		configPath := sshConfigPaths[sourceName]
		if configPath == "" {
			// A direct or restricted-inventory host must not accidentally inherit
			// wildcard rules, ProxyCommand, or identity settings from the user's
			// default OpenSSH config.
			configPath = os.DevNull
		}
		h := Host{
			ID:         sourceName + ":" + override.Alias,
			Alias:      override.Alias,
			SourceName: sourceName,
			ConfigPath: configPath,
			Probe:      true,
		}
		apply(&h, override)
		hosts = append(hosts, h)
	}

	for i := range hosts {
		if hosts[i].IdentityName != "" {
			identity, ok := identities[hosts[i].IdentityName]
			if !ok {
				markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("host %q references unknown identity %q", hosts[i].Alias, hosts[i].IdentityName))
				hosts[i].Probe = false
				continue
			}
			path, err := config.ExpandPath(identity.Path)
			if err != nil {
				markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("host %q identity: %w", hosts[i].Alias, err))
				hosts[i].Probe = false
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
				if err == nil {
					err = fmt.Errorf("private key must be a regular mode-0600 file")
				}
				markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("host %q identity %s: %w", hosts[i].Alias, path, err))
				hosts[i].Probe = false
				continue
			}
			hosts[i].IdentityFile = path
			if hosts[i].CredentialName != "" && hosts[i].CredentialName != identity.Credential {
				markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("host %q has conflicting identity and host credentials", hosts[i].Alias))
				hosts[i].Probe = false
				continue
			}
			hosts[i].CredentialName = identity.Credential
		}
		if hosts[i].CredentialName == "" {
			continue
		}
		credential, ok := credentials[hosts[i].CredentialName]
		if !ok {
			markSourceError(summaries, hosts[i].SourceName, fmt.Errorf("host %q references unknown credential %q", hosts[i].Alias, hosts[i].CredentialName))
			hosts[i].Probe = false
			continue
		}
		hosts[i].CredentialType = credential.Type
		hosts[i].CredentialProvider = credential.Provider
		hosts[i].CredentialKey = credential.Key
	}

	AssignGroups(hosts, cfg.Groups)

	sort.SliceStable(hosts, func(i, j int) bool {
		left := strings.ToLower(hosts[i].DisplayName())
		right := strings.ToLower(hosts[j].DisplayName())
		if left == right {
			return hosts[i].ID < hosts[j].ID
		}
		return left < right
	})
	for i := range summaries {
		if summaries[i].Err == nil {
			summaries[i].Hosts = 0
		}
	}
	for _, host := range hosts {
		for i := range summaries {
			if summaries[i].Name == host.SourceName && summaries[i].Err == nil {
				summaries[i].Hosts++
				break
			}
		}
	}
	return hosts, summaries
}

// AssignGroups applies the private application grouping layer to any target,
// including targets discovered dynamically after the initial inventory build.
func AssignGroups(hosts []Host, groups []config.HostGroup) {
	for i := range hosts {
		for _, group := range groups {
			if hostInGroup(hosts[i], group) {
				hosts[i].Groups = appendUnique(hosts[i].Groups, group.Name)
			}
		}
		sort.Strings(hosts[i].Groups)
	}
}

func apply(host *Host, cfg config.HostConfig) {
	if cfg.Name != "" {
		host.Name = cfg.Name
	}
	if cfg.Hostname != "" {
		host.Hostname = cfg.Hostname
	}
	if cfg.User != "" {
		host.User = cfg.User
	}
	if cfg.Port != 0 {
		host.Port = cfg.Port
	}
	if cfg.ProxyJump != "" {
		host.ProxyJump = cfg.ProxyJump
	}
	if cfg.Tags != nil {
		host.Tags = append([]string(nil), cfg.Tags...)
	}
	if cfg.Probe != nil {
		host.Probe = *cfg.Probe
	}
	if cfg.Credential != "" {
		host.CredentialName = cfg.Credential
	}
	if cfg.Identity != "" {
		host.IdentityName = cfg.Identity
	}
}

func applyGroup(host *Host, group config.GroupConfig) {
	host.Groups = appendUnique(host.Groups, group.Name)
	if group.Tags != nil {
		host.Tags = append(host.Tags, group.Tags...)
	}
	if group.Probe != nil {
		host.Probe = *group.Probe
	}
	if group.Credential != "" {
		host.CredentialName = group.Credential
	}
}

func hostInGroup(host Host, group config.HostGroup) bool {
	for _, member := range group.Members {
		member = strings.TrimSpace(member)
		if member == host.ID || member == host.Alias {
			return true
		}
	}
	for _, pattern := range group.Match {
		if matched, _ := filepath.Match(pattern, host.ID); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, host.Alias); matched {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func markSourceError(summaries []SourceSummary, source string, err error) {
	for i := range summaries {
		if summaries[i].Name == source && summaries[i].Err == nil {
			summaries[i].Err = err
			return
		}
	}
}

func (h Host) Validate() error {
	if h.Alias == "" {
		return fmt.Errorf("empty SSH alias")
	}
	if strings.HasPrefix(h.Alias, "-") || strings.ContainsAny(h.Alias, "\x00\r\n") {
		return fmt.Errorf("unsafe SSH alias %q", h.Alias)
	}
	if h.Shell != "" && strings.ContainsAny(h.Shell, " \t\r\n\x00") {
		return fmt.Errorf("host %q: shell must be one executable", h.Alias)
	}
	for i, arg := range h.ShellArgs {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return fmt.Errorf("host %q: shell argument %d is empty or unsafe", h.Alias, i)
		}
	}
	if h.TargetTransport() == TransportContainer {
		if !safeContainerID(h.ContainerID) || (h.ContainerRuntime != "docker" && h.ContainerRuntime != "podman") {
			return fmt.Errorf("container %q: runtime and immutable ID are required", h.Alias)
		}
		return nil
	}
	if h.TargetTransport() == TransportLocal {
		if h.Shell == "" || strings.ContainsAny(h.Shell, " \t\r\n\x00") {
			return fmt.Errorf("local host %q: one shell executable is required", h.Alias)
		}
		return nil
	}
	if h.CredentialName != "" && (h.CredentialProvider != "secret-service" || h.CredentialKey == "") {
		return fmt.Errorf("host %q: incomplete Secret Service credential binding", h.Alias)
	}
	return nil
}

func safeContainerID(value string) bool {
	if len(value) < 12 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}
