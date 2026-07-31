package workers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCollectorWritesToFile exercises the full "collect → append to file" path:
// multiple Collect() calls must produce one valid NDJSON line each, appended
// in order, without truncating earlier lines.
func TestCollectorWritesToFile(t *testing.T) {
	bootSec := int64(4_000_000)
	ticks := map[int]uint64{4000: 50, 4001: 100, 4002: 200}
	procRoot := buildProcFixture(t, 4000, []int{4001, 4002}, bootSec, ticks)

	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("4000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "workers.ndjson")

	const runs = 3
	for i := 0; i < runs; i++ {
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatalf("open output file (run %d): %v", i, err)
		}
		c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: f}
		if err := c.Collect(); err != nil {
			f.Close()
			t.Fatalf("Collect (run %d): %v", i, err)
		}
		f.Close()
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var snaps []Snapshot
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var s Snapshot
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			t.Fatalf("unmarshal line: %v\nraw: %s", err, scanner.Text())
		}
		snaps = append(snaps, s)
	}

	if len(snaps) != runs {
		t.Fatalf("expected %d NDJSON lines, got %d", runs, len(snaps))
	}
	for i, s := range snaps {
		if s.Event != "workers_snapshot" {
			t.Errorf("line %d: event = %q, want workers_snapshot", i, s.Event)
		}
		if len(s.Workers) != 2 {
			t.Errorf("line %d: %d workers, want 2", i, len(s.Workers))
		}
		if s.Ts.IsZero() {
			t.Errorf("line %d: zero timestamp", i)
		}
	}
}

// TestCollectorAppendsExistingFile verifies that a pre-existing log file is not
// truncated when a new Collector runs — simulating a restart of ngxray collect.
func TestCollectorAppendsExistingFile(t *testing.T) {
	bootSec := int64(5_000_000)
	ticks := map[int]uint64{5000: 10, 5001: 20}
	procRoot := buildProcFixture(t, 5000, []int{5001}, bootSec, ticks)

	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("5000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "workers.ndjson")

	// Write a sentinel line that must survive
	sentinel := `{"ts":"2000-01-01T00:00:00Z","event":"workers_snapshot","workers":[]}` + "\n"
	if err := os.WriteFile(outPath, []byte(sentinel), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: f}
	if err := c.Collect(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitNDJSON(data)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after append, got %d\n%s", len(lines), data)
	}
	var first Snapshot
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Ts.Year() != 2000 {
		t.Errorf("sentinel overwritten: first line ts = %v", first.Ts)
	}
}

// TestCollectorTimestampsAreUTC verifies that all emitted timestamps use UTC.
func TestCollectorTimestampsAreUTC(t *testing.T) {
	bootSec := int64(6_000_000)
	ticks := map[int]uint64{6000: 10, 6001: 50}
	procRoot := buildProcFixture(t, 6000, []int{6001}, bootSec, ticks)

	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("6000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bufWriter
	c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: &buf}
	if err := c.Collect(); err != nil {
		t.Fatal(err)
	}

	var snap Snapshot
	if err := json.Unmarshal(buf.b, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Ts.Location() != time.UTC {
		t.Errorf("snapshot ts not UTC: %v", snap.Ts.Location())
	}
	for _, w := range snap.Workers {
		if w.StartedAt.Location() != time.UTC {
			t.Errorf("worker pid:%d started_at not UTC: %v", w.PID, w.StartedAt.Location())
		}
	}
}

type bufWriter struct{ b []byte }

func (bw *bufWriter) Write(p []byte) (int, error) {
	bw.b = append(bw.b, p...)
	return len(p), nil
}

func splitNDJSON(data []byte) []string {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			lines = append(lines, scanner.Text())
		}
	}
	return lines
}
