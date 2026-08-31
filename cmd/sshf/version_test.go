package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/buildinfo"
)

func TestVersionCommandPrintsHumanAndJSONProvenance(t *testing.T) {
	old := []string{version, buildChannel, buildBranch, buildCommit, buildDate, buildDirty}
	version, buildChannel, buildBranch = "v0.1.0", "stable", "main"
	buildCommit, buildDate, buildDirty = "0123456789abcdef", "2026-08-30T11:35:16Z", "false"
	t.Cleanup(func() {
		version, buildChannel, buildBranch, buildCommit, buildDate, buildDirty = old[0], old[1], old[2], old[3], old[4], old[5]
	})

	var output bytes.Buffer
	if err := runVersionCommand(nil, &output); err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"sshfleet v0.1.0", "commit:", "0123456789abcdef", "channel:", "stable"} {
		if !strings.Contains(output.String(), wanted) {
			t.Fatalf("human version missing %q:\n%s", wanted, output.String())
		}
	}

	output.Reset()
	if err := runVersionCommand([]string{"--json"}, &output); err != nil {
		t.Fatal(err)
	}
	var info buildinfo.Info
	if err := json.Unmarshal(output.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version != "v0.1.0" || info.Commit != "0123456789abcdef" || info.Branch != "main" || info.Dirty || info.SourceState != "clean" {
		t.Fatalf("json info = %#v", info)
	}
}

func TestVersionCommandRejectsUnknownFlagsAndArguments(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"unexpected"}, {"--json", "unexpected"}} {
		var output bytes.Buffer
		if err := runVersionCommand(args, &output); err == nil {
			t.Fatalf("runVersionCommand(%q) unexpectedly succeeded", args)
		}
	}
}

func TestCurrentBuildInfoPreservesUnknownAdHocBuildState(t *testing.T) {
	old := []string{version, buildChannel, buildBranch, buildCommit, buildDate, buildDirty}
	version, buildChannel, buildBranch = "", "", ""
	buildCommit, buildDate, buildDirty = "", "", "unknown"
	t.Cleanup(func() {
		version, buildChannel, buildBranch, buildCommit, buildDate, buildDirty = old[0], old[1], old[2], old[3], old[4], old[5]
	})

	info := currentBuildInfo()
	if info.Version != "unknown" || info.Commit != "unknown" || info.SourceState != "unknown" || info.Dirty {
		t.Fatalf("ad-hoc build info = %#v", info)
	}
}
