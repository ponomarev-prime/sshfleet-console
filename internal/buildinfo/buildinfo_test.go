package buildinfo

import (
	"strings"
	"testing"
)

func TestInfoFormatsDevelopmentAndReleaseProvenance(t *testing.T) {
	development := New("dev-b941263+dirty", "development", "dev", "b941263123456789", "2026-08-30T11:35:16Z", "true")
	for _, wanted := range []string{"sshfleet dev-b941263+dirty", "development", "b94126312345@dev", "dirty"} {
		if !strings.Contains(development.OneLine(), wanted) {
			t.Fatalf("development line missing %q: %s", wanted, development.OneLine())
		}
	}
	release := New("v0.1.0", "stable", "main", "0123456789abcdef", "2026-08-30T11:35:16Z", "false")
	if got := release.HealthSummary(); got != "v0.1.0 · 0123456789ab@main · stable" {
		t.Fatalf("release summary = %q", got)
	}
}

func TestNewNormalizesMissingValuesAndDirtyState(t *testing.T) {
	tests := []struct {
		name      string
		dirty     string
		wantDirty bool
		wantState string
	}{
		{name: "clean", dirty: "false", wantState: "clean"},
		{name: "dirty", dirty: "true", wantDirty: true, wantState: "dirty"},
		{name: "unknown", dirty: "unknown", wantState: "unknown"},
		{name: "empty", dirty: "", wantState: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := New(" ", "", "", "", "", test.dirty)
			if info.Version != "unknown" || info.Channel != "unknown" || info.Branch != "unknown" || info.Commit != "unknown" || info.SourceDate != "unknown" {
				t.Fatalf("missing fields were not normalized: %#v", info)
			}
			if info.Dirty != test.wantDirty || info.SourceState != test.wantState {
				t.Fatalf("dirty=%q produced Dirty=%v SourceState=%q", test.dirty, info.Dirty, info.SourceState)
			}
			if info.GoVersion == "" || !strings.Contains(info.Platform, "/") {
				t.Fatalf("runtime provenance missing: %#v", info)
			}
		})
	}
}

func TestInfoFormattingHandlesShortAndUnknownCommits(t *testing.T) {
	short := New("dev-test", "development", "dev", "abc123", "2026-08-30T00:00:00Z", "false")
	if got := short.HealthSummary(); got != "dev-test · abc123@dev · development" {
		t.Fatalf("short commit summary = %q", got)
	}

	unknown := New("", "", "", "", "", "invalid")
	if got := unknown.OneLine(); got != "sshfleet unknown (unknown, unknown@unknown, unknown)" {
		t.Fatalf("unknown one-line version = %q", got)
	}
}
