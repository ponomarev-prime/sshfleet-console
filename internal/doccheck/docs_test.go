package doccheck

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var markdownLink = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate doccheck source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("documentation file is empty: %s", path)
	}
	return string(data)
}

func TestCanonicalDocumentationExistsAndHasNoBrokenLocalLinks(t *testing.T) {
	root := repositoryRoot(t)
	required := []string{
		"LICENSE",
		"NOTICE",
		"README.md",
		"SECURITY.md",
		"THIRD_PARTY_NOTICES.md",
		"docs/user-guide.md",
		"docs/configuration.md",
		"docs/features.md",
		"docs/security-model.md",
		"docs/glossary.md",
		"docs/project-goals-and-scenarios.md",
		"docs/releasing.md",
		"docs/publishing.md",
	}
	files := append([]string(nil), required...)
	for _, pattern := range []string{"*.md", "docs/*.md"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, match := range matches {
			name, err := filepath.Rel(root, match)
			if err != nil {
				t.Fatalf("relative path for %s: %v", match, err)
			}
			files = append(files, name)
		}
	}
	seen := make(map[string]bool)
	for _, name := range files {
		if seen[name] {
			continue
		}
		seen[name] = true
		path := filepath.Join(root, name)
		body := readFile(t, path)
		for _, match := range markdownLink.FindAllStringSubmatch(body, -1) {
			target := strings.TrimSpace(match[1])
			if target == "" || strings.HasPrefix(target, "#") ||
				strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target = strings.SplitN(target, "#", 2)[0]
			if target == "" {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(target)))
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s links to missing local target %q: %v", name, target, err)
			}
		}
	}
}

func TestConfigurationReferenceTracksPublicFlagsAndFields(t *testing.T) {
	root := repositoryRoot(t)
	body := readFile(t, filepath.Join(root, "docs", "configuration.md"))
	flags := []string{
		"--config", "--editor", "--inventory", "--list", "--max-concurrent",
		"--no-default-ssh-config", "--no-probe", "--no-user-ssh-config",
		"--overrides-dir", "--probe", "--refresh-interval", "--sources-dir", "--groups-dir",
		"--shell", "--shell-arg", "--ssh-config", "--user-ssh-config", "--version",
	}
	fields := []string{
		"refresh_interval", "connect_timeout", "max_concurrent", "ssh_binary",
		"probe_enabled", "load_user_ssh_config", "sources_dir", "groups_dir", "overrides_dir",
		"editor", "editor_priority", "workspace_bundle", "workspace_cleanup",
		"source_fetch_timeout", "source_max_bytes", "source_state_dir",
		"default_shell", "shell_args",
		"sources_width_percent", "preview_width_percent", "host_column_percent",
		"sources", "credentials", "identities", "host_rules", "hosts", "groups",
		"command_presets",
	}
	for _, term := range append(flags, fields...) {
		if !strings.Contains(body, term) {
			t.Errorf("configuration reference does not mention %q", term)
		}
	}
}

func TestUserGuideTracksInteractiveContract(t *testing.T) {
	root := repositoryRoot(t)
	body := readFile(t, filepath.Join(root, "docs", "user-guide.md"))
	required := []string{
		"SOURCES", "HOSTS", "PREVIEW", "All available", "Enter", "Shift+K",
		"Open terminal tab", "Open terminal in Preview", "Open SSH Fleet workspace",
		"nvim → vim → nano", "50 строк", "Git endpoint", "Host overlay",
		"Groups", "groups.d", "Group membership", "Manage group membership", "source:alias",
		"dtop", "█", "░", "Healthcheck", "Ctrl+1", "Alt+1", "Ctrl+N", "Ctrl+G", "Ctrl+D", "Ctrl+]", "LAST SESSION",
	}
	for _, term := range required {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(term)) {
			t.Errorf("user guide does not mention %q", term)
		}
	}
}

func TestREADMEIsAnEntryPointNotASecondSpecification(t *testing.T) {
	root := repositoryRoot(t)
	body := readFile(t, filepath.Join(root, "README.md"))
	for _, target := range []string{
		"docs/user-guide.md", "docs/configuration.md", "docs/features.md",
		"docs/security-model.md", "docs/glossary.md", "docs/publishing.md",
		"LICENSE", "THIRD_PARTY_NOTICES.md", "make regression",
	} {
		if !strings.Contains(body, target) {
			t.Errorf("README does not route readers to %q", target)
		}
	}
	retiredPath := strings.Join([]string{"/home", "alex", "my_code", "sshfleet`"}, "/")
	if strings.Contains(body, retiredPath) {
		t.Error("README contains the retired repository path")
	}
}

func TestCanonicalCommandAndCollisionSafeAliasesAreDocumentedAndPackaged(t *testing.T) {
	root := repositoryRoot(t)
	readme := readFile(t, filepath.Join(root, "README.md"))
	for _, term := range []string{"`sshfleet`", "`sf`", "`sshf`", "v1.0.0"} {
		if !strings.Contains(readme, term) {
			t.Errorf("README does not document command contract %q", term)
		}
	}
	installer := readFile(t, filepath.Join(root, "install.sh"))
	for _, contract := range []string{
		"$bin_dir/sshfleet",
		"install_compat_alias sshf",
		"install_compat_alias sf",
		"alias was not changed",
	} {
		if !strings.Contains(installer, contract) {
			t.Errorf("installer does not enforce %q", contract)
		}
	}
	release := readFile(t, filepath.Join(root, "tools", "build-release.sh"))
	for _, path := range []string{"$stage/bin/sshfleet", "$stage/tools/bin/sshfleet", "$stage/tools/bin/sshf"} {
		if !strings.Contains(release, path) {
			t.Errorf("release archive does not include %q", path)
		}
	}
}

