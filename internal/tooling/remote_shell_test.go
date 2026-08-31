package tooling

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRemoteWorkspaceShellRestoresToolsAfterUserRC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote workspace targets Linux")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))

	for _, shellName := range []string{"bash", "zsh", "fish", "sh"} {
		shellPath, err := exec.LookPath(shellName)
		if err != nil {
			t.Logf("skip %s: %v", shellName, err)
			continue
		}
		t.Run(shellName, func(t *testing.T) {
			root := t.TempDir()
			copyRemoteExecutable(t, filepath.Join(repoRoot, "tools", "remote", "run"), filepath.Join(root, "run"))
			copyRemoteExecutable(t, filepath.Join(repoRoot, "tools", "remote", "shell"), filepath.Join(root, "shell"))
			if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, tool := range []string{"lf", "nvim", "dtop", "bat", "sshfleet-editor"} {
				writeRemoteExecutable(t, filepath.Join(root, "bin", tool), "#!/bin/sh\nexit 0\n")
			}

			home := filepath.Join(root, "home")
			if err := os.MkdirAll(filepath.Join(home, ".config", "fish"), 0o700); err != nil {
				t.Fatal(err)
			}
			marker := shellName
			switch shellName {
			case "bash":
				writeRemoteFile(t, filepath.Join(home, ".bashrc"), "case :$PATH: in *:$SSHFLEET_WORKSPACE_ROOT/bin:*) SSHFLEET_RC_LOADED=leaked;; *) SSHFLEET_RC_LOADED="+marker+";; esac\n[ \"$XDG_CONFIG_HOME\" = \"$HOME/.config\" ] || SSHFLEET_RC_LOADED=leaked\nexport SSHFLEET_RC_LOADED\nexport PATH=/usr/bin:/bin\n", 0o600)
			case "zsh":
				writeRemoteFile(t, filepath.Join(home, ".zshrc"), "case :$PATH: in *:$SSHFLEET_WORKSPACE_ROOT/bin:*) SSHFLEET_RC_LOADED=leaked;; *) SSHFLEET_RC_LOADED="+marker+";; esac\n[ \"$XDG_CONFIG_HOME\" = \"$HOME/.config\" ] || SSHFLEET_RC_LOADED=leaked\nexport SSHFLEET_RC_LOADED\nexport PATH=/usr/bin:/bin\n", 0o600)
			case "fish":
				writeRemoteFile(t, filepath.Join(home, ".config", "fish", "config.fish"), "if test \"$XDG_CONFIG_HOME\" = \"$HOME/.config\"; and not contains \"$SSHFLEET_WORKSPACE_ROOT/bin\" $PATH\n    set -gx SSHFLEET_RC_LOADED "+marker+"\nelse\n    set -gx SSHFLEET_RC_LOADED leaked\nend\nset -gx PATH /usr/bin /bin\n", 0o600)
			default:
				writeRemoteFile(t, filepath.Join(home, ".profile"), "case :$PATH: in *:$SSHFLEET_WORKSPACE_ROOT/bin:*) SSHFLEET_RC_LOADED=leaked;; *) SSHFLEET_RC_LOADED="+marker+";; esac\n[ \"$XDG_CONFIG_HOME\" = \"$HOME/.config\" ] || SSHFLEET_RC_LOADED=leaked\nexport SSHFLEET_RC_LOADED\nPATH=/usr/bin:/bin\nexport PATH\n", 0o600)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, filepath.Join(root, "run"), "shell", "keep")
			isolateTestCommand(command)
			command.WaitDelay = time.Second
			command.Env = append(os.Environ(),
				"HOME="+home,
				"SHELL="+shellPath,
				"PATH=/usr/bin:/bin",
				"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
				"ZDOTDIR="+home,
				"TERM=dumb",
			)
			command.Stdin = strings.NewReader("printf 'RC:%s\\n' \"$SSHFLEET_RC_LOADED\"\ncommand -v lf\ncommand -v nvim\ncommand -v dtop\ncommand -v bat\nexit\n")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("workspace shell: %v\n%s", err, output)
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			text := string(output)
			if !strings.Contains(text, "RC:"+marker) {
				t.Fatalf("user rc was not loaded:\n%s", text)
			}
			for _, tool := range []string{"lf", "nvim", "dtop", "bat"} {
				want := filepath.Join(root, "bin", tool)
				if !strings.Contains(text, want) {
					t.Fatalf("%s was hidden by user rc; want %s in:\n%s", tool, want, text)
				}
			}
		})
	}
}

func TestRemoteWorkspaceShellFallsBackFromUnsupportedShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote workspace targets Unix-like shells")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	root := t.TempDir()
	copyRemoteExecutable(t, filepath.Join(repoRoot, "tools", "remote", "run"), filepath.Join(root, "run"))
	copyRemoteExecutable(t, filepath.Join(repoRoot, "tools", "remote", "shell"), filepath.Join(root, "shell"))
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRemoteExecutable(t, filepath.Join(root, "bin", "lf"), "#!/bin/sh\nexit 0\n")

	unsupported := filepath.Join(root, "unusual-shell")
	writeRemoteExecutable(t, unsupported, "#!/bin/sh\nexit 1\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(root, "run"), "shell", "keep")
	isolateTestCommand(command)
	command.WaitDelay = time.Second
	command.Env = append(os.Environ(), "HOME="+root, "SHELL="+unsupported, "PATH=/usr/bin:/bin", "TERM=dumb")
	command.Stdin = strings.NewReader("command -v lf\nexit\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fallback shell: %v\n%s", err, output)
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	text := string(output)
	if !strings.Contains(text, "unsupported interactive shell unusual-shell; using /bin/sh") || !strings.Contains(text, filepath.Join(root, "bin", "lf")) {
		t.Fatalf("unexpected fallback output:\n%s", text)
	}
}

func TestRemoteWorkspaceRunExecsShellForPromptCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote workspace targets Unix-like shells")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	root := t.TempDir()
	copyRemoteExecutable(t, filepath.Join(repoRoot, "tools", "remote", "run"), filepath.Join(root, "run"))
	writeRemoteExecutable(t, filepath.Join(root, "shell"), "#!/bin/sh\nprintf 'SHELL_PID:%s\\n' \"$$\"\nif (: </dev/tty) 2>/dev/null; then echo CONTROLLING_TTY; else echo NO_CONTROLLING_TTY; fi\nexec sleep 30\n")

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(root, "run"), "shell", "keep")
	isolateTestCommand(command)
	command.WaitDelay = 500 * time.Millisecond
	started := time.Now()
	output, err := command.CombinedOutput()
	if err == nil || ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("cancelled workspace = %v, context = %v\n%s", err, ctx.Err(), output)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancelled workspace took %s", elapsed)
	}
	if want := fmt.Sprintf("SHELL_PID:%d", command.Process.Pid); !strings.Contains(string(output), want) {
		t.Fatalf("run did not exec shell; want %q in:\n%s", want, output)
	}
	if !strings.Contains(string(output), "NO_CONTROLLING_TTY") {
		t.Fatalf("workspace test inherited a controlling TTY:\n%s", output)
	}
}

func copyRemoteExecutable(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeRemoteExecutable(t, target, string(data))
}

func writeRemoteExecutable(t *testing.T, path, body string) {
	t.Helper()
	writeRemoteFile(t, path, body, 0o700)
}

func writeRemoteFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}
