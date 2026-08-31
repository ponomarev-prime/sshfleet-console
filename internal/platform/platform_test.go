package platform

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestResolveShellPrecedenceAndFailClosed(t *testing.T) {
	paths := map[string]string{
		"/bin/fish": "/bin/fish",
		"bash":      "/usr/bin/bash",
		"zsh":       "/usr/bin/zsh",
	}
	lookup := func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", errors.New("missing")
	}
	getenv := func(name string) string {
		if name == "SHELL" {
			return "/bin/fish"
		}
		return ""
	}

	auto := resolveShellFor("linux", "auto", []string{"-l"}, OriginTOML, getenv, lookup)
	if auto.Effective != "/bin/fish" || auto.Origin != OriginOSAuto || !reflect.DeepEqual(auto.Args, []string{"-l"}) {
		t.Fatalf("auto resolution = %#v", auto)
	}
	explicit := resolveShellFor("linux", "zsh", []string{"-d", "путь с пробелом"}, OriginCLI, getenv, lookup)
	if explicit.Effective != "/usr/bin/zsh" || explicit.Origin != OriginCLI || !reflect.DeepEqual(explicit.Args, []string{"-d", "путь с пробелом"}) {
		t.Fatalf("explicit resolution = %#v", explicit)
	}
	missing := resolveShellFor("linux", "missing-shell", nil, OriginTOML, getenv, lookup)
	if missing.Effective != "" || missing.Error == "" || missing.Origin != OriginTOML {
		t.Fatalf("missing explicit shell silently fell back = %#v", missing)
	}
}

func TestWindowsAutoDetectionUsesCOMSPECAndSeparateArgv(t *testing.T) {
	lookup := func(name string) (string, error) {
		if name == `C:\Program Files\PowerShell\7\pwsh.exe` {
			return name, nil
		}
		return "", errors.New("missing")
	}
	getenv := func(name string) string {
		if name == "COMSPEC" {
			return `C:\Program Files\PowerShell\7\pwsh.exe`
		}
		return ""
	}
	got := resolveShellFor("windows", "auto", []string{"-NoLogo", "значение с пробелом"}, OriginTOML, getenv, lookup)
	if got.Effective == "" || got.Origin != OriginOSAuto || len(got.Args) != 2 || got.Args[1] != "значение с пробелом" {
		t.Fatalf("windows resolution = %#v", got)
	}
}

func TestCurrentCapabilitiesDeclareNativeBoundary(t *testing.T) {
	got := Current()
	if got.OS == "" || got.Arch == "" || got.PTYBackend == "" || got.CredentialStore == "" {
		t.Fatalf("incomplete capabilities = %#v", got)
	}
	if got.Platform() != runtime.GOOS+"/"+runtime.GOARCH || !strings.Contains(got.PTYDescription(), got.PTYBackend) {
		t.Fatalf("capability descriptions = %#v / %q", got, got.PTYDescription())
	}
	for name, directory := range map[string]func() (string, error){"config": UserConfigDir, "cache": UserCacheDir, "home": UserHomeDir} {
		if path, err := directory(); err != nil || path == "" {
			t.Fatalf("%s directory = %q, %v", name, path, err)
		}
	}
}

func TestResolutionDescriptionsAndMissingAuto(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("missing") }
	got := resolveShellFor("darwin", "auto", nil, OriginTOML, func(string) string { return "" }, missing)
	if got.Error == "" || got.Effective != "" || got.CommandText() != "" {
		t.Fatalf("missing auto = %#v", got)
	}
	resolved := ShellResolution{Effective: "/bin/zsh", Args: []string{"-l", "a b"}}
	if resolved.CommandText() != "/bin/zsh -l a b" {
		t.Fatalf("command text = %q", resolved.CommandText())
	}
}

func TestExecutableFileUsesNativeSemantics(t *testing.T) {
	dir := t.TempDir()
	name := "tool"
	mode := os.FileMode(0o700)
	if runtime.GOOS == "windows" {
		name = "tool.exe"
		mode = 0o600
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fixture"), mode); err != nil {
		t.Fatal(err)
	}
	resolved, err := ExecutableFile(path)
	if err != nil || resolved == "" {
		t.Fatalf("executable = %q, %v", resolved, err)
	}
}
