package workspace

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const (
	maxArchiveBytes    = 256 << 20
	maxExpandedBytes   = 768 << 20
	maxArchiveEntries  = 20000
	checksumFileSuffix = ".sha256"
)

var requiredEntries = map[string]bool{
	"run":                 false,
	"shell":               false,
	"manifest.toml":       false,
	"bin/lf":              false,
	"bin/nvim":            false,
	"bin/dtop":            false,
	"bin/bat":             false,
	"bin/sshfleet-open":   false,
	"bin/sshfleet-editor": false,
}

type Tool string

const (
	ToolShell    Tool = "shell"
	ToolLF       Tool = "lf"
	ToolNvim     Tool = "nvim"
	ToolDtop     Tool = "dtop"
	ToolSelfTest Tool = "self-test"
)

func (t Tool) Valid() bool {
	switch t {
	case ToolShell, ToolLF, ToolNvim, ToolDtop, ToolSelfTest:
		return true
	default:
		return false
	}
}

// OpenValidated verifies the local checksum and the closed archive shape before
// any bytes are sent to a remote host. The returned file is rewound.
func OpenValidated(filePath string) (*os.File, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat workspace bundle: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxArchiveBytes {
		return nil, fmt.Errorf("workspace bundle must be a regular file between 1 and %d bytes", maxArchiveBytes)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open workspace bundle: %w", err)
	}
	closeOnError := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return closeOnError(fmt.Errorf("hash workspace bundle: %w", err))
	}
	expected, err := readChecksum(filePath + checksumFileSuffix)
	if err != nil {
		return closeOnError(err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return closeOnError(errors.New("workspace bundle checksum mismatch"))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return closeOnError(fmt.Errorf("rewind workspace bundle: %w", err))
	}
	if err := validateArchive(file); err != nil {
		return closeOnError(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return closeOnError(fmt.Errorf("rewind workspace bundle: %w", err))
	}
	return file, nil
}

func readChecksum(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read workspace checksum: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", errors.New("invalid workspace checksum file")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("invalid workspace checksum file")
	}
	return strings.ToLower(fields[0]), nil
}

func validateArchive(reader io.Reader) error {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("open workspace gzip: %w", err)
	}
	defer gz.Close()
	found := make(map[string]bool, len(requiredEntries))
	var total int64
	entries := 0
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read workspace tar: %w", err)
		}
		entries++
		if entries > maxArchiveEntries {
			return errors.New("workspace bundle has too many entries")
		}
		name := strings.TrimPrefix(header.Name, "./")
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" {
			continue
		}
		if path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe workspace archive path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if total > maxExpandedBytes {
				return errors.New("workspace bundle expands beyond the size limit")
			}
		default:
			return fmt.Errorf("unsupported workspace archive entry %q", header.Name)
		}
		if _, required := requiredEntries[name]; required {
			found[name] = true
		}
	}
	for name := range requiredEntries {
		if !found[name] {
			return fmt.Errorf("workspace bundle is missing %s", name)
		}
	}
	return nil
}
