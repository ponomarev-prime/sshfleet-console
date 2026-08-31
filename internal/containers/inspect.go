package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
)

const maxRuntimeOutput = 8 << 20

type inspectRecord struct {
	ID           string   `json:"Id"`
	IDUpper      string   `json:"ID"`
	Name         string   `json:"Name"`
	Image        string   `json:"Image"`
	ImageName    string   `json:"ImageName"`
	Path         string   `json:"Path"`
	Args         []string `json:"Args"`
	Platform     string   `json:"Platform"`
	OS           string   `json:"Os"`
	OSUpper      string   `json:"OS"`
	Architecture string   `json:"Architecture"`
	Config       struct {
		Image      string          `json:"Image"`
		Entrypoint json.RawMessage `json:"Entrypoint"`
		Cmd        json.RawMessage `json:"Cmd"`
	} `json:"Config"`
	State struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
		Health  *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	RestartCount uint64 `json:"RestartCount"`
	Mounts       []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]json.RawMessage `json:"Networks"`
		Ports    map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

type podmanConnection struct {
	Name    string `json:"Name"`
	URI     string `json:"URI"`
	Default bool   `json:"Default"`
}

type imageInspectRecord struct {
	ID           string   `json:"Id"`
	IDUpper      string   `json:"ID"`
	RepoTags     []string `json:"RepoTags"`
	OS           string   `json:"Os"`
	OSUpper      string   `json:"OS"`
	Architecture string   `json:"Architecture"`
	Variant      string   `json:"Variant"`
}

func detectRuntimeContext(ctx context.Context, runtimeName, path string) (string, string, error) {
	switch runtimeName {
	case "docker":
		nameOutput, err := runRuntimeOutput(ctx, path, "context", "show")
		if err != nil {
			return "unknown", "unknown", fmt.Errorf("docker context: %w", err)
		}
		name := cleanBounded(strings.TrimSpace(string(nameOutput)), 128)
		if name == "" {
			name = "default"
		}
		endpointOutput, err := runRuntimeOutput(ctx, path, "context", "inspect", name, "--format", "{{json .Endpoints.docker.Host}}")
		if err != nil {
			return name, "unknown", fmt.Errorf("docker context endpoint: %w", err)
		}
		endpoint := decodeJSONString(endpointOutput)
		if endpoint == "" {
			endpoint = "local"
		}
		return name, sanitizeEndpoint(endpoint), nil
	case "podman":
		output, err := runRuntimeOutput(ctx, path, "system", "connection", "list", "--format", "json")
		if err != nil {
			return "local", "local", fmt.Errorf("podman connection: %w", err)
		}
		var connections []podmanConnection
		if len(strings.TrimSpace(string(output))) == 0 {
			return "local", "local", nil
		}
		if err := json.Unmarshal(output, &connections); err != nil {
			return "local", "local", fmt.Errorf("podman connection JSON: %w", err)
		}
		for _, connection := range connections {
			if connection.Default {
				return cleanBounded(connection.Name, 128), sanitizeEndpoint(connection.URI), nil
			}
		}
		return "local", "local", nil
	default:
		return "unknown", "unknown", fmt.Errorf("unsupported runtime %q", runtimeName)
	}
}

func inspectHosts(ctx context.Context, path, runtimeName string, hosts []inventory.Host) error {
	if len(hosts) == 0 {
		return nil
	}
	args := []string{"inspect"}
	for _, host := range hosts {
		args = append(args, host.ContainerID)
	}
	output, err := runRuntimeOutput(ctx, path, args...)
	if err != nil {
		message := commandError(runtimeName+" inspect", err, output)
		for i := range hosts {
			hosts[i].ContainerInspectError = cleanBounded(message.Error(), 512)
		}
		return message
	}
	var records []inspectRecord
	if err := json.Unmarshal(output, &records); err != nil {
		message := fmt.Errorf("%s inspect JSON: %w", runtimeName, err)
		for i := range hosts {
			hosts[i].ContainerInspectError = cleanBounded(message.Error(), 512)
		}
		return message
	}
	recordsByID := make(map[string]inspectRecord, len(records))
	for _, record := range records {
		id := strings.TrimSpace(record.ID)
		if id == "" {
			id = strings.TrimSpace(record.IDUpper)
		}
		if id != "" {
			recordsByID[id] = record
		}
	}
	missing := 0
	for i := range hosts {
		record, ok := recordsByID[hosts[i].ContainerID]
		if !ok {
			missing++
			hosts[i].ContainerInspectError = "runtime inspect did not return this immutable container ID"
			continue
		}
		applyInspection(&hosts[i], record)
	}
	if missing > 0 {
		return fmt.Errorf("%s inspect omitted %d of %d containers", runtimeName, missing, len(hosts))
	}
	return inspectImagePlatforms(ctx, path, runtimeName, hosts)
}

