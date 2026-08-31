package openssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/workspace"
)

const workspaceInstallScript = `set -eu
umask 077
platform=$(uname -s)-$(uname -m)
if [ "$platform" != "Linux-x86_64" ]; then
    printf "unsupported workspace platform: %s (need Linux-x86_64)\n" "$platform" >&2
    exit 65
fi
glibc=$(getconf GNU_LIBC_VERSION 2>/dev/null || true)
case "$glibc" in
    glibc\ *) ;;
    *)
        printf "unsupported workspace libc: %s (need glibc 2.34+)\n" "${glibc:-unknown}" >&2
        exit 65
        ;;
esac
glibc_version=${glibc#glibc }
glibc_major=${glibc_version%%.*}
glibc_minor=${glibc_version#*.}
glibc_minor=${glibc_minor%%.*}
case "$glibc_major:$glibc_minor" in
    *[!0-9:]*|:|*:)
        printf "unsupported workspace libc version: %s\n" "$glibc" >&2
        exit 65
        ;;
esac
if [ "$glibc_major" -lt 2 ] || { [ "$glibc_major" -eq 2 ] && [ "$glibc_minor" -lt 34 ]; }; then
    printf "unsupported workspace libc: %s (need glibc 2.34+)\n" "$glibc" >&2
    exit 65
fi
directory=$(mktemp -d /tmp/sshfleet-workspace.XXXXXXXX)
trap 'rm -rf -- "$directory"' EXIT HUP INT TERM
tar -xzf - -C "$directory"
test -x "$directory/run"
printf "SSHF_WORKSPACE=%s\n" "$directory"
trap - EXIT HUP INT TERM`

var remoteWorkspacePath = regexp.MustCompile(`^/tmp/sshfleet-workspace\.[A-Za-z0-9]+$`)

func (c Client) PrepareWorkspace(ctx context.Context, host inventory.Host) (string, error) {
	if strings.TrimSpace(c.WorkspaceBundle) == "" {
		return "", errors.New("portable workspace bundle is not configured; run make remote-bundle")
	}
	bundle, err := workspace.OpenValidated(c.WorkspaceBundle)
	if err != nil {
		return "", err
	}
	defer bundle.Close()
	args, err := c.baseArgs(host)
	if err != nil {
		return "", err
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args,
		"-T",
		"-o", "ClearAllForwardings=yes",
		"-o", "ForwardAgent=no",
		"-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none",
		"-o", "RequestTTY=no",
		"--", host.Alias,
		"sh -c "+quoteRemote(workspaceInstallScript),
	)
	cmd := c.commandContext(ctx, host, args...)
	cmd.Stdin = bundle
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("upload workspace: %s", message)
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "SSHF_WORKSPACE=")
		if ok && remoteWorkspacePath.MatchString(value) {
			return value, nil
		}
	}
	return "", errors.New("remote workspace did not return a safe temporary path")
}

func (c Client) WorkspaceCommand(host inventory.Host, remotePath string, tool workspace.Tool) (*exec.Cmd, error) {
	if !remoteWorkspacePath.MatchString(remotePath) {
		return nil, errors.New("unsafe remote workspace path")
	}
	if !tool.Valid() {
		return nil, fmt.Errorf("unsupported workspace tool %q", tool)
	}
	args, err := c.baseArgs(host)
	if err != nil {
		return nil, err
	}
	retention := "keep"
	if c.WorkspaceCleanup {
		retention = "cleanup"
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args,
		"-tt",
		"-o", "ClearAllForwardings=yes",
		"-o", "ForwardAgent=no",
		"-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none",
		"--", host.Alias,
		"sh "+quoteRemote(remotePath+"/run")+" "+quoteRemote(string(tool))+" "+retention,
	)
	return c.command(host, args...), nil
}

func quoteRemote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
