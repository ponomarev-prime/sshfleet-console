package openssh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

type Client struct {
	Binary           string
	ConnectTimeout   time.Duration
	AskPassBinary    string
	AskPassSelf      bool
	LocalClient      LocalClient
	WorkspaceBundle  string
	WorkspaceCleanup bool
}

// LocalClient describes the OpenSSH executable running on the workstation.
// Probe.Result SSH fields, by contrast, are collected on the remote host.
type LocalClient struct {
	Path      string
	Component probe.Component
	Error     string
}

func DetectLocalClient(ctx context.Context, binary string) LocalClient {
	if strings.TrimSpace(binary) == "" {
		binary = "ssh"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return LocalClient{Error: err.Error()}
	}
	cmd := exec.CommandContext(ctx, path, "-V")
	output, err := cmd.CombinedOutput()
	version := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	if err != nil {
		return LocalClient{Path: path, Error: err.Error()}
	}
	return LocalClient{Path: path, Component: probe.Component{State: "installed", Version: version}}
}

type Effective struct {
	Hostname            string
	User                string
	Port                string
	ProxyJump           string
	HostKeyAlias        string
	IdentityFile        []string
	UserKnownHostsFiles []string
	Error               string
}

func (c Client) InteractiveCommand(host inventory.Host) (*exec.Cmd, error) {
	return c.interactiveCommand(host, false)
}

func (c Client) InteractiveCommandWithHostKeyPrompt(host inventory.Host) (*exec.Cmd, error) {
	return c.interactiveCommand(host, true)
}

func (c Client) GitAccessCommand(host inventory.Host) (*exec.Cmd, error) {
	args, err := c.baseArgs(host)
	if err != nil {
		return nil, err
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args, "-T", "--", host.Alias)
	return c.command(host, args...), nil
}

// Command builds a non-interactive, forwarding-disabled SSH invocation for a
// locally trusted argv preset. Each argument is POSIX-quoted into one remote
// command string because OpenSSH executes remote commands through the account's
// login shell.
func (c Client) Command(ctx context.Context, host inventory.Host, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("remote command argv is empty")
	}
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return nil, fmt.Errorf("remote command argv[%d] is empty or contains control bytes", i)
		}
		quoted[i] = posixQuote(arg)
	}
	args, err := c.baseArgs(host)
	if err != nil {
		return nil, err
	}
	batchMode := "yes"
	if host.CredentialName != "" {
		batchMode = "no"
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args,
		"-T",
		"-o", "BatchMode="+batchMode,
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none",
		"-o", "RequestTTY=no",
		"--", host.Alias, strings.Join(quoted, " "),
	)
	return c.commandContext(ctx, host, args...), nil
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func (c Client) interactiveCommand(host inventory.Host, forceHostKeyPrompt bool) (*exec.Cmd, error) {
	args, err := c.baseArgs(host)
	if err != nil {
		return nil, err
	}
	if forceHostKeyPrompt {
		args = append(args,
			"-o", "StrictHostKeyChecking=ask",
			"-o", "UpdateHostKeys=no",
		)
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args, "--", host.Alias)
	if host.Shell != "" {
		command := make([]string, 0, len(host.ShellArgs)+2)
		command = append(command, "exec", posixQuote(host.Shell))
		for _, arg := range host.ShellArgs {
			command = append(command, posixQuote(arg))
		}
		args = append(args, strings.Join(command, " "))
	}
	if forceHostKeyPrompt {
		// The confirmation belongs on the controlling TTY. Do not attach the
		// password-only AskPass helper to a host-key repair session.
		return exec.Command(c.binary(), args...), nil // #nosec G204 -- no shell; executable and arguments are explicit.
	}
	return c.command(host, args...), nil
}

func (c Client) Resolve(ctx context.Context, host inventory.Host) Effective {
	args, err := c.baseArgs(host)
	if err != nil {
		return Effective{Error: err.Error()}
	}
	args = append([]string{"-G"}, args...)
	args = append(args, "--", host.Alias)
	cmd := c.commandContext(ctx, host, args...)
	output, err := cmd.Output()
	if err != nil {
		return Effective{Error: err.Error()}
	}

	return parseEffective(string(output))
}

