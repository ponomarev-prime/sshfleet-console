//go:build linux || darwin

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/knownhosts"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
	"github.com/ponomarev-prime/sshfleet-console/internal/workspace"
)

// TestDockerAliasHostKeyRepair exercises the real OpenSSH client and a real
// sshd without touching the user's SSH configuration. It is opt-in because it
// requires access to a Docker daemon and a prebuilt fixture image; run it with
// `make test-docker`.
func TestDockerAliasHostKeyRepair(t *testing.T) {
	if os.Getenv("SSHF_DOCKER_E2E") != "1" {
		t.Skip("set SSHF_DOCKER_E2E=1 or run make test-docker")
	}
	requireCommand(t, "docker")
	requireCommand(t, "ssh")
	requireCommand(t, "ssh-keygen")

	image := os.Getenv("SSHF_DOCKER_IMAGE")
	if image == "" {
		image = "sshfleet-test-sshd:local"
	}
	if output, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Fatalf("inspect Docker fixture image %q: %v: %s; run make test-docker", image, err, output)
	}

	directory := t.TempDir()
	clientKey := filepath.Join(directory, "client_key")
	generateSSHKey(t, clientKey)
	clientPublicKey, err := os.ReadFile(clientKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	encryptedClientKey := filepath.Join(directory, "client_key_encrypted")
	generateEncryptedSSHKey(t, encryptedClientKey, "fixture-passphrase")
	encryptedPublicKey, err := os.ReadFile(encryptedClientKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	authorizedKeys := append(append([]byte(nil), clientPublicKey...), encryptedPublicKey...)

	fixtureA := dockerSSHFixture(t, filepath.Join(directory, "generation-a"), authorizedKeys)
	fixtureB := dockerSSHFixture(t, filepath.Join(directory, "generation-b"), authorizedKeys)
	port := freeTCPPort(t)
	alias := "sshfleet-docker-test"
	lookup := alias
	knownHosts := filepath.Join(directory, "known_hosts")
	writeKnownHost(t, knownHosts, lookup, fixtureA+".pub")
	originalKnownHosts, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatal(err)
	}

	sshConfig := filepath.Join(directory, "ssh_config")
	writeFile(t, sshConfig, fmt.Sprintf(`Host %s
    HostName 127.0.0.1
    Port %d
    User sshfleet
    IdentityFile %s
    IdentitiesOnly yes
    HostKeyAlias %s
    UserKnownHostsFile %s
    GlobalKnownHostsFile /dev/null
    StrictHostKeyChecking yes
    UpdateHostKeys no
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    RequestTTY no
    RemoteCommand printf 'docker-ssh-ready\n'
`, alias, port, clientKey, alias, knownHosts), 0o600)

	host := inventory.Host{
		ID:         "docker:" + alias,
		Alias:      alias,
		SourceName: "docker",
		ConfigPath: sshConfig,
		Probe:      true,
	}
	client := openssh.Client{Binary: "ssh", ConnectTimeout: 2 * time.Second}

	nameA := fmt.Sprintf("sshfleet-e2e-%d-a", time.Now().UnixNano())
	stopA := startDockerSSHD(t, image, nameA, filepath.Dir(fixtureA), port)
	assertOnlineProbe(t, client, host)
	assertEncryptedKeyAskPassProbe(t, directory, sshConfig, encryptedClientKey, host, client)
	if bundle := os.Getenv("SSHF_WORKSPACE_BUNDLE_TEST"); bundle != "" {
		assertTemporaryWorkspaceBundle(t, bundle, sshConfig, client, host)
	}
	stopA()

	nameB := fmt.Sprintf("sshfleet-e2e-%d-b", time.Now().UnixNano())
	stopB := startDockerSSHD(t, image, nameB, filepath.Dir(fixtureB), port)
	defer stopB()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	changed := client.Probe(ctx, host)
	cancel()
	if changed.Status != probe.StatusHostKey {
		t.Fatalf("probe after host-key rotation = %#v, want %s", changed, probe.StatusHostKey)
	}
	if changed.PresentedHostKey == "" || changed.KnownHostsFile != knownHosts {
		t.Fatalf("incomplete host-key error details: %#v", changed)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
	effective := client.Resolve(ctx, host)
	cancel()
	if effective.Error != "" || effective.KnownHostsLookup() != lookup {
		t.Fatalf("effective SSH config = %#v, lookup want %q", effective, lookup)
	}
	plan, err := knownhosts.Inspect(
		effective.KnownHostsLookup(),
		effective.UserKnownHostsFiles,
		changed.KnownHostsFile,
		changed.KnownHostsLine,
		changed.PresentedHostKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := knownhosts.Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(applied.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(originalKnownHosts) {
		t.Fatal("host-key repair backup does not match the original known_hosts")
	}
	assertKnownHostAbsent(t, knownHosts, lookup)

	command, err := client.InteractiveCommandWithHostKeyPrompt(host)
	if err != nil {
		t.Fatal(err)
	}
	command.Env = append(os.Environ(), "LC_ALL=C", "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	harness := newPTYHarness(t, command, terminal, 80, 24)
	defer harness.close()
	harness.waitFor("Are you sure you want to continue connecting", 6*time.Second)
	harness.send("yes\r")
	harness.waitFor("docker-ssh-ready", 6*time.Second)
	if err := harness.wait(6 * time.Second); err != nil {
		t.Fatalf("repaired SSH session failed: %v\n%s", err, harness.tail(4000))
	}

	assertKnownHostPresent(t, knownHosts, lookup)
	assertOnlineProbe(t, client, host)
}

func assertTemporaryWorkspaceBundle(t *testing.T, bundle, sshConfig string, client openssh.Client, host inventory.Host) {
	t.Helper()
	client.WorkspaceBundle = bundle
	client.WorkspaceCleanup = true
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	remotePath, err := client.PrepareWorkspace(ctx, host)
	cancel()
	if err != nil {
		t.Fatalf("prepare portable workspace: %v", err)
	}
	command, err := client.WorkspaceCommand(host, remotePath, workspace.ToolShell)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdin = strings.NewReader("command -v lf\ncommand -v nvim\ncommand -v dtop\ncommand -v bat\nlf -version\nnvim --version | sed -n '1p'\ndtop --version\nbat --version\nexit\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("portable interactive workspace: %v\n%s", err, output)
	}
	for _, want := range []string{remotePath + "/bin/lf", remotePath + "/bin/nvim", remotePath + "/bin/dtop", remotePath + "/bin/bat", "r42", "NVIM v0.12.5", "dtop 0.9.0", "bat 0.26.1"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("workspace output missing %q:\n%s", want, output)
		}
	}
	check := exec.Command("ssh", "-F", sshConfig, "-o", "RemoteCommand=none", "--", host.Alias, "test ! -e "+remotePath)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("workspace directory was not removed: %v: %s", err, output)
	}
}

func dockerSSHFixture(t *testing.T, directory string, authorizedKey []byte) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	hostKey := filepath.Join(directory, "host_key")
	generateSSHKey(t, hostKey)
	if err := os.WriteFile(filepath.Join(directory, "authorized_keys"), authorizedKey, 0o600); err != nil {
		t.Fatal(err)
	}
	return hostKey
}

func generateSSHKey(t *testing.T, path string) {
	t.Helper()
	command := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate SSH key %s: %v: %s", path, err, output)
	}
}

