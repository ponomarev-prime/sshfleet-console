//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

func nativeExecutableFile(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("not an executable regular file")
	}
	return filepath.Clean(absolute), nil
}
