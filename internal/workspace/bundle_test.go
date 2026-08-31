package workspace

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenValidatedAcceptsClosedChecksummedBundle(t *testing.T) {
	filePath := writeTestBundle(t, nil)
	file, err := OpenValidated(filePath)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenValidated(filePath); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered bundle error = %v", err)
	}
}

func TestOpenValidatedRejectsTraversal(t *testing.T) {
	filePath := writeTestBundle(t, map[string]string{"../escape": "bad"})
	if _, err := OpenValidated(filePath); err == nil || !strings.Contains(err.Error(), "unsafe workspace archive path") {
		t.Fatalf("traversal error = %v", err)
	}
}

func TestBuiltBundleWhenConfigured(t *testing.T) {
	filePath := os.Getenv("SSHF_WORKSPACE_BUNDLE_TEST")
	if filePath == "" {
		t.Skip("SSHF_WORKSPACE_BUNDLE_TEST is not set")
	}
	file, err := OpenValidated(filePath)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}

func writeTestBundle(t *testing.T, extra map[string]string) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "bundle.tar.gz")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	archive := tar.NewWriter(gz)
	entries := map[string]string{
		"run":                 "#!/bin/sh\n",
		"shell":               "#!/bin/sh\n",
		"manifest.toml":       "version = 1\n",
		"bin/lf":              "lf",
		"bin/nvim":            "nvim",
		"bin/dtop":            "dtop",
		"bin/bat":             "bat",
		"bin/sshfleet-open":   "open",
		"bin/sshfleet-editor": "editor",
	}
	for name, value := range extra {
		entries[name] = value
	}
	for name, value := range entries {
		header := &tar.Header{Name: name, Mode: 0o700, Size: int64(len(value)), Typeflag: tar.TypeReg}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filePath+checksumFileSuffix, []byte(fmt.Sprintf("%x  bundle.tar.gz\n", sum)), 0o600); err != nil {
		t.Fatal(err)
	}
	return filePath
}
