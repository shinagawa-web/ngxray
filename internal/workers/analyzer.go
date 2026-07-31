package workers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"
)

// Analyze reads workers_snapshot NDJSON from r, detects nginx reloads, and
// reports old workers still running after each reload. Only snapshots newer
// than cutoff are processed; pass zero to process all.
func Analyze(r io.Reader, cutoff time.Time, out io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB: handles hosts with many workers

	prev := map[int]Worker{}
	var reloadAt *time.Time
	preReload := map[int]Worker{}

	for scanner.Scan() {
		var s Snapshot
		if err := json.Unmarshal(scanner.Bytes(), &s); err != nil {
			log.Printf("workers: skipping corrupt line: %v", err)
			continue
		}
		if s.Event != "workers_snapshot" {
			continue
		}
		if !cutoff.IsZero() && s.Ts.Before(cutoff) {
			continue
		}

		current := map[int]Worker{}
		for _, w := range s.Workers {
			current[w.PID] = w
		}

		// New PID appeared in a non-empty set → reload
		if len(prev) > 0 {
			for pid := range current {
				if _, exists := prev[pid]; !exists {
					t := s.Ts
					reloadAt = &t
					preReload = prev
					fmt.Fprintf(out, "reload detected around %s\n\n", s.Ts.Format(time.RFC3339))
					break
				}
			}
		}

		if reloadAt != nil {
			var remaining []Worker
			for pid, w := range preReload {
				if _, alive := current[pid]; alive {
					remaining = append(remaining, w)
				}
			}
			if len(remaining) == 0 {
				drain := s.Ts.Sub(*reloadAt).Round(time.Second)
				fmt.Fprintf(out, "all old workers gone as of %s (drain took %s)\n", s.Ts.Format(time.RFC3339), drain)
				reloadAt = nil
				preReload = map[int]Worker{}
			} else {
				for _, w := range remaining {
					fmt.Fprintf(out, "  old worker pid:%d started:%s still running as of %s\n",
						w.PID, w.StartedAt.Format(time.RFC3339), s.Ts.Format(time.RFC3339))
				}
			}
		}

		prev = current
	}
	return scanner.Err()
}
