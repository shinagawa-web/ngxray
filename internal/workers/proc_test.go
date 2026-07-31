package workers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildProcFixture creates a minimal fake /proc directory for testing.
func buildProcFixture(t *testing.T, masterPID int, workerPIDs []int, bootTimeSec int64, startTicks map[int]uint64) string {
	t.Helper()
	root := t.TempDir()

	statContent := fmt.Sprintf("cpu  0 0 0 0 0 0 0 0 0 0\nbtime %d\n", bootTimeSec)
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte(statContent), 0644); err != nil {
		t.Fatal(err)
	}

	writeStat(t, root, masterPID, 1, startTicks[masterPID])
	for _, wpid := range workerPIDs {
		writeStat(t, root, wpid, masterPID, startTicks[wpid])
	}
	// Unrelated process; must not appear in results
	writeStat(t, root, 9999, 1, 100)

	return root
}

// writeStat writes a minimal /proc/[pid]/stat file.
// Fields after ')': state ppid [17 zeros] starttime
// Indexes: 0=state 1=ppid 2..18=zeros 19=starttime
func writeStat(t *testing.T, root string, pid, ppid int, ticks uint64) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d (nginx) S %d", pid, ppid)
	for i := 0; i < 17; i++ { // pgrp..itrealvalue (fields 2-18)
		sb.WriteString(" 0")
	}
	fmt.Fprintf(&sb, " %d\n", ticks) // starttime at index 19
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadStatCommWithParens(t *testing.T) {
	// comm can contain spaces and '(' and ')'; LastIndex(")") must anchor correctly.
	root := t.TempDir()
	dir := filepath.Join(root, "42")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Tricky comm: "(ng )x)" — contains parens and space
	var sb strings.Builder
	fmt.Fprintf(&sb, "42 ((ng )x)) S 7")
	for i := 0; i < 17; i++ {
		sb.WriteString(" 0")
	}
	fmt.Fprintf(&sb, " 500\n")
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	ppid, startTicks, err := readStat(root, 42)
	if err != nil {
		t.Fatalf("readStat: %v", err)
	}
	if ppid != 7 {
		t.Errorf("ppid: got %d, want 7", ppid)
	}
	if startTicks != 500 {
		t.Errorf("startTicks: got %d, want 500", startTicks)
	}
}

func TestEnumerateWorkers(t *testing.T) {
	bootSec := int64(1_000_000)
	bootTime := time.Unix(bootSec, 0)
	ticks := map[int]uint64{
		1000: 100,
		1001: 200, // 2s after boot → started_at = bootSec+2
		1002: 300, // 3s after boot → started_at = bootSec+3
	}
	procRoot := buildProcFixture(t, 1000, []int{1001, 1002}, bootSec, ticks)

	workers, err := EnumerateWorkers(procRoot, 1000, bootTime, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(workers))
	}

	byPID := map[int]Worker{}
	for _, w := range workers {
		byPID[w.PID] = w
	}

	want1001 := time.Unix(bootSec+2, 0).UTC()
	want1002 := time.Unix(bootSec+3, 0).UTC()
	if got := byPID[1001].StartedAt; !got.Equal(want1001) {
		t.Errorf("pid 1001 started_at: got %v, want %v", got, want1001)
	}
	if got := byPID[1002].StartedAt; !got.Equal(want1002) {
		t.Errorf("pid 1002 started_at: got %v, want %v", got, want1002)
	}
}

