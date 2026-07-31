package fd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeWorkerStatForFD writes a minimal /proc/[pid]/stat compatible with
// workers.EnumerateWorkers (same format as workers.writeStat helper).
func writeWorkerStatForFD(t *testing.T, root string, pid, ppid int, startTicks uint64) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d (nginx) S %d", pid, ppid)
	for i := 0; i < 17; i++ {
		sb.WriteString(" 0")
	}
	fmt.Fprintf(&sb, " %d\n", startTicks)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

// buildFDCollectFixture creates a complete fake /proc for fd.Collector.Collect().
func buildFDCollectFixture(t *testing.T, masterPID, workerPID int) (procRoot, pidFile string) {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "stat"), []byte("cpu 0 0 0\nbtime 1000000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeWorkerStatForFD(t, root, masterPID, 1, 50)
	writeWorkerStatForFD(t, root, workerPID, masterPID, 100)

	writeLimits(t, root, workerPID, 1024, 4096)
	writeNetTCP(t, root, workerPID, nil)
	fdDir := filepath.Join(root, strconv.Itoa(workerPID), "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}

	pf := filepath.Join(t.TempDir(), "nginx.pid")
	if err := os.WriteFile(pf, []byte(fmt.Sprintf("%d\n", masterPID)), 0644); err != nil {
		t.Fatal(err)
	}
	return root, pf
}

type fdFailWriter struct{ err error }

func (fw *fdFailWriter) Write(p []byte) (int, error) { return 0, fw.err }

func TestFDCollect_Basic(t *testing.T) {
	procRoot, pidFile := buildFDCollectFixture(t, 10000, 10001)

	var buf bytes.Buffer
	c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: &buf}
	if err := c.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var snap Snapshot
	if err := json.Unmarshal(buf.Bytes(), &snap); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	if snap.Event != "fd_snapshot" {
		t.Errorf("event: got %q, want fd_snapshot", snap.Event)
	}
	if snap.WorkerPID != 10001 {
		t.Errorf("worker_pid: got %d, want 10001", snap.WorkerPID)
	}
	if snap.Ts.IsZero() {
		t.Error("ts: got zero timestamp")
	}
}

func TestFDCollect_MasterPIDError(t *testing.T) {
	c := &Collector{ProcRoot: t.TempDir(), PIDFile: "/nonexistent.pid"}
	if err := c.Collect(); err == nil {
		t.Error("expected error for missing pid file")
	}
}

func TestFDCollect_BootTimeError(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte("10002\n"), 0644)
	c := &Collector{ProcRoot: "/nonexistent/proc", PIDFile: pidFile}
	if err := c.Collect(); err == nil {
		t.Error("expected error for missing procRoot/stat")
	}
}

func TestFDCollect_EnumerateWorkersError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test directory permissions as root")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "stat"), []byte("btime 1000000\n"), 0644)
	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte("10003\n"), 0644)

	// 0111 = execute-only: open known files works (ReadBootTime), ReadDir fails (EnumerateWorkers)
	if err := os.Chmod(root, 0111); err != nil {
		t.Skip("cannot chmod test directory")
	}
	defer os.Chmod(root, 0755)

	c := &Collector{ProcRoot: root, PIDFile: pidFile}
	if err := c.Collect(); err == nil {
		t.Error("expected error for unlistable procRoot")
	}
}

func TestFDCollect_WorkerExited(t *testing.T) {
	// Worker enumerated but ReadCounts returns ErrNotExist (no limits file) → skip
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "stat"), []byte("btime 1000000\n"), 0644)
	masterPID, workerPID := 10004, 10005
	writeWorkerStatForFD(t, root, masterPID, 1, 50)
	writeWorkerStatForFD(t, root, workerPID, masterPID, 100)
	// Intentionally no limits file: readFDLimit → ErrNotExist → continue in Collect

	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", masterPID)), 0644)

	var buf bytes.Buffer
	c := &Collector{ProcRoot: root, PIDFile: pidFile, Out: &buf}
	if err := c.Collect(); err != nil {
		t.Fatalf("Collect: unexpected error for exited worker: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for exited worker, got: %s", buf.String())
	}
}

func TestFDCollect_ReadCountsNonExistError(t *testing.T) {
	// Worker enumerated, ReadCounts fails with non-ErrNotExist → Collect returns error
	// Use net/tcp-as-directory trick: limits exists, but readSocketTable returns EISDIR
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "stat"), []byte("btime 1000000\n"), 0644)
	masterPID, workerPID := 10006, 10007
	writeWorkerStatForFD(t, root, masterPID, 1, 50)
	writeWorkerStatForFD(t, root, workerPID, masterPID, 100)
	writeLimits(t, root, workerPID, 1024, 4096)
	netDir := filepath.Join(root, strconv.Itoa(workerPID), "net")
	if err := os.MkdirAll(filepath.Join(netDir, "tcp"), 0755); err != nil {
		t.Fatal(err)
	}

	pidFile := filepath.Join(t.TempDir(), "nginx.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", masterPID)), 0644)

	c := &Collector{ProcRoot: root, PIDFile: pidFile, Out: &bytes.Buffer{}}
	if err := c.Collect(); err == nil {
		t.Error("expected error for ReadCounts failure (EISDIR not ErrNotExist)")
	}
}

func TestFDCollect_EncodeError(t *testing.T) {
	procRoot, pidFile := buildFDCollectFixture(t, 10008, 10009)

	c := &Collector{ProcRoot: procRoot, PIDFile: pidFile, Out: &fdFailWriter{err: errors.New("write failed")}}
	if err := c.Collect(); err == nil {
		t.Error("expected error for failing writer")
	}
}
