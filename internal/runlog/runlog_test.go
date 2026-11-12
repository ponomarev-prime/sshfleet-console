package runlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateRunLifecycleAndRotation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Record(Event{Event: "run.start", Version: "lab-fixture"})
	ref := l.Ref("private:user@192.0.2.20")
	if strings.Contains(ref, "private") || len(ref) != 12 {
		t.Fatal(ref)
	}
	l.Record(Event{Event: "probe.end", Target: ref, Status: "online"})
	for path, want := range map[string]os.FileMode{dir: 0o700, l.Path(): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("permissions: %s %v", path, err)
		}
	}
	l.size = MaxBytes
	l.Record(Event{Event: "resolve.end", Target: ref})
	l.Close()
	if _, err := os.Stat(l.Path() + ".1"); err != nil {
		t.Fatal("rotation missing", err)
	}
	path, err := Latest(dir)
	if err != nil || path != l.Path() {
		t.Fatal(path, err)
	}
	data, err := Tail(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil || event.Event != "run.end" || event.Run == "" {
		t.Fatalf("last event: %s, %v", data, err)
	}
	previous, _ := os.ReadFile(l.Path() + ".1")
	if strings.Contains(string(previous), "192.0.2.20") || strings.Contains(string(previous), "private:user") {
		t.Fatal("inventory leaked")
	}
}

func TestRetentionPreservesActiveAndUnrelatedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	active, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "personal.jsonl")
	os.WriteFile(unrelated, []byte("keep"), 0o600)
	for i := 0; i < KeepCompleted+2; i++ {
		log, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		log.Close()
	}
	if _, err := os.Stat(active.Path()); err != nil {
		t.Fatal("active log removed")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated file removed")
	}
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".done") {
			count++
		}
	}
	if count != KeepCompleted {
		t.Fatalf("completed runs=%d", count)
	}
	active.Close()
}

func TestRejectsSymlinkLogDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0o700)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("followed log directory symlink")
	}
}
