package tooling

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLFOpenUsesBatAndPagesOnlyAboveFiftyLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX companion helpers are not shipped on Windows")
	}
	repo := repositoryRoot(t)
	helper := filepath.Join(repo, "tools", "launchers", "sshfleet-open")
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bat.log")
	writeExecutable(t, filepath.Join(fakeBin, "bat"), "#!/bin/sh\nprintf '%s\\n' \"${BAT_PAGER:-}|$*\" > \"$SSHFLEET_TEST_LOG\"\n")
	writeExecutable(t, filepath.Join(fakeBin, "less"), "#!/bin/sh\nexit 0\n")

	small := filepath.Join(t.TempDir(), "small file.txt")
	large := filepath.Join(t.TempDir(), "large file.txt")
	if err := os.WriteFile(small, []byte(strings.Repeat("small\n", 50)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, []byte(strings.Repeat("large\n", 51)), 0o600); err != nil {
		t.Fatal(err)
	}

	runHelper(t, helper, fakeBin, logPath, nil, small)
	assertLogContains(t, logPath, "|--paging=never -- "+small)
	runHelper(t, helper, fakeBin, logPath, nil, large)
	assertLogContains(t, logPath, "less -RF|--paging=always -- "+large)

	noLessBin := t.TempDir()
	writeExecutable(t, filepath.Join(noLessBin, "bat"), "#!/bin/sh\nprintf '%s\\n' \"${BAT_PAGER:-}|$*\" > \"$SSHFLEET_TEST_LOG\"\n")
	writeExecutable(t, filepath.Join(noLessBin, "sed"), "#!/bin/sh\nprintf '51\\n'\n")
	runHelper(t, helper, noLessBin, logPath, []string{"PATH=" + noLessBin}, large)
	assertLogContains(t, logPath, "builtin|--paging=always -- "+large)
}

func TestLFEditorUsesExplicitOverrideThenNvimVimNano(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX companion helpers are not shipped on Windows")
	}
	repo := repositoryRoot(t)
	helper := filepath.Join(repo, "tools", "launchers", "sshfleet-editor")
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "editor.log")
	for _, name := range []string{"nvim", "vim", "nano"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nprintf '"+name+"|%s\\n' \"$*\" > \"$SSHFLEET_TEST_LOG\"\n")
	}

	isolatedPath := "PATH=" + fakeBin
	runHelper(t, helper, fakeBin, logPath, []string{isolatedPath, "SSHFLEET_EDITOR=vim"}, "file with spaces.txt")
	assertLogContains(t, logPath, "vim|file with spaces.txt")
	runHelper(t, helper, fakeBin, logPath, []string{isolatedPath}, "file.txt")
	assertLogContains(t, logPath, "nvim|file.txt")
	if err := os.Remove(filepath.Join(fakeBin, "nvim")); err != nil {
		t.Fatal(err)
	}
	runHelper(t, helper, fakeBin, logPath, []string{isolatedPath}, "file.txt")
	assertLogContains(t, logPath, "vim|file.txt")
	if err := os.Remove(filepath.Join(fakeBin, "vim")); err != nil {
		t.Fatal(err)
	}
	runHelper(t, helper, fakeBin, logPath, []string{isolatedPath}, "file.txt")
	assertLogContains(t, logPath, "nano|file.txt")
}

func TestLFConfigMapsEnterAndEditorToReviewedHelpers(t *testing.T) {
	configPath := filepath.Join(repositoryRoot(t), "tools", "config", "lf", "lfrc")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config := string(data)
	for _, want := range []string{
		"cmd open $sshfleet-open \"$f\"",
		"map <enter> open",
		"map e $sshfleet-editor \"$f\"",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("lf config does not contain %q", want)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func runHelper(t *testing.T, helper, fakeBin, logPath string, extraEnv []string, args ...string) {
	t.Helper()
	command := exec.Command(helper, args...)
	pathValue := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	for _, value := range extraEnv {
		if strings.HasPrefix(value, "PATH=") {
			pathValue = strings.TrimPrefix(value, "PATH=")
		}
	}
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "PATH=") || strings.HasPrefix(value, "SSHFLEET_EDITOR=") || strings.HasPrefix(value, "SSHFLEET_TEST_LOG=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env, "PATH="+pathValue, "SSHFLEET_TEST_LOG="+logPath)
	for _, value := range extraEnv {
		if !strings.HasPrefix(value, "PATH=") {
			command.Env = append(command.Env, value)
		}
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(helper), err, output)
	}
}

func assertLogContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("log %q does not contain %q", data, want)
	}
}
