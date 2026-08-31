package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

// Info is immutable provenance embedded by tools/build-sshf.sh. Defaults keep
// ad-hoc `go build` usable while making an unversioned binary obvious.
type Info struct {
	Version     string `json:"version"`
	Channel     string `json:"channel"`
	Branch      string `json:"branch"`
	Commit      string `json:"commit"`
	SourceDate  string `json:"source_date"`
	Dirty       bool   `json:"dirty"`
	SourceState string `json:"source_state"`
	GoVersion   string `json:"go_version"`
	Platform    string `json:"platform"`
}

func New(version, channel, branch, commit, sourceDate, dirty string) Info {
	state := "unknown"
	if dirty == "true" {
		state = "dirty"
	} else if dirty == "false" {
		state = "clean"
	}
	return Info{
		Version:     valueOrUnknown(version),
		Channel:     valueOrUnknown(channel),
		Branch:      valueOrUnknown(branch),
		Commit:      valueOrUnknown(commit),
		SourceDate:  valueOrUnknown(sourceDate),
		Dirty:       dirty == "true",
		SourceState: state,
		GoVersion:   runtime.Version(),
		Platform:    runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func (i Info) OneLine() string {
	return fmt.Sprintf("sshfleet %s (%s, %s@%s, %s)", i.Version, i.Channel, shortCommit(i.Commit), i.Branch, i.SourceState)
}

func (i Info) HealthSummary() string {
	return fmt.Sprintf("%s · %s@%s · %s", i.Version, shortCommit(i.Commit), i.Branch, i.Channel)
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
