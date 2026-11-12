// Package runlog writes private, bounded diagnostics without inventory or raw output.
package runlog

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// These are documented product defaults, not limits used to judge QA runs.
const MaxBytes = 4 << 20
const KeepCompleted = 10

// Event deliberately has no arbitrary attributes, command, error text or address.
type Event struct {
	PID         int       `json:"pid"`
	Schema      int       `json:"schema"`
	Time        time.Time `json:"time"`
	Run         string    `json:"run"`
	Event       string    `json:"event"`
	ElapsedMS   int64     `json:"elapsed_ms"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	Target      string    `json:"target_ref,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Status      string    `json:"status,omitempty"`
	Count       int       `json:"count,omitempty"`
	Total       int       `json:"total,omitempty"`
	Active      int       `json:"active,omitempty"`
	Queued      int       `json:"queued,omitempty"`
	Version     string    `json:"version,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	Probes      *bool     `json:"probes_enabled,omitempty"`
	CPUReady    *bool     `json:"cpu_ready,omitempty"`
	MemoryReady *bool     `json:"memory_ready,omitempty"`
}

type Logger struct {
	mu       sync.Mutex
	file     *os.File
	path, id string
	started  time.Time
	size     int64
	failed   bool
}

var namePattern = regexp.MustCompile(`^run-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[a-f0-9]{8}\.jsonl$`)

func DefaultDir() (string, error) {
	if base := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "sshfleet", "logs"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "sshfleet", "logs"), nil
}

func Open(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("log directory must be private and not a symlink")
	}
	var nonce [4]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	now := time.Now()
	id := now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(nonce[:])
	path := filepath.Join(dir, "run-"+id+".jsonl")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{file: file, path: path, id: id, started: now}, nil
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}
func (l *Logger) Ref(identity string) string {
	if l == nil {
		return ""
	}
	hash := sha256.Sum256([]byte(l.id + ":" + identity))
	return hex.EncodeToString(hash[:6])
}
func (l *Logger) Record(event Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil || l.failed {
		return
	}
	l.write(event)
}
func (l *Logger) write(event Event) {
	event.Schema = 1
	event.PID = os.Getpid()
	event.Time = time.Now().UTC()
	event.Run = l.id
	event.ElapsedMS = time.Since(l.started).Milliseconds()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	data = append(data, '\n')
	if l.size+int64(len(data)) > MaxBytes {
		l.file.Close()
		previous := l.path + ".1"
		if info, err := os.Lstat(previous); err == nil {
			if !info.Mode().IsRegular() {
				l.failed = true
				return
			}
			if err = os.Remove(previous); err != nil {
				l.failed = true
				return
			}
		}
		if err = os.Rename(l.path, previous); err != nil {
			l.failed = true
			return
		}
		l.file, err = os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			l.failed = true
			return
		}
		l.size = 0
	}
	n, err := l.file.Write(data)
	l.size += int64(n)
	if err != nil {
		l.failed = true
	}
}

func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if !l.failed {
		l.write(Event{Event: "run.end", Status: "closed"})
	}
	l.file.Close()
	l.file = nil
	// Only completed runs are pruned. Concurrent/aborted sessions stay intact.
	marker, err := os.OpenFile(l.path+".done", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		marker.Close()
	}
	entries, _ := os.ReadDir(filepath.Dir(l.path))
	var completed []string
	for _, entry := range entries {
		if !namePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(filepath.Dir(l.path), entry.Name())
		if info, err := os.Lstat(path + ".done"); err == nil && info.Mode().IsRegular() {
			completed = append(completed, path)
		}
	}
	sort.Strings(completed)
	for len(completed) > KeepCompleted {
		old := completed[0]
		completed = completed[1:]
		for _, path := range []string{old, old + ".1", old + ".done"} {
			if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
				_ = os.Remove(path)
			}
		}
	}
}

func Latest(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && namePattern.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no launch logs yet")
	}
	sort.Strings(names)
	return filepath.Join(dir, names[len(names)-1]), nil
}
func Tail(path string, lines int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return nil, fmt.Errorf("invalid log file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	if len(rows) > lines {
		rows = rows[len(rows)-lines:]
	}
	return append(bytes.Join(rows, []byte("\n")), '\n'), nil
}
