package openssh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func TestBaseArgsAreStructured(t *testing.T) {
	c := Client{Binary: "ssh"}
	host := inventory.Host{
		Alias:      "prod-api",
		ConfigPath: "/tmp/ssh config",
		Hostname:   "10.0.0.5",
		User:       "deploy",
		Port:       2222,
		ProxyJump:  "bastion",
	}
	got, err := c.baseArgs(host)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-F", "/tmp/ssh config", "-o", "HostName=10.0.0.5", "-l", "deploy", "-p", "2222", "-J", "bastion"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestDetectLocalClientSeparatesWorkstationVersion(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ssh-local")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' 'OpenSSH_10.1 local-fixture' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	local := DetectLocalClient(context.Background(), binary)
	if local.Error != "" || local.Path != binary || local.Component.Version != "OpenSSH_10.1 local-fixture" || local.Component.State != "installed" {
		t.Fatalf("local client = %#v", local)
	}
}

func TestRejectsOptionLikeAlias(t *testing.T) {
	_, err := (Client{}).baseArgs(inventory.Host{Alias: "-oProxyCommand=bad"})
	if err == nil {
		t.Fatal("expected unsafe alias to be rejected")
	}
}

func TestParseEffectiveKnownHostsSettings(t *testing.T) {
	effective := parseEffective(`hostname 192.0.2.10
user root
port 2222
hostkeyalias inventory-node
userknownhostsfile /home/tester/.ssh/known_hosts /home/tester/.ssh/known_hosts2
`)
	if got, want := effective.KnownHostsLookup(), "inventory-node"; got != want {
		t.Fatalf("KnownHostsLookup() = %q, want %q", got, want)
	}
	wantFiles := []string{"/home/tester/.ssh/known_hosts", "/home/tester/.ssh/known_hosts2"}
	if !reflect.DeepEqual(effective.UserKnownHostsFiles, wantFiles) {
		t.Fatalf("known hosts files = %#v, want %#v", effective.UserKnownHostsFiles, wantFiles)
	}
}

func TestKnownHostsLookupUsesBracketedHostnameForNonDefaultPort(t *testing.T) {
	effective := Effective{Hostname: "192.0.2.10", Port: "2222"}
	if got, want := effective.KnownHostsLookup(), "[192.0.2.10]:2222"; got != want {
		t.Fatalf("KnownHostsLookup() = %q, want %q", got, want)
	}
}

