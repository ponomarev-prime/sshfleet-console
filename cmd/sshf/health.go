package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
	"github.com/ponomarev-prime/sshfleet-console/internal/toolcheck"
)

type healthLevel string

const (
	healthOK   healthLevel = "OK"
	healthWarn healthLevel = "WARN"
	healthFail healthLevel = "FAIL"
	healthInfo healthLevel = "INFO"
)

type healthCheck struct {
	Level  healthLevel
	Name   string
	Detail string
	Hint   string
}

type healthReport struct {
	Checks   []healthCheck
	Warnings int
	Failures int
}

func runHealthcheckCommand(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(output)
	configPath := ""
	terminalShell := ""
	var terminalArgs shellArgFlags
	strict := false
	flags.StringVar(&configPath, "config", "", "main SSH Fleet Console TOML config")
	flags.StringVar(&terminalShell, "shell", "", "local default shell for this healthcheck")
	flags.Var(&terminalArgs, "shell-arg", "local default shell argument; repeatable")
	flags.BoolVar(&strict, "strict", false, "return non-zero when an optional capability is unavailable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, usedPath, err := config.Load(configPath)
	if err != nil {
		return err
	}
	origin := applyTerminalOverrides(&cfg, terminalShell, terminalArgs.values, terminalArgs.set)
	if err := cfg.Normalize(); err != nil {
		return err
	}
	terminal := platform.ResolveShell(cfg.Terminal.DefaultShell, cfg.Terminal.ShellArgs, origin)
	report := buildHealthReport(cfg, usedPath, terminal, platform.Current())
	printHealthReport(output, report)
	if report.Failures > 0 || strict && report.Warnings > 0 {
		return fmt.Errorf("healthcheck found %d failure(s) and %d warning(s)", report.Failures, report.Warnings)
	}
	return nil
}

func buildHealthReport(cfg config.Config, usedPath string, terminal platform.ShellResolution, capabilities platform.Capabilities) healthReport {
	report := healthReport{}
	add := func(check healthCheck) {
		report.Checks = append(report.Checks, check)
		switch check.Level {
		case healthWarn:
			report.Warnings++
		case healthFail:
			report.Failures++
		}
	}

	if executable, err := os.Executable(); err == nil {
		add(healthCheck{Level: healthOK, Name: "core/binary", Detail: executable + " · " + currentBuildInfo().HealthSummary()})
	} else {
		add(healthCheck{Level: healthFail, Name: "core/binary", Detail: err.Error()})
	}
	add(healthCheck{Level: healthOK, Name: "core/config", Detail: usedPath})
	add(healthCheck{Level: healthOK, Name: "platform/system", Detail: capabilities.Platform()})
	if terminal.Error != "" {
		add(healthCheck{Level: healthWarn, Name: "terminal/default-shell", Detail: terminal.Error, Hint: "Fix [terminal].default_shell or use --shell for this run; SSH inventory remains available."})
	} else {
		add(healthCheck{Level: healthOK, Name: "terminal/default-shell", Detail: terminal.Origin + " · " + terminal.CommandText()})
	}
	ptyLevel := healthInfo
	ptyHint := "Preview terminal is disabled; use the full terminal action until a native adapter is available."
	if capabilities.EmbeddedTerminalAvailable {
		ptyLevel, ptyHint = healthOK, ""
	}
	add(healthCheck{Level: ptyLevel, Name: "terminal/embedded", Detail: capabilities.PTYDescription(), Hint: ptyHint})

	ssh := toolcheck.Resolve(cfg.App.SSHBinary)
	if ssh.Error != "" {
		add(healthCheck{Level: healthFail, Name: "core/ssh", Detail: ssh.Error, Hint: "Install an OpenSSH client or set app.ssh_binary to one executable."})
	} else {
		add(healthCheck{Level: healthOK, Name: "core/ssh", Detail: toolDescription(ssh)})
	}

	askPass := resolveAskPass()
	if askPass.Error != "" {
		add(healthCheck{Level: healthWarn, Name: "credentials/askpass", Detail: askPass.Error, Hint: "Key-only SSH remains available; password-bound hosts are disabled."})
	} else {
		detail := askPass.Origin + " · " + askPass.Path
		if askPass.Self {
			detail += " · self-contained second process"
		}
		add(healthCheck{Level: healthOK, Name: "credentials/askpass", Detail: detail})
	}

	if capabilities.CredentialStoreImplemented && capabilities.CredentialStore == "secret-service" {
		addToolCapability(&report, "credentials/secret-service", "secret-tool", healthWarn,
			"Password, key-passphrase, bearer-token and age-identity lookups need Secret Service/KWallet compatibility.")
	} else {
		add(healthCheck{Level: healthWarn, Name: "credentials/" + capabilities.CredentialStore, Detail: "native adapter not implemented", Hint: "Key-only SSH remains available; credential-bound actions fail closed."})
	}
	addToolCapability(&report, "sources/ssh-keygen", "ssh-keygen", healthWarn,
		"Host-key repair and SSHSIG verification/signing are unavailable without it.")
	addToolCapability(&report, "sources/age", "age", healthWarn,
		"Encrypted local and remote inventories are unavailable without it.")
	containerRuntimes := 0
	for _, name := range cfg.App.Containers.Runtimes {
		result := toolcheck.Resolve(name)
		if result.Error != "" {
			add(healthCheck{Level: healthInfo, Name: "containers/" + name, Detail: "missing · this runtime source is hidden"})
			continue
		}
		containerRuntimes++
		add(healthCheck{Level: healthOK, Name: "containers/" + name, Detail: toolDescription(result)})
	}
	if cfg.App.Containers.Enabled && containerRuntimes == 0 {
		add(healthCheck{Level: healthWarn, Name: "containers/discovery", Detail: "enabled but no configured runtime is available", Hint: "Install Docker/Podman or set app.containers.enabled=false."})
	}

	editor := toolcheck.ResolveEditor(cfg.App.EditorPriority)
	if editor.Error != "" {
		add(healthCheck{Level: healthWarn, Name: "editing/editor", Detail: editor.Error, Hint: "Install nvim, vim or nano; navigation and SSH remain available."})
	} else {
		add(healthCheck{Level: healthOK, Name: "editing/editor", Detail: editor.Name + " · " + toolDescription(editor)})
	}
	for _, name := range []string{"nvim", "vim", "nano", "lf", "dtop", "bat"} {
		result := toolcheck.Resolve(name)
		if result.Error != "" {
			add(healthCheck{Level: healthInfo, Name: "optional/" + name, Detail: "missing · related menu action is hidden"})
		} else {
			add(healthCheck{Level: healthOK, Name: "optional/" + name, Detail: toolDescription(result)})
		}
	}

	bundle, err := resolveWorkspaceBundle(cfg.App.WorkspaceBundle, os.Getenv("SSHF_WORKSPACE_BUNDLE"))
	if err != nil {
		add(healthCheck{Level: healthWarn, Name: "workspace/bundle", Detail: err.Error(), Hint: "Local SSH remains available; bundled remote tools are disabled."})
	} else if bundle == "" {
		add(healthCheck{Level: healthInfo, Name: "workspace/bundle", Detail: "not configured · bundled remote tools are disabled"})
	} else {
		add(healthCheck{Level: healthOK, Name: "workspace/bundle", Detail: bundle})
	}
	return report
}

func addToolCapability(report *healthReport, checkName, toolName string, missingLevel healthLevel, hint string) {
	result := toolcheck.Resolve(toolName)
	check := healthCheck{Name: checkName}
	if result.Error != "" {
		check.Level, check.Detail, check.Hint = missingLevel, "missing · "+result.Error, hint
	} else {
		check.Level, check.Detail = healthOK, toolDescription(result)
	}
	report.Checks = append(report.Checks, check)
	if check.Level == healthWarn {
		report.Warnings++
	} else if check.Level == healthFail {
		report.Failures++
	}
}

func toolDescription(result toolcheck.Result) string {
	return string(result.Origin) + " · " + result.Path
}

func printHealthReport(output io.Writer, report healthReport) {
	fmt.Fprintln(output, "SSH Fleet Console healthcheck")
	fmt.Fprintln(output, strings.Repeat("=", 21))
	counts := map[healthLevel]int{}
	for _, check := range report.Checks {
		counts[check.Level]++
		fmt.Fprintf(output, "[%s] %-27s %s\n", check.Level, check.Name, check.Detail)
		if check.Hint != "" {
			fmt.Fprintln(output, "      hint:", check.Hint)
		}
	}
	fmt.Fprintf(output, "Summary: %d ok, %d warning(s), %d failure(s), %d informational\n",
		counts[healthOK], counts[healthWarn], counts[healthFail], counts[healthInfo])
}