func TestCollectorOutput(t *testing.T) {
	bootSec := int64(2_000_000)
	ticks := map[int]uint64{2000: 50, 2001: 6000}
	procRoot := buildProcFixture(t, 2000, []int{2001}, bootSec, ticks)

	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("2000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: &buf}
	if err := c.Collect(); err != nil {
		t.Fatal(err)
	}

	var snap Snapshot
	if err := json.Unmarshal(buf.Bytes(), &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v\nraw: %s", err, buf.String())
	}
	if snap.Event != "workers_snapshot" {
		t.Errorf("event: got %q, want %q", snap.Event, "workers_snapshot")
	}
	if len(snap.Workers) != 1 || snap.Workers[0].PID != 2001 {
		t.Errorf("unexpected workers: %+v", snap.Workers)
	}
}

func TestAnalyzeReload(t *testing.T) {
	boot := time.Unix(1_000_000, 0).UTC()
	// Snapshot 1: two workers running
	s1 := Snapshot{
		Ts:    boot.Add(10 * time.Minute),
		Event: "workers_snapshot",
		Workers: []Worker{
			{PID: 101, StartedAt: boot.Add(1 * time.Minute)},
			{PID: 102, StartedAt: boot.Add(1 * time.Minute)},
		},
	}
	// Snapshot 2: reload — new pids 103,104; old pid 101 still alive
	s2 := Snapshot{
		Ts:    boot.Add(20 * time.Minute),
		Event: "workers_snapshot",
		Workers: []Worker{
			{PID: 101, StartedAt: boot.Add(1 * time.Minute)},
			{PID: 103, StartedAt: boot.Add(20 * time.Minute)},
			{PID: 104, StartedAt: boot.Add(20 * time.Minute)},
		},
	}
	// Snapshot 3: old worker 101 finally gone
	s3 := Snapshot{
		Ts:    boot.Add(30 * time.Minute),
		Event: "workers_snapshot",
		Workers: []Worker{
			{PID: 103, StartedAt: boot.Add(20 * time.Minute)},
			{PID: 104, StartedAt: boot.Add(20 * time.Minute)},
		},
	}

	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	enc.Encode(s1)
	enc.Encode(s2)
	enc.Encode(s3)

	var out bytes.Buffer
	if _, err := Analyze(&input, time.Time{}, &out); err != nil {
		t.Fatal(err)
	}
	result := out.String()
	if !strings.Contains(result, "reload detected") {
		t.Errorf("expected reload detection, got:\n%s", result)
	}
	if !strings.Contains(result, "pid:101") {
		t.Errorf("expected old worker pid:101 reported, got:\n%s", result)
	}
	if !strings.Contains(result, "all old workers gone") || !strings.Contains(result, "drain took") {
		t.Errorf("expected drain confirmation, got:\n%s", result)
	}
}

// --- ReadBootTime error paths ---

func TestReadBootTime_FileNotFound(t *testing.T) {
	_, err := ReadBootTime("/nonexistent/proc")
	if err == nil {
		t.Error("expected error for missing stat file")
	}
}

func TestReadBootTime_BadBtime(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "stat"), []byte("btime notanumber\n"), 0644)
	_, err := ReadBootTime(root)
	if err == nil {
		t.Error("expected error for non-numeric btime")
	}
}

func TestReadBootTime_NoBtimeLine(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "stat"), []byte("cpu 0 0 0 0\n"), 0644)
	_, err := ReadBootTime(root)
	if err == nil {
		t.Error("expected error when btime absent")
	}
}

// --- ReadMasterPID error paths ---

func TestReadMasterPID_FileNotFound(t *testing.T) {
	_, err := ReadMasterPID("/nonexistent/nginx.pid")
	if err == nil {
		t.Error("expected error for missing pid file")
	}
}

func TestReadMasterPID_BadContent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(f, []byte("notanumber\n"), 0644)
	_, err := ReadMasterPID(f)
	if err == nil {
		t.Error("expected error for non-numeric pid content")
	}
}

// --- EnumerateWorkers error paths ---

func TestEnumerateWorkers_ReadDirError(t *testing.T) {
	_, err := EnumerateWorkers("/nonexistent/proc", 1, time.Now(), 10*time.Millisecond)
	if err == nil {
		t.Error("expected error for missing procRoot")
	}
}

func TestEnumerateWorkers_NonNumericDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "notapid"), 0755)
	bootSec := int64(7_000_000)
	bootTime := time.Unix(bootSec, 0)
	writeStat(t, root, 7000, 1, 50)
	writeStat(t, root, 7001, 7000, 100)

	ws, err := EnumerateWorkers(root, 7000, bootTime, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].PID != 7001 {
		t.Errorf("expected only worker 7001, got %+v", ws)
	}
}

