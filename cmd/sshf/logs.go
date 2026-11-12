package main

import (
	"flag"
	"fmt"
	"github.com/ponomarev-prime/sshfleet-console/internal/runlog"
	"io"
	"os"
)

var launchLog *runlog.Logger

func startLaunchLog(dir string, disabled bool) *runlog.Logger {
	if disabled {
		return nil
	}
	var err error
	if dir == "" {
		dir, err = runlog.DefaultDir()
	}
	if err == nil {
		launchLog, err = runlog.Open(dir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "SSH Fleet run logging unavailable:", err)
		return nil
	}
	info := currentBuildInfo()
	launchLog.Record(runlog.Event{Event: "run.start", Version: info.Version, Commit: info.Commit})
	return launchLog
}

func runLogsCommand(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(out)
	dir := flags.String("dir", "", "launch log directory (default: XDG state directory)")
	pathOnly := flags.Bool("path", false, "print latest log path only")
	lines := flags.Int("lines", 100, "number of recent JSONL records")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *lines < 1 {
		return fmt.Errorf("logs takes no positional arguments and requires positive --lines")
	}
	var err error
	if *dir == "" {
		*dir, err = runlog.DefaultDir()
		if err != nil {
			return err
		}
	}
	path, err := runlog.Latest(*dir)
	if err != nil {
		return err
	}
	if *pathOnly {
		fmt.Fprintln(out, path)
		return nil
	}
	data, err := runlog.Tail(path, *lines)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