func generateEncryptedSSHKey(t *testing.T, path, passphrase string) {
	t.Helper()
	command := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-f", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate encrypted SSH key %s: %v: %s", path, err, output)
	}
}

func assertEncryptedKeyAskPassProbe(t *testing.T, directory, baseConfig, keyPath string, baseHost inventory.Host, baseClient openssh.Client) {
	t.Helper()
	askPassLog := filepath.Join(directory, "askpass.log")
	askPass := filepath.Join(directory, "fixture-askpass")
	writeFile(t, askPass, fmt.Sprintf("#!/bin/sh\nprintf 'invoked\\n' >>%q\nprintf '%%s\\n' 'fixture-passphrase'\n", askPassLog), 0o700)

	configData, err := os.ReadFile(baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	withoutDefaultIdentity := strings.ReplaceAll(string(configData), "    IdentityFile "+strings.TrimSuffix(keyPath, "_encrypted")+"\n", "")
	encryptedConfig := filepath.Join(directory, "ssh_config_encrypted")
	writeFile(t, encryptedConfig, withoutDefaultIdentity, 0o600)

	host := baseHost
	host.ID += ":encrypted"
	host.ConfigPath = encryptedConfig
	host.IdentityFile = keyPath
	host.CredentialName = "fixture-key"
	host.CredentialType = config.CredentialKeyPassphrase
	host.CredentialProvider = "secret-service"
	host.CredentialKey = "fixture/reference"
	client := baseClient
	client.AskPassBinary = askPass
	assertOnlineProbe(t, client, host)
	if info, err := os.Stat(askPassLog); err != nil || info.Size() == 0 {
		t.Fatalf("encrypted key probe did not invoke AskPass: %v", err)
	}
}

func writeKnownHost(t *testing.T, destination, lookup, publicKeyPath string) {
	t.Helper()
	publicKey, err := os.ReadFile(publicKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(publicKey))
	if len(fields) < 2 {
		t.Fatalf("invalid SSH public key: %q", publicKey)
	}
	writeFile(t, destination, lookup+" "+fields[0]+" "+fields[1]+"\n", 0o600)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func startDockerSSHD(t *testing.T, image, name, fixtureDirectory string, port int) func() {
	t.Helper()
	args := []string{
		"run", "--rm", "--detach",
		"--name", name,
		"--publish", "127.0.0.1:" + strconv.Itoa(port) + ":2222",
		"--mount", "type=bind,src=" + fixtureDirectory + ",dst=/fixture,readonly",
		image,
	}
	if output, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("start Docker SSH fixture: %v: %s", err, output)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = exec.Command("docker", "rm", "--force", name).Run()
	}
	t.Cleanup(stop)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err == nil {
			_ = connection.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			prefix := make([]byte, 4)
			_, readErr := io.ReadFull(connection, prefix)
			_ = connection.Close()
			// Docker's userland proxy can accept a TCP connection before sshd is
			// listening in the container. Only the SSH protocol banner proves the
			// fixture is ready; a bare connect caused intermittent first-probe
			// resets in the full regression suite.
			if readErr == nil && string(prefix) == "SSH-" {
				return stop
			}
		}
		if !dockerContainerRunning(name) {
			logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
			stop()
			t.Fatalf("Docker SSH fixture exited before readiness: %s", logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
	stop()
	t.Fatalf("Docker SSH fixture was not ready: %s", logs)
	return nil
}

func dockerContainerRunning(name string) bool {
	output, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", name).Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func assertOnlineProbe(t *testing.T, client openssh.Client, host inventory.Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := client.Probe(ctx, host)
	if result.Status != probe.StatusOnline || result.CPUCount < 1 || result.MemoryTotal == 0 {
		t.Fatalf("Docker SSH probe = %#v, want online Linux metrics", result)
	}
	if result.SSHServer.Version == "" || result.SSHServer.State != "running" {
		t.Fatalf("Docker SSH metadata = client:%#v server:%#v", result.SSHClient, result.SSHServer)
	}
}

func assertKnownHostAbsent(t *testing.T, file, lookup string) {
	t.Helper()
	command := exec.Command("ssh-keygen", "-F", lookup, "-f", file)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("known_hosts still contains %q: %s", lookup, output)
	}
}

func assertKnownHostPresent(t *testing.T, file, lookup string) {
	t.Helper()
	command := exec.Command("ssh-keygen", "-F", lookup, "-f", file)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("known_hosts does not contain %q: %v: %s", lookup, err, output)
	}
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command %q is unavailable: %v", name, err)
	}
}
