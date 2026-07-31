package connect

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// workerPIDs reads the nginx master PID from pidFile then walks procRoot to
// find processes whose parent is the master, returning their PIDs.
func workerPIDs(procRoot, pidFile string) ([]uint32, error) {
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return nil, fmt.Errorf("read pid file: %w", err)
	}
	masterPID, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse master pid: %w", err)
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read proc: %w", err)
	}

	var pids []uint32
	for _, e := range entries {
		pid, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				ppid, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")), 10, 32)
				if ppid == masterPID {
					pids = append(pids, uint32(pid))
				}
				break
			}
		}
	}
	return pids, nil
}