func parseEffective(output string) Effective {
	var effective Effective
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "hostname":
			effective.Hostname = value
		case "user":
			effective.User = value
		case "port":
			effective.Port = value
		case "proxyjump":
			if value != "none" {
				effective.ProxyJump = value
			}
		case "hostkeyalias":
			if value != "none" {
				effective.HostKeyAlias = value
			}
		case "identityfile":
			effective.IdentityFile = append(effective.IdentityFile, value)
		case "userknownhostsfile":
			if value != "none" {
				effective.UserKnownHostsFiles = append(effective.UserKnownHostsFiles, strings.Fields(value)...)
			}
		}
	}
	return effective
}

func (c Client) Probe(ctx context.Context, host inventory.Host) probe.Result {
	started := time.Now()
	effective := c.Resolve(ctx, host)
	if effective.Error == "" && strings.EqualFold(effective.User, "git") {
		return c.probeGitAccess(ctx, host, started)
	}
	args, err := c.baseArgs(host)
	if err != nil {
		return probe.Failure(err, "", started, 0)
	}
	timeoutSeconds := int(c.ConnectTimeout.Round(time.Second) / time.Second)
	if timeoutSeconds < 1 {
		timeoutSeconds = 6
	}
	batchMode := "yes"
	if host.CredentialName != "" {
		batchMode = "no"
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args,
		"-T",
		"-o", "BatchMode="+batchMode,
		"-o", "ConnectTimeout="+strconv.Itoa(timeoutSeconds),
		"-o", "ConnectionAttempts=1",
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none",
		"-o", "RequestTTY=no",
		"--", host.Alias, "sh", "-s",
	)
	cmd := c.commandContext(ctx, host, args...)
	cmd.Stdin = strings.NewReader(probe.LinuxScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	latency := time.Since(started)
	checkedAt := time.Now()
	if err != nil {
		return probe.Failure(err, stderr.String(), checkedAt, latency)
	}
	result, err := probe.Parse(stdout.String())
	if err != nil {
		return probe.Failure(err, err.Error(), checkedAt, latency)
	}
	result.CheckedAt = checkedAt
	result.Latency = latency
	return result
}

func (c Client) probeGitAccess(ctx context.Context, host inventory.Host, started time.Time) probe.Result {
	args, err := c.baseArgs(host)
	if err != nil {
		return probe.Failure(err, "", started, 0)
	}
	timeoutSeconds := int(c.ConnectTimeout.Round(time.Second) / time.Second)
	if timeoutSeconds < 1 {
		timeoutSeconds = 6
	}
	batchMode := "yes"
	if host.CredentialName != "" {
		batchMode = "no"
	}
	args = append(args, c.credentialArgs(host)...)
	args = append(args,
		"-T",
		"-v",
		"-o", "BatchMode="+batchMode,
		"-o", "ConnectTimeout="+strconv.Itoa(timeoutSeconds),
		"-o", "ConnectionAttempts=1",
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none",
		"-o", "RequestTTY=no",
		"--", host.Alias,
	)
	cmd := c.commandContext(ctx, host, args...)
	output, runErr := cmd.CombinedOutput()
	return gitAccessResult(runErr, string(output), time.Now(), time.Since(started))
}

var authenticatedPattern = regexp.MustCompile(`(?m)^debug1: Authenticated to .* using "([^"]+)"\.?\r?$`)

func gitAccessResult(err error, output string, checkedAt time.Time, latency time.Duration) probe.Result {
	clean := cleanSSHOutput(output)
	failure := probe.Failure(err, clean, checkedAt, latency)
	if failure.Status == probe.StatusAuth || failure.Status == probe.StatusHostKey || failure.Status == probe.StatusUnreachable {
		return failure
	}

	match := authenticatedPattern.FindStringSubmatch(output)
	authenticated := err == nil || len(match) == 2 || looksLikeGitGreeting(clean)
	if !authenticated {
		return failure
	}
	method := "accepted"
	if len(match) == 2 {
		method = match[1]
	}
	message := firstUsefulGitLine(clean)
	if message == "" {
		message = "SSH authentication accepted; interactive shell is not expected"
	}
	return probe.Result{
		Status:        probe.StatusGit,
		CheckedAt:     checkedAt,
		Latency:       latency,
		GitAuthMethod: method,
		GitMessage:    message,
	}
}

func cleanSSHOutput(output string) string {
	lines := make([]string, 0, 8)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || strings.HasPrefix(trimmed, "debug") ||
			strings.HasPrefix(trimmed, "** WARNING:") ||
			strings.HasPrefix(trimmed, "** This session") ||
			strings.HasPrefix(trimmed, "** The server") ||
			strings.Contains(lower, "pseudo-terminal will not be allocated") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}

func looksLikeGitGreeting(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "successfully authenticated") ||
		strings.Contains(lower, "welcome to gitlab") ||
		strings.Contains(lower, "does not provide shell access") ||
		strings.Contains(lower, "shell access is disabled")
}

