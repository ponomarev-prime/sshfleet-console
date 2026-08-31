package knownhosts

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

type Plan struct {
	Lookup               string
	File                 string
	Line                 int
	PresentedFingerprint string
	StoredFingerprints   []string
	digest               [sha256.Size]byte
}

type Applied struct {
	BackupPath string
	ToolOutput string
}

// Inspect performs only read-only checks. The reported file comes from
// OpenSSH's own host-key error and, when possible, is checked against the
// effective UserKnownHostsFile list from ssh -G.
func Inspect(lookup string, candidateFiles []string, reportedFile string, line int, presented string) (Plan, error) {
	lookup = strings.TrimSpace(lookup)
	if lookup == "" {
		return Plan{}, errors.New("cannot determine the known_hosts lookup name")
	}
	if strings.TrimSpace(presented) == "" {
		return Plan{}, errors.New("OpenSSH did not report the presented host-key fingerprint")
	}

	candidates, err := expandedPaths(candidateFiles)
	if err != nil {
		return Plan{}, err
	}
	reported, err := expandPath(reportedFile)
	if err != nil {
		return Plan{}, err
	}
	if reported != "" && len(candidates) > 0 && !containsPath(candidates, reported) {
		return Plan{}, fmt.Errorf("refusing file outside effective UserKnownHostsFile: %s", reported)
	}

	file := reported
	if file == "" {
		for _, candidate := range candidates {
			found, _, findErr := find(candidate, lookup)
			if findErr != nil {
				continue
			}
			if found {
				file = candidate
				break
			}
		}
	}
	if file == "" {
		return Plan{}, errors.New("no matching user known_hosts entry found")
	}
	info, err := safeRegularFile(file)
	if err != nil {
		return Plan{}, err
	}
	_ = info

	found, keyLines, err := find(file, lookup)
	if err != nil {
		return Plan{}, err
	}
	if !found {
		return Plan{}, fmt.Errorf("no entry for %q in %s", lookup, file)
	}
	fingerprints, err := fingerprints(keyLines)
	if err != nil {
		return Plan{}, err
	}
	digest, err := fileDigest(file)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Lookup:               lookup,
		File:                 file,
		Line:                 line,
		PresentedFingerprint: presented,
		StoredFingerprints:   fingerprints,
		digest:               digest,
	}, nil
}

// Apply creates a unique backup beside known_hosts, verifies that the file has
// not changed since Inspect, and asks ssh-keygen to remove all entries for the
// exact lookup token. It never scans, downloads, or accepts a replacement key.
func Apply(plan Plan) (Applied, error) {
	if plan.Lookup == "" || plan.File == "" || plan.PresentedFingerprint == "" {
		return Applied{}, errors.New("incomplete host-key repair plan")
	}
	info, err := safeRegularFile(plan.File)
	if err != nil {
		return Applied{}, err
	}
	currentDigest, err := fileDigest(plan.File)
	if err != nil {
		return Applied{}, err
	}
	if currentDigest != plan.digest {
		return Applied{}, errors.New("known_hosts changed after inspection; inspect again")
	}

	backup, err := backupFile(plan.File, info.Mode().Perm())
	if err != nil {
		return Applied{}, fmt.Errorf("create backup: %w", err)
	}

	working, err := workingCopy(plan.File, info.Mode().Perm())
	if err != nil {
		return Applied{BackupPath: backup}, fmt.Errorf("create working copy: %w", err)
	}
	defer os.Remove(working)
	defer os.Remove(working + ".old")

	cmd := exec.Command("ssh-keygen", "-R", plan.Lookup, "-f", working)
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		if found, _, findErr := find(working, plan.Lookup); findErr != nil {
			runErr = findErr
		} else if found {
			runErr = errors.New("ssh-keygen left matching entries behind")
		}
	}
	if runErr != nil {
		return Applied{BackupPath: backup, ToolOutput: strings.TrimSpace(string(output))}, fmt.Errorf("remove key in working copy: %w; original unchanged", runErr)
	}
	latestDigest, err := fileDigest(plan.File)
	if err != nil {
		return Applied{BackupPath: backup, ToolOutput: strings.TrimSpace(string(output))}, err
	}
	if latestDigest != plan.digest {
		return Applied{BackupPath: backup, ToolOutput: strings.TrimSpace(string(output))}, errors.New("known_hosts changed during repair; original left unchanged")
	}
	if err := os.Rename(working, plan.File); err != nil {
		return Applied{BackupPath: backup, ToolOutput: strings.TrimSpace(string(output))}, fmt.Errorf("install repaired known_hosts: %w", err)
	}
	if err := syncDirectory(filepath.Dir(plan.File)); err != nil {
		return Applied{BackupPath: backup, ToolOutput: strings.TrimSpace(string(output))}, fmt.Errorf("sync repaired known_hosts: %w", err)
	}

	return Applied{BackupPath: backup, ToolOutput: strings.TrimSpace(string(output))}, nil
}

