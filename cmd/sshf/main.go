package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/ponomarev-prime/sshfleet-console/internal/askpass"
	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/containers"
	"github.com/ponomarev-prime/sshfleet-console/internal/credential"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
	"github.com/ponomarev-prime/sshfleet-console/internal/sourcebundle"
	"github.com/ponomarev-prime/sshfleet-console/internal/toolcheck"
	"github.com/ponomarev-prime/sshfleet-console/internal/ui"
)

var (
	version      = "dev-unknown"
	buildCommit  = "unknown"
	buildBranch  = "unknown"
	buildDate    = "unknown"
	buildDirty   = "unknown"
	buildChannel = "development"
)

type sourceFlags []string

func (s *sourceFlags) String() string { return strings.Join(*s, ",") }
func (s *sourceFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("source cannot be empty")
	}
	*s = append(*s, value)
	return nil
}

type shellArgFlags struct {
	values []string
	set    bool
}

func (s *shellArgFlags) String() string { return strings.Join(s.values, ",") }
func (s *shellArgFlags) Set(value string) error {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("shell argument cannot be empty or contain control bytes")
	}
	s.set = true
	s.values = append(s.values, value)
	return nil
}

func main() {
	if os.Getenv(askpass.ModeEnv) == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := askpass.Run(ctx, os.Stdout, os.Getenv); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "version" {
		if err := runVersionCommand(os.Args[2:], os.Stdout); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "source" {
		if err := runSourceCommand(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "credential" {
		if err := runCredentialCommand(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "healthcheck" || os.Args[1] == "doctor" || os.Args[1] == "checkhealth") {
		if err := runHealthcheckCommand(os.Args[2:], os.Stdout); err != nil {
			fatal(err)
		}
		return
	}
	var (
		configPath        string
		overridesDir      string
		editor            string
		listOnly          bool
		showVersion       bool
		noUserConfig      bool
		forceUserConfig   bool
		noProbe           bool
		forceProbe        bool
		extraSources      sourceFlags
		extraInventories  sourceFlags
		extraLocalConfigs sourceFlags
		sourcesDir        string
		groupsDir         string
		terminalShell     string
		terminalArgs      shellArgFlags
		maxConcurrent     int
		refreshInterval   time.Duration
	)
	flag.StringVar(&configPath, "config", "", "main SSH Fleet Console TOML config (default: XDG config directory)")
	flag.StringVar(&overridesDir, "overrides-dir", "", "editable host overlay directory (default: XDG config directory)")
	flag.StringVar(&editor, "editor", "", "editor executable for config editing (default priority: SSH Fleet Console nvim, then system nvim, vim, nano)")
	flag.StringVar(&terminalShell, "shell", "", "local default shell for this run (overrides [terminal].default_shell)")
	flag.Var(&terminalArgs, "shell-arg", "local default shell argument for this run; repeatable and passed as separate argv")
	flag.Var(&extraSources, "ssh-config", "add trusted SSH config for this run; repeatable, NAME=PATH or PATH")
	flag.Var(&extraInventories, "inventory", "add restricted SSH Fleet Console TOML inventory; repeatable, NAME=PATH or PATH")
	flag.Var(&extraLocalConfigs, "local-config", "add trusted local target config; repeatable, NAME=PATH or PATH")
	flag.StringVar(&sourcesDir, "sources-dir", "", "persistent source fragments directory (default: XDG config directory)")
	flag.StringVar(&groupsDir, "groups-dir", "", "private group fragments directory (default: XDG config directory)")
	flag.BoolVar(&noUserConfig, "no-user-ssh-config", false, "do not load the built-in ~/.ssh/config source")
	flag.BoolVar(&noUserConfig, "no-default-ssh-config", false, "deprecated alias for --no-user-ssh-config")
	flag.BoolVar(&forceUserConfig, "user-ssh-config", false, "load ~/.ssh/config even when disabled in TOML")
	flag.BoolVar(&noProbe, "no-probe", false, "show inventory without background SSH probes")
	flag.BoolVar(&forceProbe, "probe", false, "enable probes even when disabled in TOML")
	flag.IntVar(&maxConcurrent, "max-concurrent", 0, "maximum simultaneous probes (default: 2 x available CPU cores)")
	flag.DurationVar(&refreshInterval, "refresh-interval", 0, "probe refresh interval (default: 10s)")
	flag.BoolVar(&listOnly, "list", false, "list discovered hosts and exit")
	flag.BoolVar(&showVersion, "v", false, "print version and exit (shorthand for --version)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(currentBuildInfo().OneLine())
		return
	}

	cfg, usedPath, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}
	if maxConcurrent < 0 {
		fatal(fmt.Errorf("--max-concurrent must be positive"))
	}
	if refreshInterval < 0 {
		fatal(fmt.Errorf("--refresh-interval must be positive"))
	}
	if noUserConfig && forceUserConfig {
		fatal(fmt.Errorf("--no-user-ssh-config and --user-ssh-config conflict"))
	}
	if noProbe && forceProbe {
		fatal(fmt.Errorf("--no-probe and --probe conflict"))
	}
	if maxConcurrent > 0 {
		cfg.App.MaxConcurrent = maxConcurrent
	}
	if refreshInterval > 0 {
		cfg.App.RefreshInterval.Duration = refreshInterval
	}
	if sourcesDir == "" {
		sourcesDir = cfg.App.SourcesDir
	}
	if groupsDir == "" {
		groupsDir = cfg.App.GroupsDir
	}
	if overridesDir == "" {
		overridesDir = cfg.App.OverridesDir
	}
	if editor == "" {
		editor = cfg.App.Editor
	} else {
		cfg.App.Editor = editor
	}
	terminalOrigin := applyTerminalOverrides(&cfg, terminalShell, terminalArgs.values, terminalArgs.set)
	workspaceBundle, err := resolveWorkspaceBundle(cfg.App.WorkspaceBundle, os.Getenv("SSHF_WORKSPACE_BUNDLE"))
	if err != nil {
		fatal(err)
	}
	if !noUserConfig {
		noUserConfig = !cfg.App.LoadUserSSHConfig && !forceUserConfig
	}
	if !noProbe {
		noProbe = !cfg.App.ProbeEnabled && !forceProbe
	}
	persistentSources, err := config.LoadSourceFragments(sourcesDir)
	if err != nil {
		fatal(err)
	}
	cfg.Sources = launchSources(cfg.Sources, persistentSources, extraSources, extraInventories, extraLocalConfigs, noUserConfig)
	mainGroups := append([]config.HostGroup(nil), cfg.Groups...)
	if groupsDir == "" {
		groupsDir, err = config.DefaultGroupsDir()
	} else {
		groupsDir, err = config.ExpandPath(groupsDir)
	}
	if err != nil {
		fatal(err)
	}
	loadGroups := func() ([]config.HostGroup, error) {
		fragments, err := config.LoadGroupFragments(groupsDir)
		if err != nil {
			return nil, err
		}
		return config.MergeHostGroups(mainGroups, fragments)
	}
	cfg.Groups, err = loadGroups()
	if err != nil {
		fatal(err)
	}
	if err := cfg.Normalize(); err != nil {
		fatal(err)
	}
	terminal := platform.ResolveShell(cfg.Terminal.DefaultShell, cfg.Terminal.ShellArgs, terminalOrigin)
	cfg.Terminal.EffectiveShell = terminal.Effective
	cfg.Terminal.EffectiveArgs = append([]string(nil), terminal.Args...)
	cfg.Terminal.EffectiveOrigin = terminal.Origin
	editorResult := toolcheck.ResolveEditor(cfg.App.EditorPriority)
	editor = editorResult.Path
	health := applicationHealth(cfg.App.SSHBinary, terminal, platform.Current())
	sshResult := toolcheck.Resolve(cfg.App.SSHBinary)
	if sshResult.Error == "" {
		cfg.App.SSHBinary = sshResult.Path
	}
	if overridesDir == "" {
		overridesDir, err = config.DefaultOverridesDir()
	} else {
		overridesDir, err = config.ExpandPath(overridesDir)
	}
	if err != nil {
		fatal(err)
	}
	buildInventoryWithGroups := func(groups []config.HostGroup) ([]inventory.Host, []inventory.SourceSummary, error) {
		overrides, err := config.LoadHostOverrides(overridesDir)
		if err != nil {
			return nil, nil, err
		}
		layered := cfg
		layered.Groups = groups
		layered.Hosts = append(append([]config.HostConfig(nil), cfg.Hosts...), overrides...)
		hosts, sources := inventory.Build(layered)
		if cfg.App.Containers.Enabled {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			containerHosts, containerSources := containers.Discover(ctx, cfg.App.Containers)
			cancel()
			inventory.AssignGroups(containerHosts, groups)
			hosts = append(hosts, containerHosts...)
			sources = append(sources, containerSources...)
		}
		sort.SliceStable(hosts, func(i, j int) bool {
			left, right := strings.ToLower(hosts[i].DisplayName()), strings.ToLower(hosts[j].DisplayName())
			if left == right {
				return hosts[i].ID < hosts[j].ID
			}
			return left < right
		})
		if noProbe {
			for i := range hosts {
				hosts[i].Probe = false
			}
		}
		return hosts, sources, nil
	}
	buildInventory := func() ([]inventory.Host, []inventory.SourceSummary, error) {
		groups, err := loadGroups()
		if err != nil {
			return nil, nil, err
		}
		return buildInventoryWithGroups(groups)
	}
	reloadGroups := func() ([]inventory.Host, []inventory.SourceSummary, []config.HostGroup, error) {
		groups, err := loadGroups()
		if err != nil {
			return nil, nil, nil, err
		}
		hosts, sources, err := buildInventoryWithGroups(groups)
		return hosts, sources, groups, err
	}
	hosts, sources, err := buildInventoryWithGroups(cfg.Groups)
	if err != nil {
		fatal(err)
	}
	if listOnly {
		printInventory(usedPath, hosts, sources)
		return
	}

	askPass := resolveAskPass()
	client := openssh.Client{
		Binary:           cfg.App.SSHBinary,
		ConnectTimeout:   cfg.App.ConnectTimeout.Duration,
		AskPassBinary:    askPass.Path,
		AskPassSelf:      askPass.Self,
		WorkspaceBundle:  workspaceBundle,
		WorkspaceCleanup: cfg.App.WorkspaceCleanup,
	}
	localContext, localCancel := context.WithTimeout(context.Background(), 2*time.Second)
	client.LocalClient = openssh.DetectLocalClient(localContext, client.Binary)
	localCancel()
	var dynamicDiscover func() ([]inventory.Host, []inventory.SourceSummary)
	if cfg.App.Containers.Enabled {
		dynamicDiscover = func() ([]inventory.Host, []inventory.SourceSummary) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			hosts, sources := containers.Discover(ctx, cfg.App.Containers)
			groups, groupErr := loadGroups()
			if groupErr != nil {
				groups = cfg.Groups
			}
			inventory.AssignGroups(hosts, groups)
			if noProbe {
				for i := range hosts {
					hosts[i].Probe = false
				}
			}
			return hosts, sources
		}
	}
	model := ui.New(
		hosts,
		sources,
		client,
		cfg.App.RefreshInterval.Duration,
		cfg.App.MaxConcurrent,
		ui.WithHostEditor(overridesDir, editor, buildInventory),
		ui.WithAppConfigEditor(usedPath),
		ui.WithUILayout(cfg.App.UI),
		ui.WithHealth(health, editorResult),
		ui.WithGroupsAndCommands(cfg.Groups, cfg.Commands),
		ui.WithGroupEditor(groupsDir, editor, reloadGroups),
		ui.WithDynamicDiscovery(cfg.App.Containers.RefreshInterval.Duration, dynamicDiscover),
	)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fatal(fmt.Errorf("run TUI: %w", err))
	}
}

func applicationHealth(sshBinary string, terminal platform.ShellResolution, capabilities platform.Capabilities) []toolcheck.Result {
	build := toolcheck.Result{Name: "sshfleet", Path: currentBuildInfo().HealthSummary(), Origin: toolcheck.OriginSSHFleet}
	platformResult := toolcheck.Result{Name: "platform", Path: capabilities.Platform() + " · " + capabilities.PTYDescription() + " · credentials " + capabilities.CredentialStore, Origin: toolcheck.OriginSSHFleet}
	terminalResult := toolcheck.Result{Name: "shell", Path: terminal.CommandText(), Origin: toolcheck.Origin(terminal.Origin), Error: terminal.Error}
	ssh := toolcheck.Resolve(sshBinary)
	ssh.Name = "ssh"
	tools := []string{"ssh-keygen", "age", "docker", "podman", "nvim", "vim", "nano", "lf", "dtop", "bat"}
	if capabilities.CredentialStoreImplemented && capabilities.CredentialStore == "secret-service" {
		tools = append([]string{"secret-tool"}, tools...)
	}
	return append([]toolcheck.Result{build, platformResult, terminalResult, ssh}, toolcheck.Health(tools)...)
}

func applyTerminalOverrides(cfg *config.Config, shell string, args []string, argsSet bool) string {
	origin := platform.OriginTOML
	if strings.TrimSpace(shell) != "" {
		cfg.Terminal.DefaultShell = shell
		origin = platform.OriginCLI
	}
	if argsSet {
		cfg.Terminal.ShellArgs = append([]string(nil), args...)
		origin = platform.OriginCLI
	}
	return origin
}

func resolveWorkspaceBundle(configured, launcherDefault string) (string, error) {
	candidate := strings.TrimSpace(configured)
	explicit := candidate != ""
	if candidate == "" {
		candidate = strings.TrimSpace(launcherDefault)
	}
	if candidate == "" {
		return "", nil
	}
	expanded, err := config.ExpandPath(candidate)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("workspace bundle %s: %w", expanded, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("workspace bundle is not a regular file: %s", expanded)
	}
	return expanded, nil
}

type askPassResolution struct {
	Path   string
	Origin string
	Self   bool
	Error  string
}

func resolveAskPass() askPassResolution {
	if configured := strings.TrimSpace(os.Getenv("SSHF_ASKPASS")); configured != "" {
		path, err := executablePath(configured)
		if err != nil {
			return askPassResolution{Origin: "configured", Error: err.Error()}
		}
		return askPassResolution{Path: path, Origin: "configured"}
	}
	if executable, err := os.Executable(); err == nil {
		if path, pathErr := executablePath(executable); pathErr == nil {
			return askPassResolution{Path: path, Origin: "built-in", Self: true}
		} else {
			return askPassResolution{Origin: "built-in", Error: pathErr.Error()}
		}
	}
	return askPassResolution{Origin: "built-in", Error: "cannot resolve current sshfleet executable"}
}

func executablePath(candidate string) (string, error) {
	if !filepath.IsAbs(candidate) && !strings.ContainsRune(candidate, filepath.Separator) {
		resolved, err := exec.LookPath(candidate)
		if err != nil {
			return "", err
		}
		candidate = resolved
	}
	path, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	return platform.ExecutableFile(path)
}

func runCredentialCommand(args []string, input *os.File, output *os.File) error {
	if len(args) < 2 || args[0] != "set" {
		return fmt.Errorf("usage: sshfleet credential set NAME [--config PATH]")
	}
	name := strings.TrimSpace(args[1])
	flags := flag.NewFlagSet("credential set", flag.ContinueOnError)
	flags.SetOutput(output)
	configPath := ""
	flags.StringVar(&configPath, "config", "", "main SSH Fleet Console TOML config")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	cfg, _, err := config.Load(configPath)
	if err != nil {
		return err
	}
	var selected *config.Credential
	for i := range cfg.Credentials {
		if cfg.Credentials[i].Name == name {
			selected = &cfg.Credentials[i]
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("credential %q is not declared in TOML", name)
	}
	if !term.IsTerminal(input.Fd()) {
		return fmt.Errorf("password input requires a terminal")
	}
	secretLabel := "Password"
	if selected.Type == config.CredentialBearer {
		secretLabel = "Bearer token"
	} else if selected.Type == config.CredentialKeyPassphrase {
		secretLabel = "Key passphrase"
	}
	fmt.Fprintf(output, "%s for %s: ", secretLabel, name)
	password, err := term.ReadPassword(input.Fd())
	fmt.Fprintln(output)
	if err != nil {
		return err
	}
	defer clear(password)
	fmt.Fprint(output, "Repeat "+strings.ToLower(secretLabel)+": ")
	repeated, err := term.ReadPassword(input.Fd())
	fmt.Fprintln(output)
	if err != nil {
		return err
	}
	defer clear(repeated)
	if !bytes.Equal(password, repeated) {
		return fmt.Errorf("secret values do not match")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := (credential.SecretService{}).Set(ctx, selected.Key, password); err != nil {
		return err
	}
	fmt.Fprintln(output, "Credential stored in Secret Service:", name)
	return nil
}

func launchSources(existing, persistent []config.Source, extraSSH, extraInventory, extraLocal []string, noUserConfig bool) []config.Source {
	var sources []config.Source
	if !noUserConfig {
		sources = append(sources, config.Source{Name: "user", Kind: "ssh_config", Path: "~/.ssh/config"})
	}
	sources = append(sources, existing...)
	sources = append(sources, persistent...)
	if noUserConfig {
		userPath, _ := config.ExpandPath("~/.ssh/config")
		kept := sources[:0]
		for _, source := range sources {
			path, _ := config.ExpandPath(source.Path)
			if source.Kind == "ssh_config" && path == userPath {
				continue
			}
			kept = append(kept, source)
		}
		sources = kept
	}

	usedNames := make(map[string]bool, len(sources))
	for _, source := range sources {
		usedNames[source.Name] = true
	}
	add := func(spec string, index int, kind string) {
		name, path := sourceSpec(spec, index+1)
		base := name
		for suffix := 2; usedNames[name]; suffix++ {
			name = fmt.Sprintf("%s-%d", base, suffix)
		}
		usedNames[name] = true
		sources = append(sources, config.Source{Name: name, Kind: kind, Path: path})
	}
	for i, spec := range extraSSH {
		add(spec, i, "ssh_config")
	}
	for i, spec := range extraInventory {
		add(spec, i, "inventory")
	}
	for i, spec := range extraLocal {
		add(spec, i, config.SourceLocalConfig)
	}

	// The same file is loaded only once even when it appears in TOML, sources.d,
	// and flags. The first declaration wins, so the built-in user source is stable.
	seenPaths := make(map[string]bool)
	filtered := sources[:0]
	for _, source := range sources {
		path, _ := config.ExpandPath(source.Path)
		key := source.Kind + "\x00" + path
		if path != "." && seenPaths[key] {
			continue
		}
		seenPaths[key] = true
		filtered = append(filtered, source)
	}
	return filtered
}

func runSourceCommand(args []string, input *os.File, output *os.File) error {
	if len(args) > 0 && args[0] == "pack" {
		return runSourcePack(args[1:], input, output)
	}
	if len(args) == 0 || args[0] != "add" {
		return fmt.Errorf("usage: sshfleet source add|pack [options]")
	}
	flags := flag.NewFlagSet("source add", flag.ContinueOnError)
	flags.SetOutput(output)
	kind, name, path, sourceURL, signingKey, ageIdentityRef, authCredential, dir, configPath := "", "", "", "", "", "", "", "", ""
	flags.StringVar(&kind, "type", "", "openssh, local, inventory, encrypted, or remote")
	flags.StringVar(&name, "name", "", "source name")
	flags.StringVar(&path, "path", "", "source file or encrypted bundle directory")
	flags.StringVar(&sourceURL, "url", "", "remote HTTPS bundle directory URL")
	flags.StringVar(&signingKey, "signing-key", "", "pinned OpenSSH allowed_signers file")
	flags.StringVar(&ageIdentityRef, "age-identity-ref", "", "secret-service:KEY or age-plugin:PATH")
	flags.StringVar(&authCredential, "auth-credential", "", "declared bearer credential name for HTTPS")
	flags.StringVar(&dir, "sources-dir", "", "source fragments directory")
	flags.StringVar(&configPath, "config", "", "main SSH Fleet Console TOML config")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	ask := func(label string, value *string) error {
		if strings.TrimSpace(*value) != "" {
			return nil
		}
		fmt.Fprint(output, label)
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return err
		}
		*value = strings.TrimSpace(line)
		return nil
	}
	if err := ask("Type [openssh/local/inventory]: ", &kind); err != nil {
		return err
	}
	if err := ask("Name: ", &name); err != nil {
		return err
	}
	if kind == "openssh" {
		kind = config.SourceSSHConfig
	}
	if kind == "encrypted" {
		kind = config.SourceEncryptedInventory
	}
	if kind == "local" {
		kind = config.SourceLocalConfig
	}
	source := config.Source{Name: name, Kind: kind, AuthCredential: authCredential}
	if kind == config.SourceRemote {
		if err := ask("HTTPS bundle URL: ", &sourceURL); err != nil {
			return err
		}
		source.URL = sourceURL
	} else {
		if err := ask("Path: ", &path); err != nil {
			return err
		}
		expanded, err := config.ExpandPath(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(expanded)
		if err != nil {
			return fmt.Errorf("inspect source: %w", err)
		}
		if kind == config.SourceEncryptedInventory {
			if !info.IsDir() {
				return fmt.Errorf("encrypted source path must be a bundle directory")
			}
		} else if info.IsDir() {
			return fmt.Errorf("source path is a directory")
		}
		source.Path = expanded
	}
	if kind == config.SourceEncryptedInventory || kind == config.SourceRemote {
		if err := ask("Allowed signers file: ", &signingKey); err != nil {
			return err
		}
		if err := ask("Age identity reference: ", &ageIdentityRef); err != nil {
			return err
		}
		expanded, err := config.ExpandPath(signingKey)
		if err != nil {
			return err
		}
		info, err := os.Stat(expanded)
		if err != nil {
			return fmt.Errorf("inspect allowed signers file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("allowed signers path is not a regular file")
		}
		source.SigningKey = expanded
		source.AgeIdentityRef = ageIdentityRef
	}
	if kind == config.SourceInventory {
		if _, err := config.LoadInventory(source.Path); err != nil {
			return err
		}
	} else if kind == config.SourceLocalConfig {
		if _, err := config.LoadLocalConfig(source.Path); err != nil {
			return err
		}
	} else if kind != config.SourceSSHConfig && kind != config.SourceEncryptedInventory && kind != config.SourceRemote {
		return fmt.Errorf("type must be openssh, local, inventory, encrypted, or remote")
	}
	if dir == "" {
		cfg, _, err := config.Load(configPath)
		if err != nil {
			return err
		}
		dir = cfg.App.SourcesDir
	}
	written, err := config.SaveSourceFragment(dir, source)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "Added source:", written)
	return nil
}

func runSourcePack(args []string, input *os.File, output *os.File) error {
	flags := flag.NewFlagSet("source pack", flag.ContinueOnError)
	flags.SetOutput(output)
	sourceID, inventoryPath, outputDir, recipient, signingKey := "", "", "", "", ""
	var revision uint64
	var expires time.Duration
	flags.StringVar(&sourceID, "source-id", "", "source identity bound by the signature")
	flags.StringVar(&inventoryPath, "inventory", "", "restricted plaintext inventory to encrypt")
	flags.StringVar(&outputDir, "output", "", "new bundle output directory")
	flags.StringVar(&recipient, "recipient", "", "age recipient")
	flags.StringVar(&signingKey, "signing-key", "", "OpenSSH private signing key")
	flags.Uint64Var(&revision, "revision", 0, "monotonically increasing positive revision")
	flags.DurationVar(&expires, "expires", 30*24*time.Hour, "bundle validity duration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	ask := func(label string, value *string) error {
		if strings.TrimSpace(*value) != "" {
			return nil
		}
		fmt.Fprint(output, label)
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return err
		}
		*value = strings.TrimSpace(line)
		return nil
	}
	for _, prompt := range []struct {
		label string
		value *string
	}{{"Source ID: ", &sourceID}, {"Inventory path: ", &inventoryPath}, {"Output directory: ", &outputDir}, {"Age recipient: ", &recipient}, {"OpenSSH signing key: ", &signingKey}} {
		if err := ask(prompt.label, prompt.value); err != nil {
			return err
		}
	}
	if revision == 0 {
		return fmt.Errorf("--revision must be positive")
	}
	if expires <= 0 {
		return fmt.Errorf("--expires must be positive")
	}
	created := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sourcebundle.Pack(ctx, sourcebundle.PackOptions{
		SourceID: sourceID, Revision: revision, InventoryPath: inventoryPath,
		OutputDir: outputDir, Recipient: recipient, SigningKey: signingKey,
		CreatedAt: created, ExpiresAt: created.Add(expires),
	}); err != nil {
		return err
	}
	fmt.Fprintln(output, "Created encrypted source bundle:", outputDir)
	return nil
}

func sourceSpec(spec string, index int) (string, string) {
	spec = strings.TrimSpace(spec)
	if name, path, ok := strings.Cut(spec, "="); ok && name != "" && path != "" && !strings.ContainsAny(name, `/\\`) {
		return name, path
	}
	base := filepath.Base(spec)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" || name == "." || name == "config" {
		name = fmt.Sprintf("extra-%d", index)
	}
	return name, spec
}

func printInventory(configPath string, hosts []inventory.Host, sources []inventory.SourceSummary) {
	fmt.Println("TOML:", configPath)
	for _, source := range sources {
		if source.Err != nil {
			fmt.Printf("SOURCE %-16s error: %v\n", source.Name, source.Err)
		} else {
			fmt.Printf("SOURCE %-16s %4d hosts  %s\n", source.Name, source.Hosts, source.Path)
		}
	}
	sort.SliceStable(hosts, func(i, j int) bool { return hosts[i].ID < hosts[j].ID })
	for _, host := range hosts {
		name := host.DisplayName()
		if name == host.Alias {
			name = ""
		}
		fmt.Printf("HOST   %-24s %-16s %s\n", host.Alias, strings.Join(host.Tags, ","), name)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sshfleet:", err)
	os.Exit(1)
}
