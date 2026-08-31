package probe

import (
	"context"
	"math"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseAndCPUDelta(t *testing.T) {
	output := `cpu_total=1200
cpu_idle=800
cpu_count=8
mem_total_kb=16000
mem_available_kb=4000
swap_total_kb=8000
swap_available_kb=6000
root_total_kb=32000
root_available_kb=8000
load=0.25 0.50 0.75
uptime_seconds=123.5
process=42|S|nginx|12.3
os_name=Debian GNU/Linux 13 (trixie)
kernel=Linux 6.12.0
architecture=x86_64
init=systemd
virtualization=kvm
systemd_version=257
systemd_state=running
systemd_failed_units=1
ssh_client_version=OpenSSH_9.9p2
ssh_client_state=installed
sshd_version=OpenSSH_9.9p2
sshd_state=active
ssh_service_unit=ssh.service
ssh_socket_state=ssh.socket inactive
ssh_agent_state=running
ssh_tools=scp, sftp, ssh-keygen, ssh-add
openssl_version=OpenSSL 3.5.0
openssl_state=installed
docker_version=28.1.1
docker_state=active
containerd_version=v2.0.5
containerd_state=active
podman_state=not-installed
kubelet_state=not-installed
`
	result, err := Parse(output)
	if err != nil {
		t.Fatal(err)
	}
	result.ApplyPrevious(Result{CPUTotal: 1000, CPUIdle: 700})
	if !result.CPUValid || math.Abs(result.CPUAvailablePct-50) > 0.001 {
		t.Fatalf("CPU available = valid:%v %.2f", result.CPUValid, result.CPUAvailablePct)
	}
	if math.Abs(result.MemoryAvailablePct-25) > 0.001 {
		t.Fatalf("memory available = %.2f", result.MemoryAvailablePct)
	}
	if result.SwapTotal != 8000*1024 || result.SwapAvail != 6000*1024 {
		t.Fatalf("swap = available:%d total:%d", result.SwapAvail, result.SwapTotal)
	}
	if result.TopProcess.Command != "nginx" || result.TopProcess.PID != 42 {
		t.Fatalf("process = %#v", result.TopProcess)
	}
	if result.CPUCount != 8 || result.RootDiskAvail != 8000*1024 {
		t.Fatalf("capacity metadata = cores:%d root:%d", result.CPUCount, result.RootDiskAvail)
	}
	if result.OSName != "Debian GNU/Linux 13 (trixie)" || result.Systemd.State != "running" {
		t.Fatalf("system metadata = os:%q systemd:%#v", result.OSName, result.Systemd)
	}
	if result.Docker.Version != "28.1.1" || result.Docker.State != "active" {
		t.Fatalf("docker = %#v", result.Docker)
	}
	if result.SSHClient.Version != "OpenSSH_9.9p2" || result.SSHServer.State != "active" || result.SSHServiceUnit != "ssh.service" || result.SSHAgent.State != "running" || result.OpenSSL.Version != "OpenSSL 3.5.0" {
		t.Fatalf("SSH metadata = client:%#v server:%#v unit:%q agent:%#v openssl:%#v", result.SSHClient, result.SSHServer, result.SSHServiceUnit, result.SSHAgent, result.OpenSSL)
	}
}

func TestLinuxScriptProducesParsableResult(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux probe requires /proc")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = strings.NewReader(LinuxScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("LinuxScript failed: %v\n%s", err, output)
	}
	result, err := Parse(string(output))
	if err != nil {
		t.Fatalf("Parse(LinuxScript output): %v\n%s", err, output)
	}
	if result.CPUCount < 1 || result.MemoryTotal == 0 || result.OSName == "" || result.Kernel == "" {
		t.Fatalf("incomplete local metadata: %#v", result)
	}
}

func TestLinuxScriptFindsSystemSSHDOutsideUserPath(t *testing.T) {
	if !strings.Contains(LinuxScript, "[ -x /usr/sbin/sshd ]") {
		t.Fatal("Linux probe must inspect the standard system sshd path for unprivileged users")
	}
}

func TestFailureClassification(t *testing.T) {
	tests := []struct {
		message string
		want    Status
	}{
		{"Permission denied (publickey).", StatusAuth},
		{"Host key verification failed.", StatusHostKey},
		{"ssh: connect to host x port 22: Connection timed out", StatusUnreachable},
		{"remote probe returned malformed data", StatusError},
	}
	for _, tt := range tests {
		if got := Failure(nil, tt.message, timeZero, 0).Status; got != tt.want {
			t.Errorf("%q: got %s, want %s", tt.message, got, tt.want)
		}
	}
}

func TestHostKeyFailureDetails(t *testing.T) {
	message := `@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
The fingerprint for the ED25519 key sent by the remote host is
SHA256:qYs5BwieBR3FjenIoRIrkKXH0ECWGM84tJ2GjZrgDTg.
Offending ECDSA key in /home/tester/.ssh/known_hosts:103
Host key verification failed.`
	result := Failure(nil, message, timeZero, 0)
	if result.Status != StatusHostKey {
		t.Fatalf("status = %s", result.Status)
	}
	if result.PresentedHostKey != "SHA256:qYs5BwieBR3FjenIoRIrkKXH0ECWGM84tJ2GjZrgDTg" {
		t.Fatalf("fingerprint = %q", result.PresentedHostKey)
	}
	if result.KnownHostsFile != "/home/tester/.ssh/known_hosts" || result.KnownHostsLine != 103 {
		t.Fatalf("offending entry = %q:%d", result.KnownHostsFile, result.KnownHostsLine)
	}
	if result.ErrorMessage != "remote host identification changed" {
		t.Fatalf("error message = %q", result.ErrorMessage)
	}
}

var timeZero = func() (z time.Time) { return z }()
