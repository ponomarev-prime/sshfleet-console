package probe

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Status string

const (
	StatusUnknown     Status = "unknown"
	StatusOnline      Status = "online"
	StatusGit         Status = "git-access"
	StatusUnreachable Status = "unreachable"
	StatusAuth        Status = "auth-error"
	StatusHostKey     Status = "host-key-error"
	StatusError       Status = "probe-error"
)

type Process struct {
	PID     int
	State   string
	Command string
	CPU     float64
}

type Component struct {
	Version string
	State   string
}

type Result struct {
	Status             Status
	CheckedAt          time.Time
	Latency            time.Duration
	CPUTotal           uint64
	CPUIdle            uint64
	CPUCount           int
	CPUAvailablePct    float64
	CPUValid           bool
	MemoryTotal        uint64
	MemoryAvail        uint64
	MemoryAvailablePct float64
	SwapTotal          uint64
	SwapAvail          uint64
	RootDiskTotal      uint64
	RootDiskAvail      uint64
	Load               [3]float64
	Uptime             time.Duration
	TopProcess         Process
	OSName             string
	Kernel             string
	Architecture       string
	Init               string
	Virtualization     string
	Systemd            Component
	SystemdFailedUnits int
	SSHClient          Component
	SSHServer          Component
	SSHAgent           Component
	OpenSSL            Component
	SSHServiceUnit     string
	SSHSocketState     string
	SSHTools           string
	Docker             Component
	Containerd         Component
	Podman             Component
	Kubelet            Component
	GitAuthMethod      string
	GitMessage         string
	PresentedHostKey   string
	KnownHostsFile     string
	KnownHostsLine     int
	ErrorMessage       string
}

func Parse(output string) (Result, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = value
	}

	required := []string{"cpu_total", "cpu_idle", "mem_total_kb", "mem_available_kb", "load", "uptime_seconds"}
	for _, key := range required {
		if _, ok := values[key]; !ok {
			return Result{}, fmt.Errorf("probe output missing %q", key)
		}
	}

	var result Result
	var err error
	if result.CPUTotal, err = parseUint(values["cpu_total"]); err != nil {
		return Result{}, fmt.Errorf("cpu_total: %w", err)
	}
	if result.CPUIdle, err = parseUint(values["cpu_idle"]); err != nil {
		return Result{}, fmt.Errorf("cpu_idle: %w", err)
	}
	memTotalKB, err := parseUint(values["mem_total_kb"])
	if err != nil {
		return Result{}, fmt.Errorf("mem_total_kb: %w", err)
	}
	memAvailKB, err := parseUint(values["mem_available_kb"])
	if err != nil {
		return Result{}, fmt.Errorf("mem_available_kb: %w", err)
	}
	result.MemoryTotal = memTotalKB * 1024
	result.MemoryAvail = memAvailKB * 1024
	if result.MemoryTotal > 0 {
		available := min(result.MemoryAvail, result.MemoryTotal)
		result.MemoryAvailablePct = float64(available) * 100 / float64(result.MemoryTotal)
	}
	swapTotalKB, _ := parseUint(values["swap_total_kb"])
	swapAvailKB, _ := parseUint(values["swap_available_kb"])
	result.SwapTotal = swapTotalKB * 1024
	result.SwapAvail = min(swapAvailKB, swapTotalKB) * 1024
	result.CPUCount, _ = strconv.Atoi(values["cpu_count"])
	rootTotalKB, _ := parseUint(values["root_total_kb"])
	rootAvailKB, _ := parseUint(values["root_available_kb"])
	result.RootDiskTotal = rootTotalKB * 1024
	result.RootDiskAvail = rootAvailKB * 1024

	loadFields := strings.Fields(values["load"])
	if len(loadFields) != 3 {
		return Result{}, fmt.Errorf("load: expected three values, got %q", values["load"])
	}
	for i, field := range loadFields {
		result.Load[i], err = strconv.ParseFloat(field, 64)
		if err != nil {
			return Result{}, fmt.Errorf("load[%d]: %w", i, err)
		}
	}
	uptimeSeconds, err := strconv.ParseFloat(values["uptime_seconds"], 64)
	if err != nil {
		return Result{}, fmt.Errorf("uptime_seconds: %w", err)
	}
	result.Uptime = time.Duration(uptimeSeconds * float64(time.Second))

	if raw := values["process"]; raw != "" {
		fields := strings.SplitN(raw, "|", 4)
		if len(fields) == 4 {
			result.TopProcess.PID, _ = strconv.Atoi(fields[0])
			result.TopProcess.State = fields[1]
			result.TopProcess.Command = fields[2]
			result.TopProcess.CPU, _ = strconv.ParseFloat(fields[3], 64)
		}
	}
	result.OSName = values["os_name"]
	result.Kernel = values["kernel"]
	result.Architecture = values["architecture"]
	result.Init = values["init"]
	result.Virtualization = values["virtualization"]
	result.Systemd = Component{Version: values["systemd_version"], State: values["systemd_state"]}
	result.SystemdFailedUnits, _ = strconv.Atoi(values["systemd_failed_units"])
	result.SSHClient = Component{Version: values["ssh_client_version"], State: values["ssh_client_state"]}
	result.SSHServer = Component{Version: values["sshd_version"], State: values["sshd_state"]}
	result.SSHAgent = Component{State: values["ssh_agent_state"]}
	result.OpenSSL = Component{Version: values["openssl_version"], State: values["openssl_state"]}
	result.SSHServiceUnit = values["ssh_service_unit"]
	result.SSHSocketState = values["ssh_socket_state"]
	result.SSHTools = values["ssh_tools"]
	result.Docker = Component{Version: values["docker_version"], State: values["docker_state"]}
	result.Containerd = Component{Version: values["containerd_version"], State: values["containerd_state"]}
	result.Podman = Component{Version: values["podman_version"], State: values["podman_state"]}
	result.Kubelet = Component{Version: values["kubelet_version"], State: values["kubelet_state"]}
	result.Status = StatusOnline
	return result, nil
}