func TestCleanPublicSnapshotPreservesVerifiedTreeAndSubmodulePins(t *testing.T) {
	root := repositoryRoot(t)
	script := readFile(t, filepath.Join(root, "tools", "create-public-snapshot.sh"))
	for _, contract := range []string{
		"source worktree must be clean",
		"ls-tree -r",
		"update-index --add --cacheinfo",
		"staged tree differs from verified source tree",
		"No remote or tags were created",
	} {
		if !strings.Contains(script, contract) {
			t.Errorf("public snapshot script does not enforce %q", contract)
		}
	}
	publishing := readFile(t, filepath.Join(root, "docs", "publishing.md"))
	for _, command := range []string{"make test-public-snapshot", "make public-snapshot"} {
		if !strings.Contains(publishing, command) {
			t.Errorf("publishing guide does not document %q", command)
		}
	}
}

func TestApacheLicenseAndDependencyNoticesAreReleaseContracts(t *testing.T) {
	root := repositoryRoot(t)
	license := readFile(t, filepath.Join(root, "LICENSE"))
	if !strings.Contains(license, "Apache License") || !strings.Contains(license, "Version 2.0, January 2004") {
		t.Fatal("LICENSE is not Apache-2.0")
	}
	if !strings.Contains(readFile(t, filepath.Join(root, "NOTICE")), "SSH Fleet Console contributors") {
		t.Fatal("NOTICE does not identify project contributors")
	}
	notices := readFile(t, filepath.Join(root, "THIRD_PARTY_NOTICES.md"))
	licenseFiles, err := filepath.Glob(filepath.Join(root, "third_party", "licenses", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(licenseFiles) != 21 {
		t.Fatalf("preserved production dependency licenses = %d, want 21", len(licenseFiles))
	}
	for _, path := range licenseFiles {
		name, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(notices, filepath.ToSlash(name)) {
			t.Errorf("third-party notices do not reference %s", name)
		}
	}
	for _, contract := range []struct {
		path string
		term string
	}{
		{"Makefile", "test-licenses:"},
		{"tools/regression.sh", "run_step licenses make test-licenses"},
		{"tools/build-release.sh", "THIRD_PARTY_NOTICES.md"},
		{"tools/test-versioning.sh", "/third_party/licenses/"},
	} {
		if !strings.Contains(readFile(t, filepath.Join(root, filepath.FromSlash(contract.path))), contract.term) {
			t.Errorf("%s does not contain licensing contract %q", contract.path, contract.term)
		}
	}
}

func TestPublishingGuideKeepsPrivateHistoryAndScreenshotsOutOfPublicBoundary(t *testing.T) {
	root := repositoryRoot(t)
	body := strings.ToLower(readFile(t, filepath.Join(root, "docs", "publishing.md")))
	for _, term := range []string{
		"apache license 2.0", "license", "notice", "third-party", "clean public history",
		"push --mirror", "archive/pre-tabs", "archive/tabs", "make test-public-screenshots",
		"private vulnerability reporting", "immutable releases", "prevent self-review",
	} {
		if !strings.Contains(body, term) {
			t.Errorf("publishing guide does not mention %q", term)
		}
	}
}

func TestRegressionScriptAndDocsRequireBoundedGoStages(t *testing.T) {
	root := repositoryRoot(t)
	script := readFile(t, filepath.Join(root, "tools", "regression.sh"))
	for _, command := range []string{
		"run_step unit go test -timeout=2m ./...",
		"run_step shell-entrypoints env SSHF_REQUIRE_ALL_SHELLS=1 make test-shell-entrypoints",
		"run_step race go test -race -timeout=4m ./...",
		"run_step coverage go test -timeout=2m",
	} {
		if !strings.Contains(script, command) {
			t.Errorf("regression script does not enforce %q", command)
		}
	}
	installFull := strings.Index(script, `run_step install-full "$repo_root/tools/test-install-full.sh"`)
	toolchainCheck := strings.Index(script, `run_step toolchain "$repo_root/tools/check.sh"`)
	if installFull < 0 || toolchainCheck < 0 || installFull > toolchainCheck {
		t.Error("regression must prepare a fresh clone with install-full before checking the optional toolchain")
	}
	for _, name := range []string{"README.md", filepath.Join("docs", "features.md")} {
		body := strings.ToLower(readFile(t, filepath.Join(root, name)))
		if !strings.Contains(body, "watchdog") || !strings.Contains(body, "stack trace") {
			t.Errorf("%s does not document watchdog diagnostics", name)
		}
		for _, shell := range []string{"sh", "bash", "zsh", "fish"} {
			if !strings.Contains(body, shell) {
				t.Errorf("%s does not document regression launch from %s", name, shell)
			}
		}
	}
}
