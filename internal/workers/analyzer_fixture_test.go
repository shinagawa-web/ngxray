package workers

import (
	"bytes"
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
	if err := Analyze(f, cutoff, &out); err != nil {
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