func (r *Result) ApplyPrevious(previous Result) {
	if previous.CPUTotal == 0 || r.CPUTotal <= previous.CPUTotal || r.CPUIdle < previous.CPUIdle {
		return
	}
	totalDelta := r.CPUTotal - previous.CPUTotal
	idleDelta := r.CPUIdle - previous.CPUIdle
	if totalDelta == 0 || idleDelta > totalDelta {
		return
	}
	r.CPUAvailablePct = float64(idleDelta) * 100 / float64(totalDelta)
	r.CPUValid = true
}

func Failure(err error, stderr string, checkedAt time.Time, latency time.Duration) Result {
	message := strings.TrimSpace(stderr)
	if message == "" && err != nil {
		message = err.Error()
	}
	lower := strings.ToLower(message)
	status := StatusError
	switch {
	case strings.Contains(lower, "host key verification failed"),
		strings.Contains(lower, "remote host identification has changed"),
		strings.Contains(lower, "no host key is known"):
		status = StatusHostKey
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "too many authentication failures"),
		strings.Contains(lower, "no supported authentication methods"):
		status = StatusAuth
	case errors.Is(err, context.DeadlineExceeded),
		strings.Contains(lower, "connection timed out"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "could not resolve hostname"),
		strings.Contains(lower, "network is unreachable"),
		strings.Contains(lower, "i/o timeout"):
		status = StatusUnreachable
	}
	result := Result{Status: status, CheckedAt: checkedAt, Latency: latency, ErrorMessage: firstLine(message)}
	if status == StatusHostKey {
		result.ErrorMessage = "host key verification failed"
		if strings.Contains(lower, "remote host identification has changed") {
			result.ErrorMessage = "remote host identification changed"
		}
		result.PresentedHostKey = hostKeyFingerprint(message)
		result.KnownHostsFile, result.KnownHostsLine = offendingKnownHostsEntry(message)
	}
	return result
}

var (
	hostKeyFingerprintPattern = regexp.MustCompile(`SHA256:[A-Za-z0-9+/]+={0,3}`)
	offendingKeyPattern       = regexp.MustCompile(`(?m)Offending [^\r\n]* key in (.+):([0-9]+)\r?$`)
)

func hostKeyFingerprint(message string) string {
	return hostKeyFingerprintPattern.FindString(message)
}

func offendingKnownHostsEntry(message string) (string, int) {
	match := offendingKeyPattern.FindStringSubmatch(message)
	if len(match) != 3 {
		return "", 0
	}
	line, _ := strconv.Atoi(match[2])
	return strings.TrimSpace(match[1]), line
}

func parseUint(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(s), 10, 64)
}

func firstLine(s string) string {
	if line, _, ok := strings.Cut(s, "\n"); ok {
		return line
	}
	return s
}

