package workers

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Snapshot is the NDJSON record emitted each collection interval.
type Snapshot struct {
	Ts      time.Time `json:"ts"`
	Event   string    `json:"event"`
	Workers []Worker  `json:"workers"`
}

// Collector polls /proc and writes workers_snapshot records.
type Collector struct {
	ProcRoot string // injectable for tests; production = "/proc"
	PIDFile  string
	Out      io.Writer
}

// Collect takes one snapshot and writes a single NDJSON line.
func (c *Collector) Collect() error {
	masterPID, err := ReadMasterPID(c.PIDFile)
	if err != nil {
		return err
	}
	bootTime, err := ReadBootTime(c.ProcRoot)
	if err != nil {
		return err
	}
	workers, err := EnumerateWorkers(c.ProcRoot, masterPID, bootTime, TickDuration)
	if err != nil {
		return err
	}
	snap := Snapshot{
		Ts:      time.Now().UTC(),
		Event:   "workers_snapshot",
		Workers: workers,
	}
	if err := json.NewEncoder(c.Out).Encode(snap); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}
