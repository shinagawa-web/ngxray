package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// resetInjectables restores all injectable vars after a test.
func resetInjectables(t *testing.T) {
	t.Helper()
	origLogFatal := logFatal
	origLogFatalf := logFatalf
	origOsExit := osExit
	origOsExecutable := osExecutable
	t.Cleanup(func() {
		logFatal = origLogFatal
		logFatalf = origLogFatalf
		osExit = origOsExit
		osExecutable = origOsExecutable
	})
}

// fatalPanic makes logFatal/logFatalf panic with the message so tests can recover.
func fatalPanic(t *testing.T) {
	t.Helper()
	resetInjectables(t)
	logFatal = func(v ...any) { panic(fmt.Sprint(v...)) }
	logFatalf = func(format string, v ...any) { panic(fmt.Sprintf(format, v...)) }
	osExit = func(code int) { panic(fmt.Sprintf("exit(%d)", code)) }
}

// mustPanic asserts that fn panics and returns the panic message.
func mustPanic(t *testing.T, fn func()) string {
	t.Helper()
	var msg string
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic but did not panic")
			}
			msg = fmt.Sprint(r)
		}()
		fn()
	}()
	return msg
}

func TestUsage(t *testing.T) {
	usage()
}

func TestDefaultConfigPath(t *testing.T) {
	p := defaultConfigPath()
	if !strings.HasSuffix(p, "ngxray.toml") {
		t.Errorf("got %q, want suffix ngxray.toml", p)
	}
}

func TestDefaultConfigPath_Error(t *testing.T) {
	resetInjectables(t)
	osExecutable = func() (string, error) { return "", fmt.Errorf("injected") }
	if p := defaultConfigPath(); p != "ngxray.toml" {
		t.Errorf("got %q, want ngxray.toml", p)
	}
}

func TestOpenAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "test.ndjson")

	f, err := openAppend(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("line1\n")
	f.Close()

	f2, err := openAppend(path)
	if err != nil {
		t.Fatal(err)
	}
	f2.WriteString("line2\n")
	f2.Close()

	data, _ := os.ReadFile(path)
	if string(data) != "line1\nline2\n" {
		t.Errorf("openAppend: got %q", string(data))
	}
}

func TestOpenAppend_MkdirAllError(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "subdir")
	os.WriteFile(blocker, []byte("file"), 0644)
	if _, err := openAppend(filepath.Join(blocker, "test.ndjson")); err == nil {
		t.Error("expected error when MkdirAll fails")
	}
}

func TestWithSignalCancel(t *testing.T) {
	ctx := withSignalCancel(context.Background())
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("context not cancelled after SIGTERM")
	}
}

func TestRunCollector_ImmediateCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	runCollector(ctx, "test", func() error { calls++; return nil }, time.Hour)
	if calls != 1 {
		t.Errorf("expected 1 immediate call, got %d", calls)
	}
}

func TestRunCollector_ErrorAndTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCollector(ctx, "test", func() error {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n >= 2 {
				cancel()
			}
			return fmt.Errorf("test error")
		}, time.Millisecond)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runCollector did not stop")
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n < 2 {
		t.Errorf("expected >= 2 calls, got %d", n)
	}
}

// --- Config helpers ---

func writeAllDisabledConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := `
[workers]
enabled  = false
pid_file = "/nonexistent"
interval = 60
output   = "/nonexistent"

[fd]
enabled  = false
interval = 60
output   = "/nonexistent"
`
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	return cfgPath
}

