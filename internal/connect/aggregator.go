package connect

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"time"
)

type upstreamAccum struct {
	latenciesMs []float64
	retransmits int
	failed      int
}

// Aggregator accumulates ConnectEvents and flushes per-upstream summaries.
type Aggregator struct {
	byUpstream map[string]*upstreamAccum
}

func newAggregator() *Aggregator {
	return &Aggregator{byUpstream: make(map[string]*upstreamAccum)}
}

// Add records a single connection event.
func (a *Aggregator) Add(e ConnectEvent) {
	key := upstreamKey(e.DAddr, e.DPort)
	acc := a.byUpstream[key]
	if acc == nil {
		acc = &upstreamAccum{}
		a.byUpstream[key] = acc
	}
	acc.latenciesMs = append(acc.latenciesMs, float64(e.LatencyNs)/1e6)
	acc.retransmits += int(e.Retransmits)
	if e.Failed != 0 {
		acc.failed++
	}
}

// Flush produces one Summary per upstream and resets accumulated state.
func (a *Aggregator) Flush(windowSec int, now time.Time) []Summary {
	summaries := make([]Summary, 0, len(a.byUpstream))
	for upstream, acc := range a.byUpstream {
		sort.Float64s(acc.latenciesMs)
		summaries = append(summaries, Summary{
			Ts:          now,
			Event:       "connect_summary",
			Upstream:    upstream,
			WindowSec:   windowSec,
			Count:       len(acc.latenciesMs),
			P50Ms:       percentile(acc.latenciesMs, 50),
			P95Ms:       percentile(acc.latenciesMs, 95),
			P99Ms:       percentile(acc.latenciesMs, 99),
			Retransmits: acc.retransmits,
			Failed:      acc.failed,
		})
	}
	a.byUpstream = make(map[string]*upstreamAccum)
	return summaries
}

// percentile returns the p-th percentile of a sorted slice (nearest-rank).
func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p*len(sorted)+99)/100 - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// upstreamKey builds a "host:port" string from a network-byte-order IPv4 address
// and a host-byte-order port.
func upstreamKey(daddr uint32, dport uint16) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], daddr)
	return fmt.Sprintf("%s:%d", net.IP(b[:]).String(), dport)
}
