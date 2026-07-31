package workers

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const hz = 100 // standard Linux clock ticks per second

// Worker is a single nginx worker process with its start time.
type Worker struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// readBootTime reads the system boot time from $procRoot/stat.
func readBootTime(procRoot string) (time.Time, error) {
	f, err := os.Open(filepath.Join(procRoot, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "btime ") {
			sec, err := strconv.ParseInt(strings.TrimPrefix(line, "btime "), 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse btime: %w", err)
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("btime not found in %s/stat", procRoot)
}

// readMasterPID reads the nginx master PID from a pid file.
func readMasterPID(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, fmt.Errorf("read pid file %s: %w", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid from %s: %w", pidFile, err)
	}
	return pid, nil
}

// enumerateWorkers returns all processes under procRoot whose PPID matches masterPID.
func enumerateWorkers(procRoot string, masterPID int, bootTime time.Time) ([]Worker, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", procRoot, err)
	}

	var workers []Worker
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		ppid, startTicks, err := readStat(procRoot, pid)
		if err != nil {
			// process likely exited
			continue
		}
		if ppid != masterPID {
			continue
		}
		// Divide first so the multiplication stays well within int64 range
		// even on boxes with multi-year uptime (safe up to ~292 years at HZ=100).
		startedAt := bootTime.Add(time.Duration(startTicks) * (time.Second / hz))
		workers = append(workers, Worker{PID: pid, StartedAt: startedAt.UTC()})
	}
	return workers, nil
}

// readStat reads ppid and starttime from $procRoot/$pid/stat.
//
// /proc/[pid]/stat format: pid (comm) state ppid ... starttime
// comm can contain spaces and parentheses, so we anchor parsing on the last ')'.
func readStat(procRoot string, pid int) (ppid int, startTicks uint64, err error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, 0, err
	}
	s := string(data)
	end := strings.LastIndex(s, ")")
	if end < 0 {
		return 0, 0, fmt.Errorf("pid %d: malformed stat", pid)
	}
	// Fields after ')': [state ppid pgrp session tty_nr tpgid flags
	//   minflt cminflt majflt cmajflt utime stime cutime cstime
	//   priority nice num_threads itrealvalue starttime ...]
	// Index 0=state, 1=ppid, 19=starttime
	fields := strings.Fields(s[end+1:])
	if len(fields) < 20 {
		return 0, 0, fmt.Errorf("pid %d: too few stat fields", pid)
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("pid %d: parse ppid: %w", pid, err)
	}
	startTicks, err = strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("pid %d: parse starttime: %w", pid, err)
	}
	return ppid, startTicks, nil
}