func TestRepairSessionForcesInteractiveHostKeyPrompt(t *testing.T) {
	command, err := (Client{Binary: "ssh"}).InteractiveCommandWithHostKeyPrompt(inventory.Host{Alias: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-o", "StrictHostKeyChecking=ask", "-o", "UpdateHostKeys=no", "--", "demo"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("args = %#v, want %#v", command.Args, want)
	}
}

func TestGitAccessCommandDoesNotRequestShell(t *testing.T) {
	command, err := (Client{Binary: "ssh"}).GitAccessCommand(inventory.Host{Alias: "github.com"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-T", "--", "github.com"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("args = %#v, want %#v", command.Args, want)
	}
}

func TestGitAccessAcceptsAuthenticatedNoShellExit(t *testing.T) {
	output := `debug1: Authenticated to github.com ([192.0.2.1]:22) using "publickey".
Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.
`
	result := gitAccessResult(errors.New("exit status 1"), output, time.Time{}, time.Second)
	if result.Status != probe.StatusGit || result.GitAuthMethod != "publickey" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.GitMessage, "successfully authenticated") {
		t.Fatalf("message = %q", result.GitMessage)
	}
}

func TestGitAccessPreservesAuthenticationFailure(t *testing.T) {
	output := `debug1: Authentications that can continue: publickey
git@example: Permission denied (publickey).
`
	result := gitAccessResult(errors.New("exit status 255"), output, time.Time{}, time.Second)
	if result.Status != probe.StatusAuth {
		t.Fatalf("status = %s, want %s", result.Status, probe.StatusAuth)
	}
}

func TestProbeAutomaticallyUsesGitHandshake(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-ssh")
	script := `#!/bin/sh
for arg do
  if [ "$arg" = "-G" ]; then
    printf 'hostname git.example\nuser git\nport 22\n'
    exit 0
  fi
done
printf '%s\n' 'debug1: Authenticated to git.example ([192.0.2.1]:22) using "publickey".' >&2
printf '%s\n' 'Welcome to GitLab, @tester!' >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	client := Client{Binary: binary, ConnectTimeout: time.Second}
	result := client.Probe(context.Background(), inventory.Host{Alias: "git.example"})
	if result.Status != probe.StatusGit || result.GitAuthMethod != "publickey" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCredentialUsesAskPassWithoutSecretInArguments(t *testing.T) {
	host := inventory.Host{Alias: "password-node-1", CredentialName: "lab-password", CredentialProvider: "secret-service", CredentialKey: "sshfleet/demo/password-nodes"}
	command, err := (Client{Binary: "ssh", AskPassBinary: "/opt/sshf-askpass"}).InteractiveCommand(host)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, wanted := range []string{"PasswordAuthentication=yes", "PubkeyAuthentication=no", "StrictHostKeyChecking=yes"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("args missing %q: %#v", wanted, command.Args)
		}
	}
	env := strings.Join(command.Env, "\n")
	for _, wanted := range []string{"SSH_ASKPASS=/opt/sshf-askpass", "SSH_ASKPASS_REQUIRE=force", "SSHF_CREDENTIAL_PROVIDER=secret-service", "SSHF_CREDENTIAL_KEY=sshfleet/demo/password-nodes"} {
		if !strings.Contains(env, wanted) {
			t.Fatalf("env missing %q", wanted)
		}
	}
}

func TestCredentialCanReuseSSHFAsAskPassProcess(t *testing.T) {
	host := inventory.Host{Alias: "password-node-1", CredentialName: "lab-password", CredentialProvider: "secret-service", CredentialKey: "sshfleet/demo/password-nodes"}
	command, err := (Client{Binary: "ssh", AskPassBinary: "/opt/sshf", AskPassSelf: true}).InteractiveCommand(host)
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Join(command.Env, "\n")
	for _, wanted := range []string{"SSH_ASKPASS=/opt/sshf", "SSHF_INTERNAL_ASKPASS=1"} {
		if !strings.Contains(env, wanted) {
			t.Fatalf("env missing %q: %#v", wanted, command.Env)
		}
	}
	if strings.Contains(strings.Join(command.Args, " "), host.CredentialKey) {
		t.Fatalf("credential key leaked into argv: %#v", command.Args)
	}
}

func TestCredentialRequiresAskPassBinary(t *testing.T) {
	host := inventory.Host{Alias: "password-node-1", CredentialName: "lab-password", CredentialProvider: "secret-service", CredentialKey: "key"}
	if _, err := (Client{Binary: "ssh"}).InteractiveCommand(host); err == nil {
		t.Fatal("expected missing helper error")
	}
}

func TestEncryptedKeyPassphraseUsesPublicKeyAskPass(t *testing.T) {
	host := inventory.Host{
		Alias: "secure-202", ConfigPath: os.DevNull,
		IdentityFile:   "/home/test/.ssh/id_secure_202",
		CredentialName: "secure-202-key", CredentialType: config.CredentialKeyPassphrase,
		CredentialProvider: "secret-service", CredentialKey: "sshfleet/keys/secure-202",
	}
	command, err := (Client{Binary: "ssh", AskPassBinary: "/opt/sshf-askpass"}).InteractiveCommand(host)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, wanted := range []string{"-i /home/test/.ssh/id_secure_202", "PasswordAuthentication=no", "PubkeyAuthentication=yes", "PreferredAuthentications=publickey", "IdentitiesOnly=yes"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("args missing %q: %#v", wanted, command.Args)
		}
	}
	if strings.Contains(joined, "sshfleet/keys/secure-202") {
		t.Fatalf("credential key leaked into argv: %#v", command.Args)
	}
	if env := strings.Join(command.Env, "\n"); !strings.Contains(env, "SSHF_CREDENTIAL_KEY=sshfleet/keys/secure-202") {
		t.Fatalf("AskPass environment missing credential reference: %#v", command.Env)
	}
}

func TestHostKeyRepairDoesNotAttachPasswordAskPass(t *testing.T) {
	host := inventory.Host{Alias: "password-node-1", CredentialName: "lab-password", CredentialProvider: "secret-service", CredentialKey: "key"}
	command, err := (Client{Binary: "ssh", AskPassBinary: "/opt/sshf-askpass"}).InteractiveCommandWithHostKeyPrompt(host)
	if err != nil {
		t.Fatal(err)
	}
	if len(command.Env) != 0 {
		t.Fatalf("repair must use controlling TTY, env = %#v", command.Env)
	}
}

func TestCommandQuotesArgumentsAndDisablesForwarding(t *testing.T) {
	command, err := (Client{Binary: "ssh"}).Command(context.Background(), inventory.Host{Alias: "alpha"}, []string{"printf", "%s", "it's safe; touch /tmp/no"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(command.Args, " ")
	for _, wanted := range []string{"ClearAllForwardings=yes", "PermitLocalCommand=no", "RequestTTY=no", `'printf' '%s' 'it'"'"'s safe; touch /tmp/no'`} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("command args missing %q: %#v", wanted, command.Args)
		}
	}
	if _, err := (Client{Binary: "ssh"}).Command(context.Background(), inventory.Host{Alias: "alpha"}, []string{"bad\ncommand"}); err == nil {
		t.Fatal("control-bearing command was accepted")
	}
}
