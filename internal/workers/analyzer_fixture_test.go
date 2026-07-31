package workers

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func analyzeFixture(t *testing.T, name string, cutoff time.Time) string {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()
	var out bytes.Buffer
	if _, err := Analyze(f, cutoff, &out); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return out.String()
}

func TestAnalyzeNoReload(t *testing.T) {
	got := analyzeFixture(t, "no_reload.ndjson", time.Time{})
	if got != "" {
		t.Errorf("expected no output for steady state, got:\n%s", got)
	}
}

func TestAnalyzeSingleReload(t *testing.T) {
	got := analyzeFixture(t, "single_reload.ndjson", time.Time{})
	if !strings.Contains(got, "reload detected") {
		t.Errorf("expected reload detection, got:\n%s", got)
	}
	if !strings.Contains(got, "all old workers gone") || !strings.Contains(got, "drain took") {
		t.Errorf("expected drain confirmation, got:\n%s", got)
	}
	// Old workers 100 and 101 should NOT appear — they disappeared immediately
	if strings.Contains(got, "pid:100") || strings.Contains(got, "pid:101") {
		t.Errorf("unexpected old worker mention, got:\n%s", got)
	}
}

func TestAnalyzeLingeringWorkers(t *testing.T) {
	got := analyzeFixture(t, "lingering_workers.ndjson", time.Time{})
	if !strings.Contains(got, "reload detected") {
		t.Errorf("expected reload detection, got:\n%s", got)
	}
	// pid:100 lingers for two snapshots after reload
	occurrences := strings.Count(got, "pid:100")
	if occurrences < 2 {
		t.Errorf("expected pid:100 reported at least twice (lingered 2 snapshots), got %d times\n%s", occurrences, got)
	}
	if !strings.Contains(got, "all old workers gone") || !strings.Contains(got, "drain took") {
		t.Errorf("expected drain confirmation, got:\n%s", got)
	}
}

func TestAnalyzeMultipleReloads(t *testing.T) {
	// Fixture: snap1=[100,101], snap2=[100,102,103] (1st reload; 101 drains, 100 lingers),
	// snap3=[100,102,104,105] (2nd reload; 103 drains, 100 and 102 linger),
	// snap4=[104,105] (all old workers finally drain).
	got := analyzeFixture(t, "multi_reload.ndjson", time.Time{})

	if strings.Count(got, "reload detected") != 2 {
		t.Errorf("expected 2 reload detections, got:\n%s", got)
	}
	// pid:100 lingers through both reloads
	if !strings.Contains(got, "pid:100") {
		t.Errorf("expected pid:100 reported as old worker, got:\n%s", got)
	}
	// pid:102 becomes old after the 2nd reload
	if !strings.Contains(got, "pid:102") {
		t.Errorf("expected pid:102 reported as old worker after 2nd reload, got:\n%s", got)
	}
	if !strings.Contains(got, "all old workers gone") || !strings.Contains(got, "drain took") {
		t.Errorf("expected drain confirmation with elapsed time, got:\n%s", got)
	}
}

func TestAnalyzeCutoffFilter(t *testing.T) {
	// Cutoff at 2026-01-01: the 2025-06-01 snapshot is skipped.
	// With only post-cutoff data the analyser sees one snapshot before the reload
	// (2026-01-01T00:00:00Z) and one after (2026-01-01T00:01:00Z), so a reload
	// is detected and old workers 10/11 are reported.
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(-1)
	got := analyzeFixture(t, "cutoff_filter.ndjson", cutoff)
	if !strings.Contains(got, "reload detected") {
		t.Errorf("expected reload detection in post-cutoff data, got:\n%s", got)
	}
	// Confirm the early 2025 snapshot did not trigger a false detection
	if strings.Count(got, "reload detected") > 1 {
		t.Errorf("expected exactly 1 reload, got:\n%s", got)
	}
}

func TestAnalyze_SkipCorruptLine(t *testing.T) {
	boot := time.Unix(1_000_000, 0).UTC()
	var input bytes.Buffer
	input.WriteString("not json at all\n")
	snap := Snapshot{Ts: boot, Event: "workers_snapshot", Workers: []Worker{{PID: 10, StartedAt: boot}}}
	json.NewEncoder(&input).Encode(snap)

	var out bytes.Buffer
	skipped, err := Analyze(&input, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("skipped: got %d, want 1", skipped)
	}
}

func TestAnalyze_SkipWrongEvent(t *testing.T) {
	boot := time.Unix(1_000_000, 0).UTC()
	var input bytes.Buffer
	// Event type not "workers_snapshot" → skipped via continue (not counted in skipped)
	wrong := Snapshot{Ts: boot, Event: "fd_snapshot", Workers: []Worker{{PID: 10, StartedAt: boot}}}
	json.NewEncoder(&input).Encode(wrong)
	valid := Snapshot{Ts: boot.Add(time.Minute), Event: "workers_snapshot", Workers: []Worker{{PID: 10, StartedAt: boot}}}
	json.NewEncoder(&input).Encode(valid)

	var out bytes.Buffer
	skipped, err := Analyze(&input, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	// Wrong event is not a parse error → skipped stays 0
	if skipped != 0 {
		t.Errorf("skipped: got %d, want 0", skipped)
	}
}
