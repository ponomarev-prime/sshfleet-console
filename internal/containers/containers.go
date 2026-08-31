package containers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
)

type psRecord struct {
	ID     string
	Names  psText
	Image  string
	State  string
	Status string
	Ports  psText
}

// Docker renders Names/Ports as strings, while Podman versions may render the
// same fields as JSON arrays or objects. Keep the parser strict about identity
// but tolerant about these display-only values.
type psText string

func (value *psText) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		*value = psText(text)
		return nil
	}
	var list []string
	if json.Unmarshal(data, &list) == nil {
		*value = psText(strings.Join(list, ", "))
		return nil
	}
	if string(data) == "null" {
		*value = ""
		return nil
	}
	*value = psText(string(data))
	return nil
}

type runtimeDiscovery struct {
	hosts      []inventory.Host
	context    string
	endpoint   string
	partialErr error
}

func Discover(ctx context.Context, cfg config.ContainerConfig) ([]inventory.Host, []inventory.SourceSummary) {
	if !cfg.Enabled {
		return nil, nil
	}
	var hosts []inventory.Host
	var summaries []inventory.SourceSummary
	for _, runtimeName := range cfg.Runtimes {
		sourceName := "containers · " + runtimeName
		path, err := exec.LookPath(runtimeName)
		if err != nil {
			summaries = append(summaries, inventory.SourceSummary{
				Name: sourceName, Path: runtimeName, Dynamic: true,
				State: inventory.SourceStateUnavailable, Err: fmt.Errorf("runtime executable is not available: %w", err),
			})
			continue
		}
		discovered, err := discoverRuntime(ctx, runtimeName, path, cfg)
		summary := inventory.SourceSummary{Name: sourceName, Path: path, Dynamic: true}
		if err != nil {
			summary.State = classifyRuntimeError(err)
			summary.Err = err
			summaries = append(summaries, summary)
			continue
		}
		summary.Hosts = len(discovered.hosts)
		summary.State = inventory.SourceStateLoaded
		if len(discovered.hosts) == 0 {
			summary.State = inventory.SourceStateEmpty
		}
		summary.Detail = runtimeDetail(discovered.context, discovered.endpoint, discovered.partialErr)
		if discovered.partialErr != nil {
			summary.State = inventory.SourceStatePartial
		}
		hosts = append(hosts, discovered.hosts...)
		summaries = append(summaries, summary)
	}
	return hosts, summaries
}

func discoverRuntime(ctx context.Context, runtimeName, path string, cfg config.ContainerConfig) (runtimeDiscovery, error) {
	contextName, endpoint, contextErr := detectRuntimeContext(ctx, runtimeName, path)
	args := []string{"ps", "--no-trunc", "--format", "{{json .}}"}
	if cfg.IncludeStopped {
		args = append(args, "--all")
	}
	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 -- resolved allowlisted runtime and fixed arguments.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return runtimeDiscovery{}, commandError(runtimeName+" ps", err, output)
	}
	var hosts []inventory.Host
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record psRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return runtimeDiscovery{}, fmt.Errorf("%s ps JSON: %w", runtimeName, err)
		}
		record.ID = session.CleanText(strings.TrimSpace(record.ID))
		name := cleanBounded(strings.TrimSpace(string(record.Names)), 256)
		if record.ID == "" || name == "" || strings.HasPrefix(record.ID, "-") {
			return runtimeDiscovery{}, fmt.Errorf("%s ps returned an unsafe container identity", runtimeName)
		}
		host := inventory.Host{
			ID: "container:" + runtimeName + ":" + record.ID, Alias: name, Name: name,
			SourceName: "containers · " + runtimeName, ConfigPath: path,
			Transport: inventory.TransportContainer, Probe: true,
			ContainerRuntime: runtimeName, ContainerID: record.ID,
			ContainerImage: cleanBounded(record.Image, 256), ContainerState: cleanBounded(record.State, 64),
			ContainerStatus: cleanBounded(record.Status, 256), ContainerPorts: cleanBounded(string(record.Ports), 512),
			ContainerContext: contextName, ContainerEndpoint: endpoint,
			ContainerShellPolicy:    cfg.ShellPolicy,
			ContainerDiscoveryState: string(inventory.SourceStateLoaded),
			ContainerShells:         append([]string(nil), cfg.ShellPriority...), Tags: []string{"container", runtimeName},
		}
		if err := host.Validate(); err != nil {
			return runtimeDiscovery{}, fmt.Errorf("%s ps returned an unsafe container identity: %w", runtimeName, err)
		}
		hosts = append(hosts, host)
	}
	if err := scanner.Err(); err != nil {
		return runtimeDiscovery{}, fmt.Errorf("%s ps output: %w", runtimeName, err)
	}
	inspectErr := inspectHosts(ctx, path, runtimeName, hosts)
	partialErr := joinPartialErrors(contextErr, inspectErr)
	if partialErr != nil {
		for i := range hosts {
			hosts[i].ContainerDiscoveryState = string(inventory.SourceStatePartial)
			hosts[i].ContainerDiscoveryError = cleanBounded(partialErr.Error(), 512)
		}
	}
	return runtimeDiscovery{hosts: hosts, context: contextName, endpoint: endpoint, partialErr: partialErr}, nil
}

