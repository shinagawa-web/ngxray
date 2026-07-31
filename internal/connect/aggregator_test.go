package connect

import (
	"encoding/binary"
	"testing"
	"time"
)

func makeEvent(daddr uint32, dport uint16, latencyMs float64, retransmits uint8, failed uint8) ConnectEvent {
	return ConnectEvent{
		TsNs:        0,
		LatencyNs:   uint64(latencyMs * 1e6),
		DAddr:       daddr,
		DPort:       dport,
		Failed:      failed,
		Retransmits: retransmits,
	}
}

// ipv4 constructs a DAddr value the way the BPF pipeline does:
// __builtin_memcpy on a LE kernel packs network-order bytes as a LE uint32.
func ipv4(a, b, c, d byte) uint32 {
	return binary.LittleEndian.Uint32([]byte{a, b, c, d})
}

func TestAggregator_Basic(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(10, 0, 0, 1)
	agg.Add(makeEvent(addr, 8080, 1.0, 0, 0))
	agg.Add(makeEvent(addr, 8080, 2.0, 0, 0))
	agg.Add(makeEvent(addr, 8080, 3.0, 0, 0))

	summaries := agg.Flush(60, time.Now())
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
	s := summaries[0]
	if s.Count != 3 {
		t.Errorf("count: got %d, want 3", s.Count)
	}
	if s.P50Ms != 2.0 {
		t.Errorf("p50: got %.1f, want 2.0", s.P50Ms)
	}
	if s.Upstream != "10.0.0.1:8080" {
		t.Errorf("upstream: got %q", s.Upstream)
	}
	if s.Event != "connect_summary" {
		t.Errorf("event: got %q", s.Event)
	}
}

func TestAggregator_MultipleUpstreams(t *testing.T) {
	agg := newAggregator()
	addr1 := ipv4(10, 0, 0, 1)
	addr2 := ipv4(10, 0, 0, 2)
	agg.Add(makeEvent(addr1, 8080, 1.0, 0, 0))
	agg.Add(makeEvent(addr2, 9090, 5.0, 1, 0))

	summaries := agg.Flush(60, time.Now())
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
}

func TestAggregator_Retransmits(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(192, 168, 1, 1)
	agg.Add(makeEvent(addr, 443, 10.0, 2, 0))
	agg.Add(makeEvent(addr, 443, 20.0, 3, 0))

	summaries := agg.Flush(60, time.Now())
	if summaries[0].Retransmits != 5 {
		t.Errorf("retransmits: got %d, want 5", summaries[0].Retransmits)
	}
}

func TestAggregator_Failed(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(10, 0, 0, 1)
	agg.Add(makeEvent(addr, 8080, 100.0, 0, 1))
	agg.Add(makeEvent(addr, 8080, 200.0, 0, 0))

	summaries := agg.Flush(60, time.Now())
	if summaries[0].Failed != 1 {
		t.Errorf("failed: got %d, want 1", summaries[0].Failed)
	}
	if summaries[0].Count != 2 {
		t.Errorf("count: got %d, want 2", summaries[0].Count)
	}
}

func TestAggregator_FlushResetsState(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(10, 0, 0, 1)
	agg.Add(makeEvent(addr, 8080, 1.0, 0, 0))

	agg.Flush(60, time.Now())

	summaries := agg.Flush(60, time.Now())
	if len(summaries) != 0 {
		t.Errorf("expected empty flush after reset, got %d summaries", len(summaries))
	}
}

func TestAggregator_Percentiles(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(10, 0, 0, 1)
	// 100 events: 1ms to 100ms
	for i := 1; i <= 100; i++ {
		agg.Add(makeEvent(addr, 8080, float64(i), 0, 0))
	}

	summaries := agg.Flush(60, time.Now())
	s := summaries[0]
	// p50 of [1..100]: index = ceil(50/100*100)-1 = 49 → 50ms
	if s.P50Ms != 50.0 {
		t.Errorf("p50: got %.1f, want 50.0", s.P50Ms)
	}
	// p95: index = ceil(95/100*100)-1 = 94 → 95ms
	if s.P95Ms != 95.0 {
		t.Errorf("p95: got %.1f, want 95.0", s.P95Ms)
	}
	// p99: index = ceil(99/100*100)-1 = 98 → 99ms
	if s.P99Ms != 99.0 {
		t.Errorf("p99: got %.1f, want 99.0", s.P99Ms)
	}
}

func TestAggregator_SingleEvent(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(10, 0, 0, 1)
	agg.Add(makeEvent(addr, 8080, 5.0, 0, 0))

	summaries := agg.Flush(60, time.Now())
	s := summaries[0]
	if s.P50Ms != 5.0 || s.P95Ms != 5.0 || s.P99Ms != 5.0 {
		t.Errorf("single event percentiles: p50=%.1f p95=%.1f p99=%.1f", s.P50Ms, s.P95Ms, s.P99Ms)
	}
}

func TestPercentile_Empty(t *testing.T) {
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("got %.1f, want 0", got)
	}
}

func TestPercentile_ZeroP(t *testing.T) {
	// p=0: idx = (0*n+99)/100 - 1 = -1, clamped to 0 → first element.
	s := []float64{5.0, 10.0, 15.0}
	if got := percentile(s, 0); got != 5.0 {
		t.Errorf("p=0: got %.1f, want 5.0", got)
	}
}

func TestUpstreamKey(t *testing.T) {
	addr := ipv4(10, 0, 0, 1)
	key := upstreamKey(addr, 8080)
	if key != "10.0.0.1:8080" {
		t.Errorf("got %q, want %q", key, "10.0.0.1:8080")
	}
}

func TestAggregator_WindowSec(t *testing.T) {
	agg := newAggregator()
	addr := ipv4(10, 0, 0, 1)
	agg.Add(makeEvent(addr, 8080, 1.0, 0, 0))
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	summaries := agg.Flush(120, now)
	if summaries[0].WindowSec != 120 {
		t.Errorf("window_s: got %d, want 120", summaries[0].WindowSec)
	}
	if !summaries[0].Ts.Equal(now) {
		t.Errorf("ts: got %v, want %v", summaries[0].Ts, now)
	}
}
