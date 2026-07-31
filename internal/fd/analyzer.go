package fd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

type workerSeries struct {
	snaps []Snapshot
}

// Analyze reads fd_snapshot NDJSON from r and writes a human-readable report.
// Only snapshots newer than cutoff are processed; pass zero to process all.
// Returns the number of lines skipped due to parse errors.
func Analyze(r io.Reader, cutoff time.Time, out io.Writer) (skipped int, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	series := map[int]*workerSeries{}

	for scanner.Scan() {
		var s Snapshot
		if err2 := json.Unmarshal(scanner.Bytes(), &s); err2 != nil {
			skipped++
			continue
		}
		if s.Event != "fd_snapshot" {
			continue
		}
		if !cutoff.IsZero() && s.Ts.Before(cutoff) {
			continue
		}
		ws := series[s.WorkerPID]
		if ws == nil {
			ws = &workerSeries{}
			series[s.WorkerPID] = ws
		}
		ws.snaps = append(ws.snaps, s)
	}
	if err2 := scanner.Err(); err2 != nil {
		return skipped, err2
	}

	if len(series) == 0 {
		return skipped, nil
	}

	// Report latest snapshot per worker, sorted by PID for stable output
	pids := make([]int, 0, len(series))
	for pid := range series {
		pids = append(pids, pid)
	}
	sort.Ints(pids)

	// Use the most recent snapshot across all workers as the report timestamp
	var reportTs time.Time
	for _, ws := range series {
		if last := ws.snaps[len(ws.snaps)-1]; last.Ts.After(reportTs) {
			reportTs = last.Ts
		}
	}
	fmt.Fprintf(out, "FD utilization (%s):\n", reportTs.Format(time.RFC3339))

	for _, pid := range pids {
		ws := series[pid]
		latest := ws.snaps[len(ws.snaps)-1]
		rate := fdRate(ws.snaps)

		rateStr := ""
		if rate != 0 {
			rateStr = fmt.Sprintf("  ↑ %.1f FDs/min", rate)
			if rate < 0 {
				rateStr = fmt.Sprintf("  ↓ %.1f FDs/min", -rate)
			}
		}

		fmt.Fprintf(out, "  worker %d:  %.0f%%  (%d / %d)%s\n",
			pid, latest.Pct, latest.FDCount, latest.FDLimit, rateStr)
		fmt.Fprintf(out, "    client sockets:    %d\n", latest.ClientSockets)
		fmt.Fprintf(out, "    upstream sockets:  %d\n", latest.UpstreamSockets)
		fmt.Fprintf(out, "    files:             %d\n", latest.Files)
		fmt.Fprintf(out, "    other:             %d\n", latest.Other)

		if rate > 0 {
			remaining := float64(latest.FDLimit-latest.FDCount) / rate
			fmt.Fprintf(out, "    projected exhaustion: ~%.0f minutes at current rate\n", remaining)
		}

		guidance(out, latest)
		fmt.Fprintln(out)
	}
	return skipped, nil
}

// fdRate returns the rate of FD growth in FDs/min using linear regression over
// the snapshot series. Returns 0 if fewer than 2 snapshots are available.
func fdRate(snaps []Snapshot) float64 {
	if len(snaps) < 2 {
		return 0
	}
	first := snaps[0]
	last := snaps[len(snaps)-1]
	elapsed := last.Ts.Sub(first.Ts).Minutes()
	if elapsed <= 0 {
		return 0
	}
	return float64(last.FDCount-first.FDCount) / elapsed
}

// guidance prints root cause hints based on the FD breakdown.
func guidance(out io.Writer, s Snapshot) {
	if s.FDCount == 0 {
		return
	}
	clientPct := float64(s.ClientSockets) / float64(s.FDCount) * 100
	upstreamPct := float64(s.UpstreamSockets) / float64(s.FDCount) * 100

	switch {
	case clientPct >= 70:
		fmt.Fprintf(out, "    → client sockets dominant: check keepalive_timeout; see slow client detection for send_timeout guidance\n")
	case upstreamPct >= 70:
		fmt.Fprintf(out, "    → upstream sockets dominant: upstream keepalive may not be working; see TIME_WAIT accumulation and keepalive reuse ratio\n")
	}
}
