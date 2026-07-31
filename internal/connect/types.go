package connect

import "time"

// ConnectEvent is decoded from a raw ring buffer record.
// Its layout must match struct connect_event in connect.bpf.c.
type ConnectEvent struct {
	TsNs        uint64
	LatencyNs   uint64
	DAddr       uint32 // IPv4 destination address, network byte order
	DPort       uint16 // destination port, host byte order
	Failed      uint8  // 1 = did not reach ESTABLISHED
	Retransmits uint8
}

// Summary is written as one NDJSON line per upstream per collection interval.
type Summary struct {
	Ts          time.Time `json:"ts"`
	Event       string    `json:"event"` // "connect_summary"
	Upstream    string    `json:"upstream"`
	WindowSec   int       `json:"window_s"`
	Count       int       `json:"count"`
	P50Ms       float64   `json:"p50_ms"`
	P95Ms       float64   `json:"p95_ms"`
	P99Ms       float64   `json:"p99_ms"`
	Retransmits int       `json:"retransmits"`
	Failed      int       `json:"failed"`
}
