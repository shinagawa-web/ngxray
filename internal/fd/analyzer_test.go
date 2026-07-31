package fd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func makeSnapshot(pid, fdCount, fdLimit int, ts time.Time) Snapshot {
	return Snapshot{
		Ts:        ts,
		Event:     "fd_snapshot",
		WorkerPID: pid,
		FDCount:   fdCount,
		FDLimit:   fdLimit,
		Pct:       float64(fdCount) / float64(fdLimit) * 100,
	}
}

func encodeSnapshots(t *testing.T, snaps ...Snapshot) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, s := range snaps {
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

func TestAnalyze_Basic(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	input := encodeSnapshots(t,
		makeSnapshot(101, 512, 1024, t0),
	)
	var out bytes.Buffer
	skipped, err := Analyze(input, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped: got %d, want 0", skipped)
	}
	result := out.String()
	if !strings.Contains(result, "worker 101") {
		t.Errorf("expected worker 101 in output:\n%s", result)
	}
	if !strings.Contains(result, "50%") {
		t.Errorf("expected 50%% utilization in output:\n%s", result)
	}
}

func TestAnalyze_GrowthRate(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	// 100 FDs at t0, 160 FDs 60 minutes later → +1 FD/min
	input := encodeSnapshots(t,
		makeSnapshot(101, 100, 1000, t0),
		makeSnapshot(101, 160, 1000, t0.Add(60*time.Minute)),
	)
	var out bytes.Buffer
	_, err := Analyze(input, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if !strings.Contains(result, "FDs/min") {
		t.Errorf("expected growth rate in output:\n%s", result)
	}
	if !strings.Contains(result, "projected exhaustion") {
		t.Errorf("expected exhaustion projection in output:\n%s", result)
	}
}

func TestAnalyze_NoGrowth(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	input := encodeSnapshots(t,
		makeSnapshot(101, 100, 1000, t0),
		makeSnapshot(101, 100, 1000, t0.Add(30*time.Minute)),
	)
	var out bytes.Buffer
	_, err := Analyze(input, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if strings.Contains(result, "FDs/min") {
		t.Errorf("expected no growth rate when stable:\n%s", result)
	}
	if strings.Contains(result, "projected exhaustion") {
		t.Errorf("expected no exhaustion projection when stable:\n%s", result)
	}
}

func TestAnalyze_CutoffFilter(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	// snapshot before cutoff should be excluded
	input := encodeSnapshots(t,
		makeSnapshot(101, 900, 1000, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(input, cutoff, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() > 0 {
		t.Errorf("expected no output when all snapshots before cutoff, got:\n%s", out.String())
	}
}

func TestAnalyze_MultipleWorkers(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	input := encodeSnapshots(t,
		makeSnapshot(101, 200, 1000, t0),
		makeSnapshot(102, 800, 1000, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(input, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if !strings.Contains(result, "worker 101") {
		t.Errorf("expected worker 101 in output:\n%s", result)
	}
	if !strings.Contains(result, "worker 102") {
		t.Errorf("expected worker 102 in output:\n%s", result)
	}
}

func TestAnalyze_ClientSocketGuidance(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	snap := Snapshot{
		Ts:            t0,
		Event:         "fd_snapshot",
		WorkerPID:     101,
		FDCount:       100,
		FDLimit:       1000,
		Pct:           10,
		ClientSockets: 80, // 80% of FDs
		Other:         20,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(snap); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := Analyze(&buf, time.Time{}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "client sockets dominant") {
		t.Errorf("expected client socket guidance:\n%s", out.String())
	}
}

func TestAnalyze_SkipCorruptLines(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	buf.WriteString("this is not json\n")
	if err := json.NewEncoder(&buf).Encode(makeSnapshot(101, 100, 1000, t0)); err != nil {
		t.Fatal(err)
	}
	buf.WriteString("{broken\n")

	var out bytes.Buffer
	skipped, err := Analyze(&buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Errorf("skipped: got %d, want 2", skipped)
	}
	if !strings.Contains(out.String(), "worker 101") {
		t.Errorf("expected valid snapshot to still be reported:\n%s", out.String())
	}
}
