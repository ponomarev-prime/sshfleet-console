package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ShellAuto    = "auto"
	OriginCLI    = "cli"
	OriginTOML   = "toml"
	OriginOSAuto = "os-auto"
	OriginHost   = "host"
)

// Capabilities is the explicit boundary between portable application logic
// and facilities that need a native implementation on each operating system.
type Capabilities struct {
	OS                         string
	Arch                       string
	PTYBackend                 string
	EmbeddedTerminalAvailable  bool
	CredentialStore            string
	CredentialStoreImplemented bool
	LocalProbeAvailable        bool
}

func Current() Capabilities {
	capabilities := nativeCapabilities()
	capabilities.OS = runtime.GOOS
	capabilities.Arch = runtime.GOARCH
	return capabilities
}

func (c Capabilities) Platform() string { return c.OS + "/" + c.Arch }

func (c Capabilities) PTYDescription() string {
	if c.EmbeddedTerminalAvailable {
		return c.PTYBackend + " · available"
	}
	return c.PTYBackend + " · unavailable"
}

func UserConfigDir() (string, error) { return os.UserConfigDir() }
func UserCacheDir() (string, error)  { return os.UserCacheDir() }
func UserHomeDir() (string, error)   { return os.UserHomeDir() }

func ExecutableFile(path string) (string, error) { return nativeExecutableFile(path) }

type ShellResolution struct {
	Configured string
	Effective  string
	Args       []string
	Origin     string
	Available  []string
	Error      string
}

func (r ShellResolution) CommandText() string {
	if r.Effective == "" {
		return ""
	}
	parts := append([]string{r.Effective}, r.Args...)
	return strings.Join(parts, " ")
}

// ResolveShell resolves one executable only. Arguments remain an immutable
// argv slice and are never interpreted by a shell. An explicit missing shell
// fails closed; fallback candidates are used only for "auto".
func ResolveShell(configured string, args []string, origin string) ShellResolution {
	return resolveShellFor(runtime.GOOS, configured, args, origin, os.Getenv, exec.LookPath)
}

type lookupFunc func(string) (string, error)
type getenvFunc func(string) string

func resolveShellFor(goos, configured string, args []string, origin string, getenv getenvFunc, lookup lookupFunc) ShellResolution {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = ShellAuto
	}
	result := ShellResolution{Configured: configured, Args: append([]string(nil), args...), Origin: origin}
	candidates := shellCandidates(goos, getenv)
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		path, err := lookup(candidate)
		if err != nil {
			continue
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result.Available = append(result.Available, path)
	}
	if configured != ShellAuto {
		path, err := lookup(configured)
		if err != nil {
			result.Error = fmt.Sprintf("configured shell %q not found", configured)
			if len(result.Available) > 0 {
				result.Error += "; available: " + strings.Join(result.Available, ", ")
			}
			return result
		}
		result.Effective = filepath.Clean(path)
		if result.Origin == "" {
			result.Origin = OriginTOML
		}
		return result
	}
	result.Origin = OriginOSAuto
	if len(result.Available) == 0 {
		result.Error = "OS auto-detection found no supported local shell"
		return result
	}
	result.Effective = result.Available[0]
	return result
}

func shellCandidates(goos string, getenv getenvFunc) []string {
	var candidates []string
	appendCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}
	if goos == "windows" {
		appendCandidate(getenv("COMSPEC"))
		for _, candidate := range []string{"pwsh.exe", "powershell.exe", "cmd.exe"} {
			appendCandidate(candidate)
		}
		return candidates
	}
	appendCandidate(getenv("SHELL"))
	if goos == "darwin" {
		for _, candidate := range []string{"zsh", "bash", "sh"} {
			appendCandidate(candidate)
		}
		return candidates
	}
	for _, candidate := range []string{"bash", "zsh", "fish", "sh"} {
		appendCandidate(candidate)
	}
	return candidates
}
