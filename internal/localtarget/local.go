package localtarget

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func InteractiveCommand(host inventory.Host) (*exec.Cmd, error) {
	if host.TargetTransport() != inventory.TransportLocal {
		return nil, fmt.Errorf("host %q is not a direct local target", host.Alias)
	}
	if err := host.Validate(); err != nil {
		return nil, err
	}
	path, err := exec.LookPath(host.Shell)
	if err != nil {
		return nil, fmt.Errorf("find local shell %q: %w", host.Shell, err)
	}
	cmd := exec.Command(path, host.ShellArgs...) // #nosec G204 -- trusted local_config supplies separate executable and argv fields.
	if err := configureDirectory(cmd, host.WorkingDirectory); err != nil {
		return nil, err
	}
	return cmd, nil
}

func Command(ctx context.Context, host inventory.Host, argv []string) (*exec.Cmd, error) {
	if host.TargetTransport() != inventory.TransportLocal {
		return nil, fmt.Errorf("host %q is not a direct local target", host.Alias)
	}
	if len(argv) == 0 || argv[0] == "" || strings.ContainsAny(argv[0], "\x00\r\n") {
		return nil, fmt.Errorf("local command argv is empty or unsafe")
	}
	for i, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return nil, fmt.Errorf("local command argv[%d] is empty or contains control bytes", i)
		}
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, fmt.Errorf("find local command %q: %w", argv[0], err)
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...) // #nosec G204 -- locally trusted argv preset, no shell.
	if err := configureDirectory(cmd, host.WorkingDirectory); err != nil {
		return nil, err
	}
	return cmd, nil
}

func Probe(ctx context.Context, host inventory.Host) probe.Result {
	started := time.Now()
	capabilities := platform.Current()
	if !capabilities.LocalProbeAvailable {
		return probe.Failure(fmt.Errorf("native local metrics probe is not implemented on %s", capabilities.Platform()), "", time.Now(), time.Since(started))
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		return probe.Failure(err, "", time.Now(), time.Since(started))
	}
	cmd := exec.CommandContext(ctx, sh, "-s") // #nosec G204 -- fixed POSIX probe invocation.
	cmd.Stdin = strings.NewReader(probe.LinuxScript)
	if err := configureDirectory(cmd, host.WorkingDirectory); err != nil {
		return probe.Failure(err, "", time.Now(), time.Since(started))
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return probe.Failure(err, stderr.String(), time.Now(), time.Since(started))
	}
	result, err := probe.Parse(stdout.String())
	if err != nil {
		return probe.Failure(err, err.Error(), time.Now(), time.Since(started))
	}
	result.CheckedAt = time.Now()
	result.Latency = time.Since(started)
	return result
}

func configureDirectory(cmd *exec.Cmd, configured string) error {
	if strings.TrimSpace(configured) == "" {
		return nil
	}
	directory, err := config.ExpandPath(configured)
	if err != nil {
		return fmt.Errorf("expand local working directory: %w", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("inspect local working directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local working directory is not a directory: %s", directory)
	}
	cmd.Dir = directory
	return nil
}