func firstUsefulGitLine(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func (c Client) baseArgs(host inventory.Host) ([]string, error) {
	if err := host.Validate(); err != nil {
		return nil, err
	}
	if host.CredentialName != "" && strings.TrimSpace(c.AskPassBinary) == "" {
		return nil, fmt.Errorf("host %q requires SSH Fleet AskPass", host.Alias)
	}
	var args []string
	if host.ConfigPath != "" {
		args = append(args, "-F", host.ConfigPath)
	}
	if host.Hostname != "" {
		args = append(args, "-o", "HostName="+host.Hostname)
	}
	if host.User != "" {
		args = append(args, "-l", host.User)
	}
	if host.Port != 0 {
		args = append(args, "-p", strconv.Itoa(host.Port))
	}
	if host.ProxyJump != "" {
		args = append(args, "-J", host.ProxyJump)
	}
	if host.IdentityFile != "" {
		args = append(args, "-i", host.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	return args, nil
}

func (c Client) binary() string {
	if strings.TrimSpace(c.Binary) == "" {
		return "ssh"
	}
	return c.Binary
}

func (c Client) credentialArgs(host inventory.Host) []string {
	if host.CredentialName == "" {
		return nil
	}
	switch host.CredentialType {
	case "", config.CredentialPassword:
		return []string{
			"-o", "PasswordAuthentication=yes",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "PubkeyAuthentication=no",
			"-o", "PreferredAuthentications=password",
			"-o", "NumberOfPasswordPrompts=1",
			"-o", "StrictHostKeyChecking=yes",
		}
	case config.CredentialKeyPassphrase:
		return []string{
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "PubkeyAuthentication=yes",
			"-o", "PreferredAuthentications=publickey",
			"-o", "IdentitiesOnly=yes",
			"-o", "StrictHostKeyChecking=yes",
		}
	default:
		return nil
	}
}

func (c Client) command(host inventory.Host, args ...string) *exec.Cmd {
	cmd := exec.Command(c.binary(), args...) // #nosec G204 -- no shell; executable and arguments are explicit.
	c.configureCredential(cmd, host)
	return cmd
}

func (c Client) commandContext(ctx context.Context, host inventory.Host, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.binary(), args...) // #nosec G204 -- no shell; executable and arguments are explicit.
	c.configureCredential(cmd, host)
	return cmd
}

func (c Client) configureCredential(cmd *exec.Cmd, host inventory.Host) {
	if host.CredentialName == "" {
		return
	}
	cmd.Env = append(os.Environ(),
		"SSH_ASKPASS="+c.AskPassBinary,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=sshfleet",
		"SSHF_CREDENTIAL_PROVIDER="+host.CredentialProvider,
		"SSHF_CREDENTIAL_KEY="+host.CredentialKey,
	)
	if c.AskPassSelf {
		cmd.Env = append(cmd.Env, "SSHF_INTERNAL_ASKPASS=1")
	}
}

func (e Effective) Summary() string {
	if e.Error != "" {
		return e.Error
	}
	target := e.Hostname
	if e.User != "" {
		target = e.User + "@" + target
	}
	if e.Port != "" && e.Port != "22" {
		target += ":" + e.Port
	}
	return fmt.Sprintf("%s", target)
}

func (e Effective) KnownHostsLookup() string {
	// OpenSSH treats HostKeyAlias as the complete lookup token and does not
	// decorate it with a non-default port. Only the real hostname uses the
	// bracketed [host]:port known_hosts form.
	if e.HostKeyAlias != "" {
		return e.HostKeyAlias
	}
	host := e.Hostname
	if host == "" {
		return ""
	}
	if e.Port != "" && e.Port != "22" {
		return "[" + host + "]:" + e.Port
	}
	return host
}
