package inventory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
)

type fakeBundleLoader struct {
	calls []string
}

func (f *fakeBundleLoader) Load(_ context.Context, source config.Source, authKey string) (config.Inventory, error) {
	f.calls = append(f.calls, source.Kind+":"+source.Name+":"+authKey)
	return config.Inventory{Version: 1, Hosts: []config.HostConfig{{Alias: source.Name + "-01", Hostname: "192.0.2.60"}}}, nil
}

func TestBuildAppliesTOMLMetadata(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshPath, []byte("Host app-01\n  HostName 10.0.0.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := false
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "lab", Kind: "ssh_config", Path: sshPath}}
	cfg.Hosts = []config.HostConfig{{Alias: "app-01", Name: "Application", Tags: []string{"prod"}, Probe: &probe}}

	hosts, summaries := Build(cfg)
	if len(summaries) != 1 || summaries[0].Err != nil {
		t.Fatalf("summaries = %#v", summaries)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts = %#v", hosts)
	}
	if hosts[0].DisplayName() != "Application" || hosts[0].Probe || hosts[0].Tags[0] != "prod" {
		t.Fatalf("host = %#v", hosts[0])
	}
}

func TestBuildLoadsTrustedLocalTargetsWithoutSSHInheritance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.toml")
	if err := os.WriteFile(path, []byte(`version = 1
[[local_hosts]]
alias = "this-machine"
name = "Local workstation"
mode = "direct"
shell = "/bin/sh"
working_directory = "/tmp"

[[local_hosts]]
alias = "local-sshd"
mode = "ssh"
host = "127.0.0.1"
port = 2222
user = "operator"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "local", Kind: config.SourceLocalConfig, Path: path}}
	cfg.Groups = []config.HostGroup{{Name: "on-my-pc", Match: []string{"local:*"}}}
	hosts, sources := Build(cfg)
	if len(hosts) != 2 || len(sources) != 1 || sources[0].Hosts != 2 {
		t.Fatalf("hosts/sources = %#v / %#v", hosts, sources)
	}
	byAlias := map[string]Host{}
	for _, host := range hosts {
		byAlias[host.Alias] = host
	}
	direct := byAlias["this-machine"]
	if direct.TargetTransport() != TransportLocal || direct.Shell != "/bin/sh" || direct.ConfigPath != path {
		t.Fatalf("direct target = %#v", direct)
	}
	localSSH := byAlias["local-sshd"]
	if localSSH.TargetTransport() != TransportSSH || localSSH.Hostname != "127.0.0.1" || localSSH.Port != 2222 {
		t.Fatalf("local ssh target = %#v", localSSH)
	}
	for _, host := range hosts {
		if len(host.Groups) != 1 || host.Groups[0] != "on-my-pc" {
			t.Fatalf("group assignment = %#v", host)
		}
	}
}

func TestBuildDirectLocalTargetInheritsResolvedGlobalTerminal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local.toml")
	if err := os.WriteFile(path, []byte(`version = 1
[[local_hosts]]
alias = "this-machine"
mode = "direct"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Terminal.EffectiveShell = "/usr/bin/fish"
	cfg.Terminal.EffectiveArgs = []string{"-l", "значение с пробелом"}
	cfg.Terminal.EffectiveOrigin = "toml"
	cfg.Sources = []config.Source{{Name: "local", Kind: config.SourceLocalConfig, Path: path}}
	hosts, _ := Build(cfg)
	if len(hosts) != 1 || hosts[0].Shell != "/usr/bin/fish" || hosts[0].ShellOrigin != "toml" || len(hosts[0].ShellArgs) != 2 || hosts[0].ShellArgs[1] != "значение с пробелом" {
		t.Fatalf("inherited terminal = %#v", hosts)
	}
}

func TestBuildAppliesLaterHostLayerLast(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshPath, []byte("Host app-01\n  HostName 10.0.0.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "lab", Kind: "ssh_config", Path: sshPath}}
	cfg.Hosts = []config.HostConfig{
		{Alias: "app-01", Source: "lab", Name: "Base name", Tags: []string{"base"}},
		{Alias: "app-01", Source: "lab", Name: "Overlay name", Tags: []string{"overlay"}},
	}

	hosts, _ := Build(cfg)
	if len(hosts) != 1 || hosts[0].Name != "Overlay name" || len(hosts[0].Tags) != 1 || hosts[0].Tags[0] != "overlay" {
		t.Fatalf("host = %#v", hosts)
	}
}

func TestBuildMigratesLegacyDefaultOverlayToBuiltInUserSource(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshPath, []byte("Host app-01\n  HostName 10.0.0.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "user", Kind: config.SourceSSHConfig, Path: sshPath}}
	cfg.Hosts = []config.HostConfig{{Alias: "app-01", Source: "default", Name: "Legacy overlay"}}
	hosts, summaries := Build(cfg)
	if len(hosts) != 1 || hosts[0].SourceName != "user" || hosts[0].Name != "Legacy overlay" {
		t.Fatalf("legacy overlay created an orphan host: %#v", hosts)
	}
	if len(summaries) != 1 || summaries[0].Hosts != 1 {
		t.Fatalf("source summary = %#v", summaries)
	}
}

func TestBuildDropsUserOverlayWhenUserSourceIsDisabled(t *testing.T) {
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "secure", Kind: config.SourceInventory, Path: filepath.Join(t.TempDir(), "missing")}}
	cfg.Hosts = []config.HostConfig{{Alias: "old-user-host", Source: "default"}}
	hosts, _ := Build(cfg)
	if len(hosts) != 0 {
		t.Fatalf("disabled user overlay leaked into inventory: %#v", hosts)
	}
}

func TestBuildRestrictedInventoryGroupsAndCredentialRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.toml")
	data := `version = 1
[[groups]]
name = "password-nodes"
match = "password-node-*"
tags = ["stand"]

[[hosts]]
alias = "password-node-1"
hostname = "127.0.0.1"
user = "root"
port = 2222
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "lab", Kind: "inventory", Path: path}}
	cfg.Credentials = []config.Credential{{Name: "stands", Type: "password", Provider: "secret-service", Key: "sshfleet/lab/stands"}}
	cfg.HostRules = []config.HostRule{{Source: "lab", Match: "password-node-*", Credential: "stands"}}
	hosts, summaries := Build(cfg)
	if len(summaries) != 1 || summaries[0].Err != nil {
		t.Fatalf("summaries = %#v", summaries)
	}
	if len(hosts) != 1 || hosts[0].Tags[0] != "stand" || hosts[0].CredentialKey != "sshfleet/lab/stands" {
		t.Fatalf("hosts = %#v", hosts)
	}
	if hosts[0].ConfigPath != os.DevNull {
		t.Fatalf("restricted inventory inherited OpenSSH config: %q", hosts[0].ConfigPath)
	}
}

func TestBuildResolvesLogicalEncryptedIdentityWithoutPuttingPathInInventory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secure.toml")
	keyPath := filepath.Join(dir, "id_secure_202")
	if err := os.WriteFile(keyPath, []byte("encrypted fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := `version = 1
[[hosts]]
alias = "secure-202"
hostname = "192.0.2.202"
user = "operator"
identity = "perf-202"
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "secure", Kind: config.SourceInventory, Path: path}}
	cfg.Credentials = []config.Credential{{Name: "perf-key", Type: config.CredentialKeyPassphrase, Provider: "secret-service", Key: "sshfleet/keys/perf-202"}}
	cfg.Identities = []config.Identity{{Name: "perf-202", Path: keyPath, Credential: "perf-key"}}
	hosts, summaries := Build(cfg)
	if len(summaries) != 1 || summaries[0].Err != nil || len(hosts) != 1 {
		t.Fatalf("hosts/summaries = %#v / %#v", hosts, summaries)
	}
	host := hosts[0]
	if host.IdentityFile != keyPath || host.CredentialType != config.CredentialKeyPassphrase || host.CredentialKey != "sshfleet/keys/perf-202" {
		t.Fatalf("resolved host = %#v", host)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), keyPath) || strings.Contains(string(contents), "sshfleet/keys/perf-202") {
		t.Fatalf("restricted inventory leaked local key details: %s", contents)
	}
}

