package connect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerPIDs(t *testing.T) {
	dir := t.TempDir()

	// Write master PID file.
	pidFile := filepath.Join(dir, "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("100\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fake /proc entries:
	// PID 101: PPid=100 (nginx worker)
	// PID 102: PPid=100 (nginx worker)
	// PID 103: PPid=1   (unrelated process)
	// PID 100: PPid=1   (nginx master itself, not a worker)
	procs := map[string]string{
		"100": "PPid:\t1\n",
		"101": "PPid:\t100\n",
		"102": "PPid:\t100\n",
		"103": "PPid:\t1\n",
	}
	for pid, status := range procs {
		d := filepath.Join(dir, pid)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "status"), []byte(status), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pids, err := workerPIDs(dir, pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 2 {
		t.Fatalf("got %d pids, want 2: %v", len(pids), pids)
	}
	pidSet := make(map[uint32]bool)
	for _, p := range pids {
		pidSet[p] = true
	}
	if !pidSet[101] || !pidSet[102] {
		t.Errorf("expected pids 101 and 102, got %v", pids)
	}
}

func TestWorkerPIDs_NoPIDFile(t *testing.T) {
	dir := t.TempDir()
	_, err := workerPIDs(dir, filepath.Join(dir, "nonexistent.pid"))
	if err == nil {
		t.Error("expected error for missing pid file")
	}
}

func TestWorkerPIDs_InvalidPIDFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := workerPIDs(dir, pidFile)
	if err == nil {
		t.Error("expected error for invalid pid")
	}
}

func TestWorkerPIDs_NoWorkers(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nginx.pid")
	if err := os.WriteFile(pidFile, []byte("100\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Only master in /proc, no children.
	d := filepath.Join(dir, "100")
	os.MkdirAll(d, 0755)
	os.WriteFile(filepath.Join(d, "status"), []byte("PPid:\t1\n"), 0644)

	pids, err := workerPIDs(dir, pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 0 {
		t.Errorf("expected 0 pids, got %v", pids)
	}
}

func TestWorkerPIDs_SkipsPIDDirWithNoStatusFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nginx.pid")
	os.WriteFile(pidFile, []byte("100\n"), 0644)

	// PID dir exists but has no status file.
	os.MkdirAll(filepath.Join(dir, "101"), 0755)

	pids, err := workerPIDs(dir, pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 0 {
		t.Errorf("expected 0 pids, got %v", pids)
	}
}

func TestWorkerPIDs_BadProcRoot(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nginx.pid")
	os.WriteFile(pidFile, []byte("100\n"), 0644)

	_, err := workerPIDs(filepath.Join(dir, "nonexistent"), pidFile)
	if err == nil {
		t.Error("expected error for non-existent proc root")
	}
}

func TestWorkerPIDs_SkipsNonPIDDirs(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nginx.pid")
	os.WriteFile(pidFile, []byte("100\n"), 0644)

	// Non-numeric dir (should be skipped).
	os.MkdirAll(filepath.Join(dir, "self"), 0755)
	// Valid worker.
	d := filepath.Join(dir, "101")
	os.MkdirAll(d, 0755)
	os.WriteFile(filepath.Join(d, "status"), []byte("PPid:\t100\n"), 0644)

	pids, err := workerPIDs(dir, pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 1 || pids[0] != 101 {
		t.Errorf("got pids %v, want [101]", pids)
	}
}