const LinuxScript = `set -eu
export LC_ALL=C

set -- $(head -n 1 /proc/stat)
shift
cpu_total=0
for value in "$@"; do cpu_total=$((cpu_total + value)); done
cpu_idle=$(( ${4:-0} + ${5:-0} ))

mem_total=$(awk '/^MemTotal:/ {print $2; exit}' /proc/meminfo)
mem_available=$(awk '/^MemAvailable:/ {print $2; exit}' /proc/meminfo)
swap_total=$(awk '/^SwapTotal:/ {print $2; exit}' /proc/meminfo)
swap_available=$(awk '/^SwapFree:/ {print $2; exit}' /proc/meminfo)
load=$(awk '{print $1 " " $2 " " $3}' /proc/loadavg)
uptime=$(awk '{print $1}' /proc/uptime)
process=$(ps -eo pid=,stat=,comm=,pcpu= --sort=-pcpu 2>/dev/null | awk '$3 !~ /^(awk|ps|sh|sshd)$/ {print $1 "|" $2 "|" $3 "|" $4; exit}')

cpu_count=$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)
if [ -z "$cpu_count" ]; then
  cpu_count=$(awk '/^processor[[:space:]]*:/ {count++} END {print count+0}' /proc/cpuinfo)
fi

root_stats=$(df -Pk / 2>/dev/null | awk 'NR == 2 {print $2 " " $4; exit}')
set -- $root_stats
root_total=${1:-0}
root_available=${2:-0}

os_name=$(awk -F= '$1 == "PRETTY_NAME" {value=substr($0, index($0, "=")+1); gsub(/^"|"$/, "", value); print value; exit}' /etc/os-release 2>/dev/null || true)
if [ -z "$os_name" ]; then os_name=$(uname -s 2>/dev/null || true); fi
kernel=$(uname -sr 2>/dev/null || true)
architecture=$(uname -m 2>/dev/null || true)
init_name=$(cat /proc/1/comm 2>/dev/null || true)
virtualization=$(systemd-detect-virt 2>/dev/null || true)
if [ -z "$virtualization" ]; then virtualization=none; fi

service_state() {
  service_name=$1
  process_name=$2
  if [ "$init_name" = systemd ] && command -v systemctl >/dev/null 2>&1; then
    state=$(systemctl is-active "$service_name" 2>/dev/null || true)
    if [ -n "$state" ]; then printf '%s' "$state"; else printf unknown; fi
  elif ps -eo comm= 2>/dev/null | awk -v name="$process_name" '$1 == name {found=1} END {exit !found}'; then
    printf running
  else
    printf inactive
  fi
}

systemd_version=
systemd_state=not-installed
systemd_failed_units=
if command -v systemd >/dev/null 2>&1; then
  systemd_version=$(systemd --version 2>/dev/null | awk 'NR == 1 {print $2}' || true)
  systemd_state=not-init
  if [ "$init_name" = systemd ] && command -v systemctl >/dev/null 2>&1; then
    systemd_state=$(systemctl is-system-running 2>/dev/null || true)
    if [ -z "$systemd_state" ]; then systemd_state=unknown; fi
    systemd_failed_units=$(systemctl --failed --no-legend --no-pager 2>/dev/null | awk 'NF {count++} END {print count+0}' || true)
  fi
fi

ssh_client_version=
ssh_client_state=not-installed
if command -v ssh >/dev/null 2>&1; then
  ssh_client_version=$(ssh -V 2>&1 | awk 'NR == 1 {print; exit}' || true)
  ssh_client_state=installed
fi

sshd_version=
sshd_state=not-installed
ssh_service_unit=
ssh_socket_state=not-installed
sshd_binary=$(command -v sshd 2>/dev/null || true)
if [ -z "$sshd_binary" ] && [ -x /usr/sbin/sshd ]; then sshd_binary=/usr/sbin/sshd; fi
if [ -n "$sshd_binary" ]; then
  sshd_version=$($sshd_binary -V 2>&1 | awk 'NR == 1 {print; exit}' || true)
  sshd_state=$(service_state ssh sshd)
  if [ "$init_name" = systemd ] && command -v systemctl >/dev/null 2>&1; then
    for unit in ssh.service sshd.service; do
      load_state=$(systemctl show "$unit" --property=LoadState --value 2>/dev/null || true)
      if [ -n "$load_state" ] && [ "$load_state" != not-found ]; then
        ssh_service_unit=$unit
        sshd_state=$(systemctl is-active "$unit" 2>/dev/null || true)
        if [ -z "$sshd_state" ]; then sshd_state=unknown; fi
        break
      fi
    done
    for unit in ssh.socket sshd.socket; do
      load_state=$(systemctl show "$unit" --property=LoadState --value 2>/dev/null || true)
      if [ -n "$load_state" ] && [ "$load_state" != not-found ]; then
        socket_active=$(systemctl is-active "$unit" 2>/dev/null || true)
        if [ -z "$socket_active" ]; then socket_active=unknown; fi
        ssh_socket_state="$unit $socket_active"
        break
      fi
    done
  fi
fi

ssh_agent_state=not-installed
if command -v ssh-agent >/dev/null 2>&1; then
  ssh_agent_state=available
  if ps -eo comm= 2>/dev/null | awk '$1 == "ssh-agent" {found=1} END {exit !found}'; then
    ssh_agent_state=running
  fi
fi

ssh_tools=
for tool in scp sftp ssh-keygen ssh-add; do
  if command -v "$tool" >/dev/null 2>&1; then
    if [ -n "$ssh_tools" ]; then ssh_tools="$ssh_tools, "; fi
    ssh_tools="$ssh_tools$tool"
  fi
done

openssl_version=
openssl_state=not-installed
if command -v openssl >/dev/null 2>&1; then
  openssl_version=$(openssl version 2>/dev/null | awk 'NR == 1 {print; exit}' || true)
  openssl_state=installed
fi

docker_version=
docker_state=not-installed
if command -v docker >/dev/null 2>&1; then
  docker_version=$(docker --version 2>/dev/null | awk 'NR == 1 {gsub(/,/, "", $3); print $3}' || true)
  docker_state=$(service_state docker dockerd)
fi

containerd_version=
containerd_state=not-installed
if command -v containerd >/dev/null 2>&1; then
  containerd_version=$(containerd --version 2>/dev/null | awk 'NR == 1 {print $3}' || true)
  containerd_state=$(service_state containerd containerd)
fi

podman_version=
podman_state=not-installed
if command -v podman >/dev/null 2>&1; then
  podman_version=$(podman --version 2>/dev/null | awk 'NR == 1 {print $3}' || true)
  podman_state=installed
fi

kubelet_version=
kubelet_state=not-installed
if command -v kubelet >/dev/null 2>&1; then
  kubelet_version=$(kubelet --version 2>/dev/null | awk 'NR == 1 {print $2}' || true)
  kubelet_state=$(service_state kubelet kubelet)
fi

printf 'cpu_total=%s\n' "$cpu_total"
printf 'cpu_idle=%s\n' "$cpu_idle"
printf 'cpu_count=%s\n' "$cpu_count"
printf 'mem_total_kb=%s\n' "$mem_total"
printf 'mem_available_kb=%s\n' "$mem_available"
printf 'swap_total_kb=%s\n' "$swap_total"
printf 'swap_available_kb=%s\n' "$swap_available"
printf 'root_total_kb=%s\n' "$root_total"
printf 'root_available_kb=%s\n' "$root_available"
printf 'load=%s\n' "$load"
printf 'uptime_seconds=%s\n' "$uptime"
printf 'process=%s\n' "$process"
printf 'os_name=%s\n' "$os_name"
printf 'kernel=%s\n' "$kernel"
printf 'architecture=%s\n' "$architecture"
printf 'init=%s\n' "$init_name"
printf 'virtualization=%s\n' "$virtualization"
printf 'systemd_version=%s\n' "$systemd_version"
printf 'systemd_state=%s\n' "$systemd_state"
printf 'systemd_failed_units=%s\n' "$systemd_failed_units"
printf 'ssh_client_version=%s\n' "$ssh_client_version"
printf 'ssh_client_state=%s\n' "$ssh_client_state"
printf 'sshd_version=%s\n' "$sshd_version"
printf 'sshd_state=%s\n' "$sshd_state"
printf 'ssh_service_unit=%s\n' "$ssh_service_unit"
printf 'ssh_socket_state=%s\n' "$ssh_socket_state"
printf 'ssh_agent_state=%s\n' "$ssh_agent_state"
printf 'ssh_tools=%s\n' "$ssh_tools"
printf 'openssl_version=%s\n' "$openssl_version"
printf 'openssl_state=%s\n' "$openssl_state"
printf 'docker_version=%s\n' "$docker_version"
printf 'docker_state=%s\n' "$docker_state"
printf 'containerd_version=%s\n' "$containerd_version"
printf 'containerd_state=%s\n' "$containerd_state"
printf 'podman_version=%s\n' "$podman_version"
printf 'podman_state=%s\n' "$podman_state"
printf 'kubelet_version=%s\n' "$kubelet_version"
printf 'kubelet_state=%s\n' "$kubelet_state"
`
