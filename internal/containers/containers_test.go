package containers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

func TestDiscoverAndCommandsUseRuntimeWithoutShellInterpolation(t *testing.T) {
	dir := t.TempDir()
	runtimePath := filepath.Join(dir, "docker")
	script := `#!/bin/sh
if [ "$1" = context ] && [ "$2" = show ]; then printf '%s\n' default; exit 0; fi
if [ "$1" = context ] && [ "$2" = inspect ]; then printf '%s\n' '"unix:///var/run/docker.sock"'; exit 0; fi
if [ "$1" = ps ]; then
  printf '%s\n' '{"ID":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","Names":"demo-box","Image":"alpine:3.20","State":"running","Status":"Up 2 minutes","Ports":"127.0.0.1:8080->80/tcp"}'
  exit 0
fi
if [ "$1" = inspect ]; then
  printf '%s\n' '[{"Id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","Platform":"linux/amd64","Config":{"Image":"alpine:3.20","Entrypoint":["/entrypoint"],"Cmd":["serve","--token","do-not-display"]},"State":{"Status":"running","Running":true,"Health":{"Status":"healthy"}},"HostConfig":{"RestartPolicy":{"Name":"unless-stopped"}},"Mounts":[{"Type":"bind","Source":"/safe/source","Destination":"/data","RW":false}],"NetworkSettings":{"Networks":{"bridge":{}},"Ports":{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}}}]'
  exit 0
fi
if [ "$1" = exec ] && [ "$3" = test ]; then exit 0; fi
exit 0
`
	if err := os.WriteFile(runtimePath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.ContainerConfig{Enabled: true, Runtimes: []string{"docker"}, ShellPriority: []string{"/bin/ash", "/bin/sh"}}
	hosts, sources := Discover(context.Background(), cfg)
	if len(hosts) != 1 || len(sources) != 1 || sources[0].Err != nil || !sources[0].Dynamic {
		t.Fatalf("discovery = %#v / %#v", hosts, sources)
	}
	host := hosts[0]
	if host.TargetTransport() != inventory.TransportContainer || host.Alias != "demo-box" || Probe(host).Status != probe.StatusOnline {
		t.Fatalf("container host = %#v", host)
	}
	if host.ContainerContext != "default" || host.ContainerEndpoint != "unix:///var/run/docker.sock" || host.ContainerPlatform != "linux/amd64" || host.ContainerHealth != "healthy" {
		t.Fatalf("container inspection = %#v", host)
	}
	if host.ContainerCommand != "serve --token [redacted]" || strings.Contains(host.ContainerCommand, "do-not-display") {
		t.Fatalf("container command was not redacted: %q", host.ContainerCommand)
	}
	if host.ContainerMounts != "/safe/source→/data (bind,ro)" || !strings.Contains(host.ContainerNetworks, "127.0.0.1:8080→80/tcp") {
		t.Fatalf("container topology = mounts:%q networks:%q", host.ContainerMounts, host.ContainerNetworks)
	}
	interactive, shell, err := InteractiveCommand(context.Background(), host)
	if err != nil || shell != "/bin/ash" || strings.Join(interactive.Args[1:], " ") != "exec --interactive --tty "+host.ContainerID+" /bin/ash" {
		t.Fatalf("interactive = %#v shell=%q err=%v", interactive, shell, err)
	}
	command, err := Command(context.Background(), host, []string{"printf", "%s", "a;touch /tmp/no"})
	if err != nil || command.Args[len(command.Args)-1] != "a;touch /tmp/no" {
		t.Fatalf("command argv = %#v err=%v", command, err)
	}
	logs, err := LogsCommand(host)
	if err != nil || strings.Join(logs.Args[1:], " ") != "logs --tail 200 --follow "+host.ContainerID {
		t.Fatalf("logs = %#v err=%v", logs, err)
	}
}

func TestDiscoveryDistinguishesMissingDeniedAndEmptyRuntimes(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, sources := Discover(context.Background(), config.ContainerConfig{Enabled: true, Runtimes: []string{"docker"}})
		if len(sources) != 1 || sources[0].State != inventory.SourceStateUnavailable || sources[0].Err == nil {
			t.Fatalf("missing runtime = %#v", sources)
		}
	})

	t.Run("denied", func(t *testing.T) {
		dir := t.TempDir()
		writeRuntime(t, dir, `#!/bin/sh
if [ "$1" = context ]; then exit 0; fi
printf '%s\n' 'permission denied while connecting to the daemon socket' >&2
exit 1
`)
		t.Setenv("PATH", dir)
		_, sources := Discover(context.Background(), config.ContainerConfig{Enabled: true, Runtimes: []string{"docker"}})
		if len(sources) != 1 || sources[0].State != inventory.SourceStateDenied || sources[0].Err == nil {
			t.Fatalf("denied runtime = %#v", sources)
		}
	})

	t.Run("empty", func(t *testing.T) {
		dir := t.TempDir()
		writeRuntime(t, dir, `#!/bin/sh
if [ "$1" = context ] && [ "$2" = show ]; then printf '%s\n' default; fi
exit 0
`)
		t.Setenv("PATH", dir)
		hosts, sources := Discover(context.Background(), config.ContainerConfig{Enabled: true, Runtimes: []string{"docker"}})
		if len(hosts) != 0 || len(sources) != 1 || sources[0].State != inventory.SourceStateEmpty || sources[0].Err != nil {
			t.Fatalf("empty runtime = %#v / %#v", hosts, sources)
		}
	})
}