func TestEnumerateWorkers_StatReadError(t *testing.T) {
	root := t.TempDir()
	// pid dir exists but no stat file → readStat returns error → skip
	os.MkdirAll(filepath.Join(root, "8001"), 0755)
	ws, err := EnumerateWorkers(root, 8000, time.Now(), 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 0 {
		t.Errorf("expected no workers, got %+v", ws)
	}
}

// --- readStat error paths ---

func TestReadStat_MissingFile(t *testing.T) {
	_, _, err := readStat(t.TempDir(), 9999)
	if err == nil {
		t.Error("expected error for missing stat file")
	}
}

func TestReadStat_NoCloseParen(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "42")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "stat"), []byte("42 (nginx S 1\n"), 0644)
	_, _, err := readStat(root, 42)
	if err == nil {
		t.Error("expected error for stat with no closing paren")
	}
}

func TestReadStat_TooFewFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "43")
	os.MkdirAll(dir, 0755)
	// Only state+ppid after ')': 2 fields, need ≥20
	os.WriteFile(filepath.Join(dir, "stat"), []byte("43 (nginx) S 1\n"), 0644)
	_, _, err := readStat(root, 43)
	if err == nil {
		t.Error("expected error for too few stat fields")
	}
}

func TestReadStat_BadPPID(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "44")
	os.MkdirAll(dir, 0755)
	var sb strings.Builder
	fmt.Fprintf(&sb, "44 (nginx) S notanumber")
	for i := 0; i < 18; i++ {
		sb.WriteString(" 0")
	}
	sb.WriteString(" 500\n")
	os.WriteFile(filepath.Join(dir, "stat"), []byte(sb.String()), 0644)
	_, _, err := readStat(root, 44)
	if err == nil {
		t.Error("expected error for bad ppid")
	}
}

func TestReadStat_BadStarttime(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "45")
	os.MkdirAll(dir, 0755)
	var sb strings.Builder
	fmt.Fprintf(&sb, "45 (nginx) S 1")
	for i := 0; i < 17; i++ {
		sb.WriteString(" 0")
	}
	sb.WriteString(" notanumber\n")
	os.WriteFile(filepath.Join(dir, "stat"), []byte(sb.String()), 0644)
	_, _, err := readStat(root, 45)
	if err == nil {
		t.Error("expected error for bad starttime")
	}
}

// --- workers.Collector error paths ---

type failWriter struct{ err error }

func (fw *failWriter) Write(p []byte) (int, error) { return 0, fw.err }

func TestWorkerCollect_MasterPIDError(t *testing.T) {
	c := &Collector{ProcRoot: t.TempDir(), PIDFile: "/nonexistent.pid"}
	if err := c.Collect(); err == nil {
		t.Error("expected error for missing pid file")
	}
}

func TestWorkerCollect_BootTimeError(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte("1000\n"), 0644)
	c := &Collector{ProcRoot: "/nonexistent/proc", PIDFile: pidFile}
	if err := c.Collect(); err == nil {
		t.Error("expected error for missing procRoot/stat")
	}
}

func TestWorkerCollect_EnumerateWorkersError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test directory permissions as root")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "stat"), []byte("btime 1000000\n"), 0644)
	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte("1000\n"), 0644)

	// 0111 = execute only: ReadBootTime can open known file, ReadDir cannot list
	if err := os.Chmod(root, 0111); err != nil {
		t.Skip("cannot chmod test directory")
	}
	defer os.Chmod(root, 0755)

	c := &Collector{ProcRoot: root, PIDFile: pidFile}
	if err := c.Collect(); err == nil {
		t.Error("expected error for unlistable procRoot")
	}
}

func TestWorkerCollect_EncodeError(t *testing.T) {
	bootSec := int64(9_000_000)
	ticks := map[int]uint64{9000: 10, 9001: 20}
	procRoot := buildProcFixture(t, 9000, []int{9001}, bootSec, ticks)
	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte("9000\n"), 0644)

	c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: &failWriter{err: errors.New("disk full")}}
	if err := c.Collect(); err == nil {
		t.Error("expected error for failing writer")
	}
}

func TestReadBootTime_ScannerError(t *testing.T) {
	root := t.TempDir()
	// A line longer than bufio.MaxScanTokenSize (64KB) triggers scanner.Err() = bufio.ErrTooLong
	longLine := strings.Repeat("x", 64*1024+1) + "\n"
	os.WriteFile(filepath.Join(root, "stat"), []byte(longLine), 0644)
	_, err := ReadBootTime(root)
	if err == nil {
		t.Error("expected scanner error for oversized line")
	}
}
