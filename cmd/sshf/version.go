package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/ponomarev-prime/sshfleet-console/internal/buildinfo"
)

func currentBuildInfo() buildinfo.Info {
	return buildinfo.New(version, buildChannel, buildBranch, buildCommit, buildDate, buildDirty)
}

func runVersionCommand(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(output)
	jsonOutput := false
	flags.BoolVar(&jsonOutput, "json", false, "print machine-readable build provenance")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("version takes no positional arguments")
	}
	info := currentBuildInfo()
	if jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(info)
	}
	fmt.Fprintln(output, info.OneLine())
	fmt.Fprintln(output, "  commit:     ", info.Commit)
	fmt.Fprintln(output, "  branch:     ", info.Branch)
	fmt.Fprintln(output, "  channel:    ", info.Channel)
	fmt.Fprintln(output, "  source date:", info.SourceDate)
	fmt.Fprintln(output, "  source:     ", info.SourceState)
	fmt.Fprintln(output, "  go:         ", info.GoVersion)
	fmt.Fprintln(output, "  platform:   ", info.Platform)
	return nil
}
