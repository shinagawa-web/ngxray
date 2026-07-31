package workers

import (
	"bytes"
	"encoding/json"
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