func TestBuildLoadsEverySourceKindAndIsolatesRestrictedHosts(t *testing.T) {
	dir := t.TempDir()
	userConfig := filepath.Join(dir, "user_config")
	customConfig := filepath.Join(dir, "custom_config")
	restricted := filepath.Join(dir, "restricted.toml")
	if err := os.WriteFile(userConfig, []byte("Host user-01\n  HostName 192.0.2.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customConfig, []byte("Host custom-01\n  HostName 192.0.2.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(restricted, []byte("version = 1\n[[hosts]]\nalias = \"plain-01\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := &fakeBundleLoader{}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{
		{Name: "user", Kind: config.SourceSSHConfig, Path: userConfig},
		{Name: "custom", Kind: config.SourceSSHConfig, Path: customConfig},
		{Name: "plain", Kind: config.SourceInventory, Path: restricted},
		{Name: "encrypted", Kind: config.SourceEncryptedInventory, Path: filepath.Join(dir, "bundle")},
		{Name: "remote", Kind: config.SourceRemote, URL: "https://inventory.example/fleet/", AuthCredential: "remote-token"},
	}
	cfg.Credentials = []config.Credential{{Name: "remote-token", Type: "bearer", Provider: "secret-service", Key: "sshfleet/http/token"}}
	hosts, summaries := BuildWithOptions(cfg, BuildOptions{Context: context.Background(), Bundles: loader})
	if len(hosts) != 5 || len(summaries) != 5 {
		t.Fatalf("hosts/summaries = %d/%d: %#v / %#v", len(hosts), len(summaries), hosts, summaries)
	}
	if got := strings.Join(loader.calls, ","); got != "encrypted_inventory:encrypted:,remote:remote:sshfleet/http/token" {
		t.Fatalf("bundle calls = %q", got)
	}
	for _, host := range hosts {
		if host.SourceName != "user" && host.SourceName != "custom" && host.ConfigPath != os.DevNull {
			t.Fatalf("restricted host %s inherited config %q", host.ID, host.ConfigPath)
		}
	}
}

func TestBuildAppliesPrivateGroupsAcrossSources(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("Host alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("Host beta prod-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Sources = []config.Source{{Name: "user", Kind: config.SourceSSHConfig, Path: first}, {Name: "lab", Kind: config.SourceSSHConfig, Path: second}}
	cfg.Groups = []config.HostGroup{{Name: "mixed", Members: []string{"user:alpha", "lab:beta"}, Match: []string{"lab:prod-*"}}}
	hosts, _ := Build(cfg)
	if len(hosts) != 3 {
		t.Fatalf("hosts = %#v", hosts)
	}
	for _, host := range hosts {
		if len(host.Groups) != 1 || host.Groups[0] != "mixed" {
			t.Fatalf("host %s groups = %#v", host.ID, host.Groups)
		}
	}
}