func inspectImagePlatforms(ctx context.Context, path, runtimeName string, hosts []inventory.Host) error {
	refs := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if strings.Contains(host.ContainerPlatform, "/") || !safeRuntimeReference(host.ContainerImage) {
			continue
		}
		if _, ok := seen[host.ContainerImage]; ok {
			continue
		}
		seen[host.ContainerImage] = struct{}{}
		refs = append(refs, host.ContainerImage)
	}
	if len(refs) == 0 {
		return nil
	}
	args := append([]string{"image", "inspect"}, refs...)
	output, err := runRuntimeOutput(ctx, path, args...)
	if err != nil {
		return commandError(runtimeName+" image inspect", err, output)
	}
	var records []imageInspectRecord
	if err := json.Unmarshal(output, &records); err != nil {
		return fmt.Errorf("%s image inspect JSON: %w", runtimeName, err)
	}
	platforms := make(map[string]string, len(records)*2)
	for index, record := range records {
		osName := record.OS
		if osName == "" {
			osName = record.OSUpper
		}
		platform := strings.Trim(osName+"/"+record.Architecture, "/")
		if record.Variant != "" {
			platform += "/" + record.Variant
		}
		platform = cleanBounded(platform, 128)
		for _, tag := range record.RepoTags {
			platforms[tag] = platform
		}
		id := record.ID
		if id == "" {
			id = record.IDUpper
		}
		if id != "" {
			platforms[id] = platform
		}
		if index < len(refs) {
			platforms[refs[index]] = platform
		}
	}
	for i := range hosts {
		if platform := platforms[hosts[i].ContainerImage]; platform != "" {
			hosts[i].ContainerPlatform = platform
		}
	}
	return nil
}

func safeRuntimeReference(value string) bool {
	return value != "" && len(value) <= 512 && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00\r\n")
}

func applyInspection(host *inventory.Host, record inspectRecord) {
	if value := cleanBounded(record.Config.Image, 256); value != "" {
		host.ContainerImage = value
	} else if value := cleanBounded(record.ImageName, 256); value != "" {
		host.ContainerImage = value
	}
	if value := cleanBounded(record.State.Status, 64); value != "" {
		host.ContainerState = value
	}
	if record.State.Health != nil {
		host.ContainerHealth = cleanBounded(record.State.Health.Status, 64)
	} else {
		host.ContainerHealth = "not-configured"
	}
	entrypoint := decodeStringList(record.Config.Entrypoint)
	if len(entrypoint) == 0 && record.Path != "" {
		entrypoint = []string{record.Path}
	}
	command := decodeStringList(record.Config.Cmd)
	if len(command) == 0 {
		command = record.Args
	}
	host.ContainerEntrypoint = safeCommandText(entrypoint)
	host.ContainerCommand = safeCommandText(command)
	host.ContainerMounts = mountSummary(record)
	host.ContainerNetworks = networkSummary(record)
	if value := cleanBounded(record.HostConfig.RestartPolicy.Name, 64); value != "" {
		host.ContainerRestart = value
	} else {
		host.ContainerRestart = "no"
	}
	host.ContainerPlatform = platformSummary(record)
}

func decodeStringList(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var value string
	if json.Unmarshal(raw, &value) == nil && value != "" {
		return []string{value}
	}
	return nil
}

func safeCommandText(argv []string) string {
	if len(argv) == 0 {
		return "—"
	}
	clean := make([]string, 0, len(argv))
	redactNext := false
	for _, argument := range argv {
		value := cleanBounded(argument, 256)
		lower := strings.ToLower(value)
		if redactNext {
			clean = append(clean, "[redacted]")
			redactNext = false
			continue
		}
		if secretFlag(lower) {
			clean = append(clean, value)
			redactNext = true
			continue
		}
		if index := strings.IndexByte(value, '='); index > 0 && secretName(strings.ToLower(strings.TrimLeft(value[:index], "-"))) {
			clean = append(clean, value[:index+1]+"[redacted]")
			continue
		}
		clean = append(clean, sanitizeEndpoint(value))
	}
	return cleanBounded(strings.Join(clean, " "), 512)
}

