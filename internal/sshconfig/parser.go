package sshconfig

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

// Aliases conservatively enumerates literal Host aliases. Wildcard patterns
// still affect connection resolution through OpenSSH, but cannot be listed as
// concrete machines without a separate inventory.
func Aliases(path string) ([]string, error) {
	seenFiles := make(map[string]bool)
	aliases := make(map[string]struct{})
	if err := parseFile(path, seenFiles, aliases); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(aliases))
	for alias := range aliases {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out, nil
}

func parseFile(path string, seenFiles map[string]bool, aliases map[string]struct{}) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve ssh config path %q: %w", path, err)
	}
	if seenFiles[abs] {
		return nil
	}
	seenFiles[abs] = true

	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		words, err := splitWords(scanner.Text())
		if err != nil {
			return fmt.Errorf("%s:%d: %w", abs, lineNo, err)
		}
		if len(words) < 2 {
			continue
		}
		switch strings.ToLower(words[0]) {
		case "host":
			for _, candidate := range words[1:] {
				if isLiteralAlias(candidate) {
					aliases[candidate] = struct{}{}
				}
			}
		case "include":
			for _, pattern := range words[1:] {
				matches, err := includeMatches(pattern)
				if err != nil {
					return fmt.Errorf("%s:%d: include %q: %w", abs, lineNo, pattern, err)
				}
				for _, match := range matches {
					if err := parseFile(match, seenFiles, aliases); err != nil {
						return fmt.Errorf("included from %s:%d: %w", abs, lineNo, err)
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", abs, err)
	}
	return nil
}

func includeMatches(pattern string) ([]string, error) {
	pattern = os.ExpandEnv(pattern)
	home, err := platform.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if pattern == "~" || strings.HasPrefix(pattern, "~/") {
		pattern = filepath.Join(home, strings.TrimPrefix(pattern, "~/"))
	} else if !filepath.IsAbs(pattern) {
		// OpenSSH resolves relative Include paths in a user config from ~/.ssh.
		pattern = filepath.Join(home, ".ssh", pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func isLiteralAlias(s string) bool {
	return s != "" && !strings.HasPrefix(s, "!") && !strings.ContainsAny(s, "*?!")
}

func splitWords(line string) ([]string, error) {
	var words []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}

	for _, r := range line {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '#' && current.Len() == 0:
			flush()
			return words, nil
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r) || r == '=':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return words, nil
}