func Probe(host inventory.Host) probe.Result {
	if host.ContainerDiscoveryState == string(inventory.SourceStateStale) {
		return probe.Result{Status: probe.StatusUnknown, CheckedAt: time.Now(), ErrorMessage: "runtime state is stale: " + host.ContainerDiscoveryError}
	}
	status := probe.StatusOnline
	message := ""
	if !strings.EqualFold(host.ContainerState, "running") {
		status = probe.StatusUnreachable
		message = "container is " + host.ContainerState
	}
	return probe.Result{Status: status, CheckedAt: time.Now(), ErrorMessage: message}
}

func InteractiveCommand(ctx context.Context, host inventory.Host) (*exec.Cmd, string, error) {
	path, shell, err := resolveShell(ctx, host)
	if err != nil {
		return nil, "", err
	}
	cmd := exec.Command(path, "exec", "--interactive", "--tty", host.ContainerID, shell) // #nosec G204 -- immutable discovered ID and allowlisted shell path.
	return cmd, shell, nil
}

func LogsCommand(host inventory.Host) (*exec.Cmd, error) {
	if err := host.Validate(); err != nil {
		return nil, err
	}
	path, err := exec.LookPath(host.ContainerRuntime)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", host.ContainerRuntime, err)
	}
	return exec.Command(path, "logs", "--tail", "200", "--follow", host.ContainerID), nil // #nosec G204 -- immutable discovered ID, no shell.
}

// Command executes a trusted argv preset without involving a shell. Container
// IDs come from runtime discovery and are revalidated immediately before use.
func Command(ctx context.Context, host inventory.Host, argv []string) (*exec.Cmd, error) {
	if err := host.Validate(); err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("container command argv is empty")
	}
	for i, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return nil, fmt.Errorf("container command argv[%d] is empty or unsafe", i)
		}
	}
	path, err := exec.LookPath(host.ContainerRuntime)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", host.ContainerRuntime, err)
	}
	args := append([]string{"exec", host.ContainerID}, argv...)
	return exec.CommandContext(ctx, path, args...), nil // #nosec G204 -- validated discovered ID and locally trusted argv, no shell.
}

func resolveShell(ctx context.Context, host inventory.Host) (string, string, error) {
	if err := host.Validate(); err != nil {
		return "", "", err
	}
	path, err := exec.LookPath(host.ContainerRuntime)
	if err != nil {
		return "", "", fmt.Errorf("find %s: %w", host.ContainerRuntime, err)
	}
	for _, shell := range host.ContainerShells {
		check := exec.CommandContext(ctx, path, "exec", host.ContainerID, "test", "-x", shell) // #nosec G204 -- immutable discovered ID and validated absolute shell path.
		if check.Run() == nil {
			return path, shell, nil
		}
	}
	return "", "", fmt.Errorf("container %s has none of the configured shells: %s", host.Alias, strings.Join(host.ContainerShells, ", "))
}
