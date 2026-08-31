package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

func TestResolveAskPassFallsBackToCurrentBinary(t *testing.T) {
	t.Setenv("SSHF_ASKPASS", "")
	t.Setenv("PATH", "")
	resolution := resolveAskPass()
	if resolution.Error != "" || !resolution.Self || resolution.Origin != "built-in" || resolution.Path == "" {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestTerminalCLIOverridesTOMLWithoutJoiningArgv(t *testing.T) {
	cfg := config.Defaults()
	cfg.Terminal.DefaultShell = "bash"
	cfg.Terminal.ShellArgs = []string{"-l"}
	origin := applyTerminalOverrides(&cfg, "fish", []string{"-d", "значение с пробелом"}, true)
	if origin != platform.OriginCLI || cfg.Terminal.DefaultShell != "fish" || len(cfg.Terminal.ShellArgs) != 2 || cfg.Terminal.ShellArgs[1] != "значение с пробелом" {
		t.Fatalf("terminal override = %s / %#v", origin, cfg.Terminal)
	}
}

func TestTerminalCLIArgumentsAloneMarkCLIOrigin(t *testing.T) {
	cfg := config.Defaults()
	cfg.Terminal.DefaultShell = "bash"
	cfg.Terminal.ShellArgs = []string{"-l"}
	origin := applyTerminalOverrides(&cfg, "", []string{"-d", "значение с пробелом"}, true)
	if origin != platform.OriginCLI || cfg.Terminal.DefaultShell != "bash" || len(cfg.Terminal.ShellArgs) != 2 || cfg.Terminal.ShellArgs[1] != "значение с пробелом" {
		t.Fatalf("terminal argument override = %s / %#v", origin, cfg.Terminal)
	}
}

func TestShellArgFlagsKeepArgumentsSeparateAndRejectUnsafeValues(t *testing.T) {
	var values shellArgFlags
	if err := values.Set("значение с пробелом"); err != nil {
		t.Fatal(err)
	}
	if !values.set || len(values.values) != 1 || values.values[0] != "значение с пробелом" {
		t.Fatalf("shell arguments = %#v", values)
	}
	for _, invalid := range []string{"", "bad\nargument", "bad\x00argument"} {
		if err := values.Set(invalid); err == nil {
			t.Fatalf("unsafe shell argument %q accepted", invalid)
		}
	}
}

func TestResolveAskPassRejectsInvalidExplicitHelper(t *testing.T) {
	t.Setenv("SSHF_ASKPASS", filepath.Join(t.TempDir(), "missing"))
	resolution := resolveAskPass()
	if resolution.Error == "" || resolution.Path != "" || resolution.Origin != "configured" {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestResolveAskPassFindsExplicitHelperNameOnPath(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "custom-askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SSHF_ASKPASS", "custom-askpass")
	resolution := resolveAskPass()
	if resolution.Error != "" || resolution.Path != helper || resolution.Origin != "configured" || resolution.Self {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestHealthcheckExplainsCapabilitiesAndOptionalTools(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ssh", "secret-tool", "ssh-keygen", "age", "nano"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SSHF_COMPANION_DIRS", "")
	t.Setenv("SSHF_ASKPASS", "")
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	if err := runHealthcheckCommand([]string{"--config", os.DevNull}, &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, wanted := range []string{
		"SSH Fleet Console healthcheck",
		"[OK] core/ssh",
		"[OK] credentials/askpass",
		"self-contained second process",
		"[OK] editing/editor",
		"[INFO] containers/docker",
		"[WARN] containers/discovery",
		"[INFO] optional/lf",
		"related menu action is hidden",
		"Summary:",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("healthcheck missing %q:\n%s", wanted, text)
		}
	}
}

func TestHealthcheckFailsOnlyForRequiredCapabilityByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SSHF_COMPANION_DIRS", "")
	t.Setenv("SSHF_ASKPASS", "")
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	if err := runHealthcheckCommand([]string{"--config", os.DevNull}, &output); err == nil {
		t.Fatal("missing required ssh accepted")
	}
	if !strings.Contains(output.String(), "[FAIL] core/ssh") {
		t.Fatalf("missing failure in output:\n%s", output.String())
	}

	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runHealthcheckCommand([]string{"--config", os.DevNull}, &output); err != nil {
		t.Fatalf("optional warnings failed default healthcheck: %v\n%s", err, output.String())
	}
	output.Reset()
	if err := runHealthcheckCommand([]string{"--config", os.DevNull, "--strict"}, &output); err == nil {
		t.Fatal("strict healthcheck accepted missing security capabilities")
	}
}

func TestHealthcheckReportsMissingExplicitShellWithoutBreakingSSHCore(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHF_COMPANION_DIRS", "")
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	if err := runHealthcheckCommand([]string{"--config", os.DevNull, "--shell", "definitely-missing-shell"}, &output); err != nil {
		t.Fatalf("optional local shell broke SSH core: %v\n%s", err, output.String())
	}
	if text := output.String(); !strings.Contains(text, "[WARN] terminal/default-shell") || !strings.Contains(text, "configured shell") || !strings.Contains(text, "[OK] core/ssh") {
		t.Fatalf("missing-shell report:\n%s", text)
	}
}

func TestApplicationHealthIncludesPlatformAndResolvedShell(t *testing.T) {
	results := applicationHealth("missing-ssh", platform.ShellResolution{Effective: "/bin/fish", Origin: platform.OriginCLI}, platform.Capabilities{OS: "testos", Arch: "testarch", PTYBackend: "test-pty", CredentialStore: "test-store"})
	if len(results) < 4 || results[1].Name != "platform" || !strings.Contains(results[1].Path, "testos/testarch") || results[2].Name != "shell" || results[2].Path != "/bin/fish" || string(results[2].Origin) != platform.OriginCLI {
		t.Fatalf("application health = %#v", results)
	}
}
