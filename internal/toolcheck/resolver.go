package toolcheck

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

type Origin string

const (
	OriginSSHFleet Origin = "sshfleet"
	OriginSystem   Origin = "system"
	OriginMissing  Origin = "missing"
)

type Result struct {
	Name   string
	Path   string
	Origin Origin
	Error  string
}

// Resolve searches SSH Fleet Console-owned companion directories before the system
// PATH. It never uses the current working directory as an implicit tool source.
func Resolve(name string) Result {
	name = strings.TrimSpace(name)
	result := Result{Name: name, Origin: OriginMissing}
	if name == "" || strings.ContainsAny(name, " \t\r\n\x00") {
		result.Error = "tool name must be one executable"
		return result
	}
	own := companionDirs()
	if filepath.IsAbs(name) || strings.ContainsRune(name, filepath.Separator) {
		path, err := executableFile(name)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Path, result.Origin = path, OriginSystem
		for _, dir := range own {
			if within(path, dir) {
				result.Origin = OriginSSHFleet
				break
			}
		}
		return result
	}
	for _, dir := range own {
		path, err := executableFile(filepath.Join(dir, name))
		if err == nil {
			result.Path, result.Origin = path, OriginSSHFleet
			return result
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.TrimSpace(dir) == "" || dir == "." {
			continue
		}
		absolute, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		skip := false
		for _, ownDir := range own {
			if samePath(absolute, ownDir) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		path, err := executableFile(filepath.Join(absolute, name))
		if err == nil {
			result.Path, result.Origin = path, OriginSystem
			return result
		}
	}
	result.Error = "not found"
	return result
}

func ResolveEditor(priority []string) Result {
	if len(priority) == 0 {
		priority = []string{"nvim", "vim", "nano"}
	}
	errors := make([]string, 0, len(priority))
	for _, name := range priority {
		result := Resolve(name)
		if result.Error == "" {
			return result
		}
		errors = append(errors, name+": "+result.Error)
	}
	return Result{Name: "editor", Origin: OriginMissing, Error: strings.Join(errors, "; ")}
}

func Health(names []string) []Result {
	results := make([]Result, 0, len(names))
	for _, name := range names {
		results = append(results, Resolve(name))
	}
	return results
}

func companionDirs() []string {
	var candidates []string
	candidates = append(candidates, filepath.SplitList(os.Getenv("SSHF_COMPANION_DIRS"))...)
	if executable, err := os.Executable(); err == nil {
		executable, _ = filepath.EvalSymlinks(executable)
		dir := filepath.Dir(executable)
		root := filepath.Dir(dir)
		candidates = append(candidates, filepath.Join(root, "tools", "bin"), filepath.Join(root, ".toolchain", "bin"))
	}
	seen := make(map[string]struct{}, len(candidates))
	dirs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		if _, exists := seen[absolute]; exists {
			continue
		}
		seen[absolute] = struct{}{}
		dirs = append(dirs, absolute)
	}
	return dirs
}

func executableFile(path string) (string, error) {
	return platform.ExecutableFile(path)
}

func samePath(left, right string) bool {
	leftPath, leftErr := filepath.EvalSymlinks(left)
	rightPath, rightErr := filepath.EvalSymlinks(right)
	if leftErr == nil {
		left = leftPath
	}
	if rightErr == nil {
		right = rightPath
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func within(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