func secretFlag(value string) bool {
	name := strings.TrimLeft(value, "-")
	return secretName(name)
}

func secretName(value string) bool {
	for _, marker := range []string{"password", "passwd", "passphrase", "token", "secret", "api-key", "apikey", "access-key", "private-key"} {
		if value == marker || strings.HasSuffix(value, "_"+marker) || strings.HasSuffix(value, "-"+marker) {
			return true
		}
	}
	return false
}

func mountSummary(record inspectRecord) string {
	if len(record.Mounts) == 0 {
		return "—"
	}
	items := make([]string, 0, len(record.Mounts))
	for _, mount := range record.Mounts {
		mode := "ro"
		if mount.RW {
			mode = "rw"
		}
		items = append(items, fmt.Sprintf("%s→%s (%s,%s)", cleanBounded(mount.Source, 128), cleanBounded(mount.Destination, 128), cleanBounded(mount.Type, 32), mode))
	}
	sort.Strings(items)
	return cleanBounded(strings.Join(items, "; "), 768)
}

func networkSummary(record inspectRecord) string {
	items := make([]string, 0, len(record.NetworkSettings.Networks)+len(record.NetworkSettings.Ports))
	for name := range record.NetworkSettings.Networks {
		items = append(items, cleanBounded(name, 128))
	}
	for containerPort, bindings := range record.NetworkSettings.Ports {
		if len(bindings) == 0 {
			items = append(items, cleanBounded(containerPort, 64))
			continue
		}
		for _, binding := range bindings {
			host := binding.HostIP
			if host == "" {
				host = "*"
			}
			items = append(items, cleanBounded(host+":"+binding.HostPort+"→"+containerPort, 128))
		}
	}
	if len(items) == 0 {
		return "—"
	}
	sort.Strings(items)
	return cleanBounded(strings.Join(items, ", "), 768)
}

func platformSummary(record inspectRecord) string {
	if value := cleanBounded(record.Platform, 128); value != "" {
		return value
	}
	osName := record.OS
	if osName == "" {
		osName = record.OSUpper
	}
	if osName == "" && record.Architecture == "" {
		return "unknown"
	}
	return cleanBounded(strings.Trim(osName+"/"+record.Architecture, "/"), 128)
}

func runRuntimeOutput(ctx context.Context, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...) // #nosec G204 -- path is an allowlisted runtime and args are fixed/discovered immutable IDs.
	output, err := command.CombinedOutput()
	if len(output) > maxRuntimeOutput {
		return output[:maxRuntimeOutput], fmt.Errorf("runtime output exceeded %d bytes", maxRuntimeOutput)
	}
	return output, err
}

func commandError(operation string, err error, output []byte) error {
	detail := cleanBounded(strings.TrimSpace(string(output)), 512)
	if detail == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w: %s", operation, err, detail)
}

func classifyRuntimeError(err error) inventory.SourceState {
	if err == nil {
		return inventory.SourceStateLoaded
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return inventory.SourceStateUnavailable
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"permission denied", "access denied", "operation not permitted", "authorization denied"} {
		if strings.Contains(lower, marker) {
			return inventory.SourceStateDenied
		}
	}
	return inventory.SourceStateUnavailable
}

func runtimeDetail(contextName, endpoint string, partialErr error) string {
	detail := "context=" + valueOr(contextName, "unknown") + " · endpoint=" + valueOr(endpoint, "unknown")
	if partialErr != nil {
		detail += " · " + cleanBounded(partialErr.Error(), 512)
	}
	return detail
}

func joinPartialErrors(left, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return fmt.Errorf("%v; %v", left, right)
}

func decodeJSONString(value []byte) string {
	trimmed := strings.TrimSpace(string(value))
	var decoded string
	if json.Unmarshal([]byte(trimmed), &decoded) == nil {
		return decoded
	}
	return strings.Trim(trimmed, "\"")
}

func sanitizeEndpoint(value string) string {
	clean := cleanBounded(value, 512)
	parsed, err := url.Parse(clean)
	if err != nil || parsed.Scheme == "" {
		return clean
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func cleanBounded(value string, limit int) string {
	value = strings.TrimSpace(session.CleanText(value))
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
