//go:build windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func nativeExecutableFile(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	candidates := []string{absolute}
	if filepath.Ext(absolute) == "" {
		extensions := filepath.SplitList(strings.ReplaceAll(os.Getenv("PATHEXT"), ";", string(os.PathListSeparator)))
		if len(extensions) == 0 {
			extensions = []string{".COM", ".EXE", ".BAT", ".CMD"}
		}
		for _, extension := range extensions {
			candidates = append(candidates, absolute+extension)
		}
	}
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.Mode().IsRegular() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("not an executable regular file")
}
