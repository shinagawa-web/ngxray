package connect

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Analyze reads connect_summary NDJSON from r, groups records by upstream
// within the cutoff window, and writes a diagnostic report to out.
// It returns the number of skipped (corrupt) lines and any scanner error.
func Analyze(r io.Reader, cutoff time.Time, out io.Writer) (skipped int, err error) {
	type upstreamStats struct {
		count       int
		retransmits int
		failed      int
		maxP99Ms    float64
	}

	byUpstream := make(map[string]*upstreamStats)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var s Summary
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil || s.Event != "connect_summary" {
			skipped++
			continue
		}
		if !cutoff.IsZero() && s.Ts.Before(cutoff) {
			continue
		}
		st := byUpstream[s.Upstream]
		if st == nil {
			st = &upstreamStats{}
			byUpstream[s.Upstream] = st
		}
		st.count += s.Count
		st.retransmits += s.Retransmits
		st.failed += s.Failed
		if s.P99Ms > st.maxP99Ms {
			st.maxP99Ms = s.P99Ms
		}
	}
	if err := sc.Err(); err != nil {
		return skipped, err
	}
	if len(byUpstream) == 0 {
		return skipped, nil
	}

	fmt.Fprintf(out, "%-25s  %6s  %8s  %6s  %6s\n", "upstream", "count", "max_p99", "retx", "failed")
	fmt.Fprintf(out, "%-25s  %6s  %8s  %6s  %6s\n", "-------", "-----", "-------", "----", "------")
	for upstream, st := range byUpstream {
		warn := ""
		if st.maxP99Ms >= 100 && st.retransmits > 0 {
			warn = "  ← check upstream"
		}
		fmt.Fprintf(out, "%-25s  %6d  %7.1fms  %6d  %6d%s\n",
			upstream, st.count, st.maxP99Ms, st.retransmits, st.failed, warn)
	}
	return skipped, nil
}