func find(file, lookup string) (bool, []string, error) {
	cmd := exec.Command("ssh-keygen", "-F", lookup, "-f", file)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("find %q in %s: %v: %s", lookup, file, err, strings.TrimSpace(string(output)))
	}
	var keyLines []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			keyLines = append(keyLines, line)
		}
	}
	return len(keyLines) > 0, keyLines, nil
}

func fingerprints(lines []string) ([]string, error) {
	seen := make(map[string]bool)
	var result []string
	for _, line := range lines {
		fields := strings.Fields(line)
		keyIndex := 1
		if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
			keyIndex = 2
		}
		if len(fields) <= keyIndex+1 {
			continue
		}
		key := fields[keyIndex] + " " + fields[keyIndex+1] + "\n"
		cmd := exec.Command("ssh-keygen", "-l", "-E", "sha256", "-f", "-")
		cmd.Stdin = strings.NewReader(key)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("fingerprint stored host key: %v: %s", err, strings.TrimSpace(string(output)))
		}
		parts := strings.Fields(string(output))
		for _, part := range parts {
			if strings.HasPrefix(part, "SHA256:") && !seen[part] {
				seen[part] = true
				result = append(result, part)
				break
			}
		}
	}
	if len(result) == 0 {
		return nil, errors.New("could not fingerprint stored host keys")
	}
	return result, nil
}

func expandedPaths(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		for _, item := range strings.Fields(path) {
			if item == "none" {
				continue
			}
			expanded, err := expandPath(item)
			if err != nil {
				return nil, err
			}
			if expanded != "" && !containsPath(result, expanded) {
				result = append(result, expanded)
			}
		}
	}
	return result, nil
}

func expandPath(path string) (string, error) {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "" || path == "none" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := platform.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func safeRegularFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlinked known_hosts file: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("known_hosts is not a regular file: %s", path)
	}
	return info, nil
}

func fileDigest(path string) ([sha256.Size]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func backupFile(source string, mode os.FileMode) (string, error) {
	directory := filepath.Dir(source)
	base := filepath.Base(source) + ".sshfleet-backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	for attempt := 0; attempt < 100; attempt++ {
		name := filepath.Join(directory, base)
		if attempt > 0 {
			name += fmt.Sprintf("-%d", attempt)
		}
		input, err := os.Open(source)
		if err != nil {
			return "", err
		}
		output, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			_ = input.Close()
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		_, copyErr := io.Copy(output, input)
		closeInputErr := input.Close()
		syncErr := output.Sync()
		closeOutputErr := output.Close()
		if err := errors.Join(copyErr, closeInputErr, syncErr, closeOutputErr); err != nil {
			return name, err
		}
		if err := syncDirectory(directory); err != nil {
			return name, err
		}
		return name, nil
	}
	return "", errors.New("could not allocate a unique backup name")
}

func workingCopy(source string, mode os.FileMode) (string, error) {
	directory := filepath.Dir(source)
	temporary, err := os.CreateTemp(directory, ".sshfleet-known-hosts-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
		return "", err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		_ = input.Close()
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
		return "", err
	}
	closeInputErr := input.Close()
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
		return "", errors.Join(closeInputErr, err)
	}
	closeOutputErr := temporary.Close()
	if err := errors.Join(closeInputErr, closeOutputErr); err != nil {
		_ = os.Remove(temporaryName)
		return "", err
	}
	return temporaryName, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
