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
//
// Known limitation: the reload heuristic (new PID appears that was absent from
// the previous snapshot) also fires when a single worker crashes and the master
// immediately starts a replacement. In stable production environments this is
// rare; when it occurs the surviving workers will be reported as "old" until
// they naturally drain or the next real reload clears the state.
func Analyze(r io.Reader, cutoff time.Time, out io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB: handles hosts with many workers

	prev := map[int]Worker{}
	oldWorkers := map[int]Worker{} // accumulated set of workers expected to drain
	var reloadAt *time.Time        // time of the first reload in the current drain cycle

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

		// Detect reload: new PID appeared in a non-empty prev set.
		// On each reload, union prev into oldWorkers so multiple consecutive
		// reloads accumulate correctly instead of overwriting earlier state.
		if len(prev) > 0 {
			for pid := range current {
				if _, exists := prev[pid]; !exists {
					if reloadAt == nil {
						t := s.Ts
						reloadAt = &t
					}
					for pid, w := range prev {
						oldWorkers[pid] = w
					}
					fmt.Fprintf(out, "reload detected around %s\n\n", s.Ts.Format(time.RFC3339))
					break
				}
			}
		}

		if reloadAt != nil {
			for pid := range oldWorkers {
				if _, alive := current[pid]; !alive {
					delete(oldWorkers, pid)
				}
			}

			if len(oldWorkers) == 0 {
				drain := s.Ts.Sub(*reloadAt).Round(time.Second)
				fmt.Fprintf(out, "all old workers gone as of %s (drain took %s)\n",
					s.Ts.Format(time.RFC3339), drain)
				reloadAt = nil
			} else {
				for _, w := range oldWorkers {
					fmt.Fprintf(out, "  old worker pid:%d started:%s still running as of %s\n",
						w.PID, w.StartedAt.Format(time.RFC3339), s.Ts.Format(time.RFC3339))
				}
			}
		}

		prev = current
	}
	return scanner.Err()
}