func TestSafeCommandTextRedactsAssignmentsAndURLCredentials(t *testing.T) {
	got := safeCommandText([]string{"serve", "PASSWORD=hunter2", "--api-key=value", "https://user:pass@example.test/path?token=hidden"})
	for _, forbidden := range []string{"hunter2", "value", "user:pass", "token=hidden"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("safe command %q contains %q", got, forbidden)
		}
	}
}

func TestDiscoverInspectionDoesNotRequireContainerShell(t *testing.T) {
	dir := t.TempDir()
	writeRuntime(t, dir, `#!/bin/sh
if [ "$1" = context ] && [ "$2" = show ]; then printf '%s\n' default; exit 0; fi
if [ "$1" = context ] && [ "$2" = inspect ]; then printf '%s\n' '"unix:///run/test.sock"'; exit 0; fi
if [ "$1" = ps ]; then
  printf '%s\n' '{"ID":"abcdef0123456789","Names":"distroless-stopped","Image":"scratch:test","State":"exited","Status":"Exited (0)"}'
  exit 0
fi
if [ "$1" = inspect ]; then
  printf '%s\n' '[{"Id":"abcdef0123456789","Platform":"linux/amd64","Config":{"Image":"scratch:test","Entrypoint":["/application"]},"State":{"Status":"exited","Running":false},"HostConfig":{"RestartPolicy":{"Name":"no"}}}]'
  exit 0
fi
if [ "$1" = exec ]; then exit 99; fi
exit 1
`)
	t.Setenv("PATH", dir)
	hosts, sources := Discover(context.Background(), config.ContainerConfig{
		Enabled: true, IncludeStopped: true, Runtimes: []string{"docker"},
		ShellPolicy: config.ContainerShellPolicyFirstAvailable, ShellPriority: []string{"/bin/sh"},
	})
	if len(hosts) != 1 || len(sources) != 1 || sources[0].State != inventory.SourceStateLoaded {
		t.Fatalf("shell-free discovery = %#v / %#v", hosts, sources)
	}
	if hosts[0].ContainerState != "exited" || hosts[0].ContainerEntrypoint != "/application" || Probe(hosts[0]).Status != probe.StatusUnreachable {
		t.Fatalf("stopped distroless inspection = %#v", hosts[0])
	}
	if _, _, err := InteractiveCommand(context.Background(), hosts[0]); err == nil {
		t.Fatal("shell action unexpectedly succeeded for a shell-free container")
	}
}

func writeRuntime(t *testing.T, dir, script string) {
	t.Helper()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestContainerIdentityValidationFailsClosed(t *testing.T) {
	host := inventory.Host{Alias: "bad", Transport: inventory.TransportContainer, ContainerRuntime: "docker", ContainerID: "$(touch-bad)"}
	if _, err := LogsCommand(host); err == nil {
		t.Fatal("unsafe runtime identity was accepted")
	}
}

func TestDiscoveryRejectsUnsafeRuntimeIdentityBeforeInspect(t *testing.T) {
	dir := t.TempDir()
	writeRuntime(t, dir, `#!/bin/sh
if [ "$1" = context ] && [ "$2" = show ]; then printf '%s\n' default; exit 0; fi
if [ "$1" = context ] && [ "$2" = inspect ]; then printf '%s\n' '"local"'; exit 0; fi
if [ "$1" = ps ]; then printf '%s\n' '{"ID":"--format","Names":"malicious","Image":"safe"}'; exit 0; fi
if [ "$1" = inspect ]; then printf '%s\n' inspect-was-called >&2; exit 42; fi
exit 0
`)
	t.Setenv("PATH", dir)
	hosts, sources := Discover(context.Background(), config.ContainerConfig{Enabled: true, Runtimes: []string{"docker"}})
	if len(hosts) != 0 || len(sources) != 1 || sources[0].Err == nil || !strings.Contains(sources[0].Err.Error(), "unsafe container identity") {
		t.Fatalf("unsafe discovery = %#v / %#v", hosts, sources)
	}
	if strings.Contains(sources[0].Err.Error(), "inspect-was-called") {
		t.Fatal("inspect was invoked with an unsafe runtime identity")
	}
}
