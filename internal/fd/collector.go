package fd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shinagawa-web/ngxray/internal/workers"
)

// Snapshot is the NDJSON record emitted each collection interval.
type Snapshot struct {
	Ts              time.Time `json:"ts"`
	Event           string    `json:"event"`
	WorkerPID       int       `json:"worker_pid"`
	FDCount         int       `json:"fd_count"`
	FDLimit         int       `json:"fd_limit"`
	Pct             float64   `json:"pct"`
	ClientSockets   int       `json:"client_sockets"`
	UpstreamSockets int       `json:"upstream_sockets"`
	Files           int       `json:"files"`
	Other           int       `json:"other"`
}

// Collector polls /proc and writes fd_snapshot records for each worker.
type Collector struct {
	ProcRoot string
	PIDFile  string
	Out      io.Writer
}

// Collect takes one snapshot for every nginx worker and writes NDJSON lines.
func (c *Collector) Collect() error {
	masterPID, err := workers.ReadMasterPID(c.PIDFile)
	if err != nil {
		return err
	}
	bootTime, err := workers.ReadBootTime(c.ProcRoot)
	if err != nil {
		return err
	}
	ws, err := workers.EnumerateWorkers(c.ProcRoot, masterPID, bootTime, workers.TickDuration)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, w := range ws {
		counts, err := ReadCounts(c.ProcRoot, w.PID)
		if err != nil {
			// Ignore ESRCH/ENOENT: worker exited between enumeration and FD read
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrProcessDone) {
				continue
			}
			return fmt.Errorf("read counts pid %d: %w", w.PID, err)
		}
		snap := Snapshot{
			Ts:              now,
			Event:           "fd_snapshot",
			WorkerPID:       w.PID,
			FDCount:         counts.FDCount,
			FDLimit:         counts.FDLimit,
			Pct:             counts.Pct(),
			ClientSockets:   counts.ClientSockets,
			UpstreamSockets: counts.UpstreamSockets,
			Files:           counts.Files,
			Other:           counts.Other,
		}
		if err := json.NewEncoder(c.Out).Encode(snap); err != nil {
			return fmt.Errorf("write fd_snapshot pid %d: %w", w.PID, err)
		}
	}
	return nil
}
