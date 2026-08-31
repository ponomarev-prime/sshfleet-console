//go:build !windows

package tooling

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestRegressionEntrypointFromSupportedShells(t *testing.T) {
	requireAll := os.Getenv("SSHF_REQUIRE_ALL_SHELLS") == "1"
	root := repositoryRoot(t)

	for _, shellName := range []string{"sh", "bash", "zsh", "fish"} {
		shellName := shellName
		t.Run(shellName, func(t *testing.T) {
			shellPath, err := exec.LookPath(shellName)
			if err != nil {
				if requireAll {
					t.Fatalf("required shell %s is unavailable: %v", shellName, err)
				}
				t.Skipf("optional local shell %s is unavailable: %v", shellName, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, shellPath, "-lc", "make regression")
			command.Dir = root
			command.Env = regressionShellEnvironment(t.TempDir())

			terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 100})
			if err != nil {
				t.Fatalf("start %s PTY: %v", shellName, err)
			}
			var output bytes.Buffer
			copyDone := make(chan struct{})
			go func() {
				_, _ = io.Copy(&output, terminal)
				close(copyDone)
			}()

			waitErr := command.Wait()
			_ = terminal.Close()
			select {
			case <-copyDone:
			case <-time.After(time.Second):
				t.Fatal("PTY output reader did not stop")
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("%s entrypoint timed out:\n%s", shellName, output.String())
			}
			if waitErr != nil {
				t.Fatalf("%s entrypoint: %v\n%s", shellName, waitErr, output.String())
			}
			if !strings.Contains(output.String(), "SSH Fleet regression entrypoint: ok") {
				t.Fatalf("%s did not reach regression probe:\n%s", shellName, output.String())
			}
		})
	}
}

func regressionShellEnvironment(home string) []string {
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") ||
			strings.HasPrefix(entry, "TERM=") ||
			strings.HasPrefix(entry, "ZDOTDIR=") ||
			strings.HasPrefix(entry, "SSHF_REGRESSION_ENTRYPOINT_PROBE=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"HOME="+home,
		"ZDOTDIR="+home,
		"TERM=xterm-256color",
		"SSHF_REGRESSION_ENTRYPOINT_PROBE=1",
	)
}
