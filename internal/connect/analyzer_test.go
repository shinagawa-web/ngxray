package connect

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func encodeSummaries(t *testing.T, summaries ...Summary) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, s := range summaries {
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

func makeSummary(upstream string, count int, p99Ms float64, retransmits, failed int, ts time.Time) Summary {
	return Summary{
		Ts:          ts,
		Event:       "connect_summary",
		Upstream:    upstream,
		WindowSec:   60,
		Count:       count,
		P50Ms:       p99Ms / 4,
		P95Ms:       p99Ms / 2,
		P99Ms:       p99Ms,
		Retransmits: retransmits,
		Failed:      failed,
	}
}

func TestAnalyze_Basic(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 100, 2.0, 0, 0, t0),
	)
	var out bytes.Buffer
	skipped, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped: got %d, want 0", skipped)
	}
	result := out.String()
	if !strings.Contains(result, "10.0.0.1:8080") {
		t.Errorf("expected upstream in output:\n%s", result)
	}
}

func TestAnalyze_Warning(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 50, 150.0, 3, 1, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "check upstream") {
		t.Errorf("expected warning for high p99 + retransmits:\n%s", out.String())
	}
}

func TestAnalyze_NoWarning_LowP99(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 50, 10.0, 5, 0, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "check upstream") {
		t.Errorf("unexpected warning for low p99:\n%s", out.String())
	}
}

func TestAnalyze_NoWarning_NoRetransmits(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 50, 200.0, 0, 0, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "check upstream") {
		t.Errorf("unexpected warning with no retransmits:\n%s", out.String())
	}
}

func TestAnalyze_CutoffFilter(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	cutoff := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 100, 5.0, 0, 0, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(buf, cutoff, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() > 0 {
		t.Errorf("expected no output when all records before cutoff:\n%s", out.String())
	}
}

func TestAnalyze_SkipCorruptLines(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	buf.WriteString("not json\n")
	json.NewEncoder(&buf).Encode(makeSummary("10.0.0.1:8080", 10, 1.0, 0, 0, t0))
	buf.WriteString("{broken\n")

	var out bytes.Buffer
	skipped, err := Analyze(&buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Errorf("skipped: got %d, want 2", skipped)
	}
	if !strings.Contains(out.String(), "10.0.0.1:8080") {
		t.Errorf("valid summary should still appear:\n%s", out.String())
	}
}

func TestAnalyze_SkipWrongEventType(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	s := makeSummary("10.0.0.1:8080", 10, 1.0, 0, 0, t0)
	s.Event = "workers_snapshot"
	buf := encodeSummaries(t, s)

	var out bytes.Buffer
	skipped, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("wrong event type should be skipped: skipped=%d", skipped)
	}
}

func TestAnalyze_MultipleUpstreams(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 100, 2.0, 0, 0, t0),
		makeSummary("10.0.0.2:9090", 50, 5.0, 2, 0, t0),
	)
	var out bytes.Buffer
	_, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if !strings.Contains(result, "10.0.0.1:8080") || !strings.Contains(result, "10.0.0.2:9090") {
		t.Errorf("both upstreams should appear:\n%s", result)
	}
}

func TestAnalyze_AggregatesAcrossWindows(t *testing.T) {
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	buf := encodeSummaries(t,
		makeSummary("10.0.0.1:8080", 100, 2.0, 1, 0, t0),
		makeSummary("10.0.0.1:8080", 200, 10.0, 2, 1, t1),
	)
	var out bytes.Buffer
	_, err := Analyze(buf, time.Time{}, &out)
	if err != nil {
		t.Fatal(err)
	}
	result := out.String()
	// total count should be 300 (100+200)
	if !strings.Contains(result, "300") {
		t.Errorf("expected aggregated count 300 in output:\n%s", result)
	}
}

type errReader struct{ err error }

func (r *errReader) Read(p []byte) (int, error) { return 0, r.err }

func TestAnalyze_ScannerError(t *testing.T) {
	r := &errReader{err: errors.New("read failed")}
	_, err := Analyze(r, time.Time{}, io.Discard)
	if err == nil {
		t.Error("expected scanner error to be propagated")
	}
}