func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	workersFile := filepath.Join(dir, "workers.ndjson")
	fdFile := filepath.Join(dir, "fd.ndjson")
	os.WriteFile(workersFile, nil, 0644)
	os.WriteFile(fdFile, nil, 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1

[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = %q

[fd]
enabled  = true
interval = 60
output   = %q
`, workersFile, fdFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	return cfgPath
}

// --- main() tests ---

func TestMain_NoArgs(t *testing.T) {
	fatalPanic(t)
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = []string{"ngxray"}
	msg := mustPanic(t, main)
	if !strings.Contains(msg, "exit(1)") {
		t.Errorf("got %q, want exit(1)", msg)
	}
}

func TestMain_UnknownCommand(t *testing.T) {
	fatalPanic(t)
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = []string{"ngxray", "badcmd"}
	msg := mustPanic(t, main)
	if !strings.Contains(msg, "exit(1)") {
		t.Errorf("got %q, want exit(1)", msg)
	}
}

func TestMain_Collect(t *testing.T) {
	fatalPanic(t)
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	cfgPath := writeAllDisabledConfig(t, t.TempDir())
	os.Args = []string{"ngxray", "collect", "--config", cfgPath}
	main() // all disabled → returns without panic
}

func TestMain_Report(t *testing.T) {
	fatalPanic(t)
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	cfgPath := writeTestConfig(t, t.TempDir())
	os.Args = []string{"ngxray", "report", "--config", cfgPath}
	main()
}

// --- runCollect tests ---

func TestRunCollect_AllDisabled(t *testing.T) {
	cfgPath := writeAllDisabledConfig(t, t.TempDir())
	runCollect(context.Background(), []string{"--config", cfgPath})
}

func TestRunCollect_EnabledWithCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`
[workers]
enabled  = true
pid_file = "/nonexistent/nginx.pid"
interval = 60
output   = %q
[fd]
enabled  = true
interval = 60
output   = %q
`, filepath.Join(dir, "workers.ndjson"), filepath.Join(dir, "fd.ndjson"))
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	runCollect(ctx, []string{"--config", cfgPath})
}

func TestRunCollect_BadConfig(t *testing.T) {
	fatalPanic(t)
	msg := mustPanic(t, func() {
		runCollect(context.Background(), []string{"--config", "/nonexistent/ngxray.toml"})
	})
	if !strings.Contains(msg, "load config") {
		t.Errorf("got %q", msg)
	}
}

func TestRunCollect_WorkersIntervalZero(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	cfg := fmt.Sprintf(`
[workers]
enabled  = true
pid_file = "/nonexistent"
interval = 0
output   = %q
[fd]
enabled  = false
interval = 60
output   = "/nonexistent"
`, filepath.Join(dir, "workers.ndjson"))
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runCollect(context.Background(), []string{"--config", cfgPath}) })
	if !strings.Contains(msg, "workers.interval") {
		t.Errorf("got %q", msg)
	}
}

func TestRunCollect_WorkersOpenError(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	// Make a file where the output dir should be, blocking MkdirAll
	blocker := filepath.Join(dir, "blocked")
	os.WriteFile(blocker, []byte{}, 0644)
	cfg := fmt.Sprintf(`
[workers]
enabled  = true
pid_file = "/nonexistent"
interval = 60
output   = %q
[fd]
enabled  = false
interval = 60
output   = "/nonexistent"
`, filepath.Join(blocker, "workers.ndjson"))
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runCollect(context.Background(), []string{"--config", cfgPath}) })
	if !strings.Contains(msg, "open output") {
		t.Errorf("got %q", msg)
	}
}

func TestRunCollect_FDIntervalZero(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	cfg := fmt.Sprintf(`
[workers]
enabled  = false
pid_file = "/nonexistent"
interval = 60
output   = "/nonexistent"
[fd]
enabled  = true
interval = 0
output   = %q
`, filepath.Join(dir, "fd.ndjson"))
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runCollect(context.Background(), []string{"--config", cfgPath}) })
	if !strings.Contains(msg, "fd.interval") {
		t.Errorf("got %q", msg)
	}
}

func TestRunCollect_FDOpenError(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	os.WriteFile(blocker, []byte{}, 0644)
	cfg := fmt.Sprintf(`
[workers]
enabled  = false
pid_file = "/nonexistent"
interval = 60
output   = "/nonexistent"
[fd]
enabled  = true
interval = 60
output   = %q
`, filepath.Join(blocker, "fd.ndjson"))
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runCollect(context.Background(), []string{"--config", cfgPath}) })
	if !strings.Contains(msg, "open output") {
		t.Errorf("got %q", msg)
	}
}

// --- runReport tests ---

func TestRunReport_EmptyFiles(t *testing.T) {
	cfgPath := writeTestConfig(t, t.TempDir())
	runReport([]string{"--config", cfgPath})
}

func TestRunReport_WorkersOnly(t *testing.T) {
	dir := t.TempDir()
	workersFile := filepath.Join(dir, "workers.ndjson")
	os.WriteFile(workersFile, nil, 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1
[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = %q
[fd]
enabled  = false
interval = 60
output   = "/nonexistent"
`, workersFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	runReport([]string{"--config", cfgPath})
}

func TestRunReport_FDOnly(t *testing.T) {
	dir := t.TempDir()
	fdFile := filepath.Join(dir, "fd.ndjson")
	os.WriteFile(fdFile, nil, 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1
[workers]
enabled  = false
pid_file = "/var/run/nginx.pid"
interval = 60
output   = "/nonexistent"
[fd]
enabled  = true
interval = 60
output   = %q
`, fdFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	runReport([]string{"--config", cfgPath})
}

func TestRunReport_SkippedLines(t *testing.T) {
	dir := t.TempDir()
	workersFile := filepath.Join(dir, "workers.ndjson")
	fdFile := filepath.Join(dir, "fd.ndjson")
	os.WriteFile(workersFile, []byte("not json\n"), 0644)
	os.WriteFile(fdFile, []byte("not json\n"), 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1
[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = %q
[fd]
enabled  = true
interval = 60
output   = %q
`, workersFile, fdFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	runReport([]string{"--config", cfgPath})
}

func TestRunReport_BadConfig(t *testing.T) {
	fatalPanic(t)
	msg := mustPanic(t, func() {
		runReport([]string{"--config", "/nonexistent/ngxray.toml"})
	})
	if !strings.Contains(msg, "load config") {
		t.Errorf("got %q", msg)
	}
}

func TestRunReport_DaysZero(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	cfg := `
[report]
days = 0
[workers]
enabled = false
pid_file = "/nonexistent"
interval = 60
output = "/nonexistent"
[fd]
enabled = false
interval = 60
output = "/nonexistent"
`
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runReport([]string{"--config", cfgPath}) })
	if !strings.Contains(msg, "report.days") {
		t.Errorf("got %q", msg)
	}
}

func TestRunReport_WorkersMissingFile(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	cfg := `
[report]
days = 1
[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = "/nonexistent/workers.ndjson"
[fd]
enabled  = false
interval = 60
output   = "/nonexistent"
`
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runReport([]string{"--config", cfgPath}) })
	if !strings.Contains(msg, "open") {
		t.Errorf("got %q", msg)
	}
}

func TestRunReport_FDMissingFile(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	workersFile := filepath.Join(dir, "workers.ndjson")
	os.WriteFile(workersFile, nil, 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1
[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = %q
[fd]
enabled  = true
interval = 60
output   = "/nonexistent/fd.ndjson"
`, workersFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runReport([]string{"--config", cfgPath}) })
	if !strings.Contains(msg, "open") {
		t.Errorf("got %q", msg)
	}
}

func TestRunReport_WorkersAnalyzeError(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	// Write 65KB+ line to trigger bufio.Scanner error
	workersFile := filepath.Join(dir, "workers.ndjson")
	longLine := make([]byte, 2<<20+1) // 2MB, exceeds 1MB scanner buffer
	for i := range longLine {
		longLine[i] = 'x'
	}
	longLine[len(longLine)-1] = '\n'
	os.WriteFile(workersFile, longLine, 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1
[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = %q
[fd]
enabled  = false
interval = 60
output   = "/nonexistent"
`, workersFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runReport([]string{"--config", cfgPath}) })
	if !strings.Contains(msg, "workers report") {
		t.Errorf("got %q", msg)
	}
}

func TestRunReport_FDAnalyzeError(t *testing.T) {
	fatalPanic(t)
	dir := t.TempDir()
	workersFile := filepath.Join(dir, "workers.ndjson")
	os.WriteFile(workersFile, nil, 0644)
	fdFile := filepath.Join(dir, "fd.ndjson")
	longLine := make([]byte, 2<<20+1)
	for i := range longLine {
		longLine[i] = 'x'
	}
	longLine[len(longLine)-1] = '\n'
	os.WriteFile(fdFile, longLine, 0644)
	cfg := fmt.Sprintf(`
[report]
days = 1
[workers]
enabled  = true
pid_file = "/var/run/nginx.pid"
interval = 60
output   = %q
[fd]
enabled  = true
interval = 60
output   = %q
`, workersFile, fdFile)
	cfgPath := filepath.Join(dir, "ngxray.toml")
	os.WriteFile(cfgPath, []byte(cfg), 0644)
	msg := mustPanic(t, func() { runReport([]string{"--config", cfgPath}) })
	if !strings.Contains(msg, "fd report") {
		t.Errorf("got %q", msg)
	}
}

// Silence the log output during expected-fatal tests.
var _ = log.New
