//go:build linux

package connect

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/shinagawa-web/ngxray/internal/connect/bpf"
)

func (c *Collector) collect(ctx context.Context) error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("remove memlock: %w", err)
	}

	var objs bpf.ConnectObjects
	if err := bpf.LoadConnectObjects(&objs, nil); err != nil {
		return fmt.Errorf("load bpf objects: %w", err)
	}
	defer objs.Close()

	tp1, err := link.Tracepoint("sock", "inet_sock_set_state", objs.HandleInetSockSetState, nil)
	if err != nil {
		return fmt.Errorf("attach inet_sock_set_state: %w", err)
	}
	defer tp1.Close()

	tp2, err := link.Tracepoint("tcp", "tcp_retransmit_skb", objs.HandleTcpRetransmitSkb, nil)
	if err != nil {
		return fmt.Errorf("attach tcp_retransmit_skb: %w", err)
	}
	defer tp2.Close()

	if err := c.refreshWorkerPIDs(objs.WorkerPids); err != nil {
		log.Printf("connect: populate worker_pids: %v", err)
	}

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("open ringbuf: %w", err)
	}
	defer rd.Close()

	events := drainRingBuf(rd)
	agg := newAggregator()
	enc := json.NewEncoder(c.Out)
	windowSec := int(c.Interval.Seconds())

	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	refreshTicker := time.NewTicker(30 * time.Second)
	defer refreshTicker.Stop()

	for {
		select {
		case e, ok := <-events:
			if !ok {
				return nil
			}
			agg.Add(e)
		case <-ticker.C:
			for _, s := range agg.Flush(windowSec, time.Now()) {
				if err := enc.Encode(s); err != nil {
					log.Printf("connect: write summary: %v", err)
				}
			}
		case <-refreshTicker.C:
			if err := c.refreshWorkerPIDs(objs.WorkerPids); err != nil {
				log.Printf("connect: refresh worker_pids: %v", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// drainRingBuf spawns a goroutine that reads from rd and forwards decoded
// ConnectEvents on the returned channel. The channel is closed when rd is
// closed or returns an error.
func drainRingBuf(rd *ringbuf.Reader) <-chan ConnectEvent {
	ch := make(chan ConnectEvent, 256)
	go func() {
		defer close(ch)
		for {
			rec, err := rd.Read()
			if err != nil {
				return
			}
			var e ConnectEvent
			if err := binary.Read(bytes.NewReader(rec.RawSample), binary.LittleEndian, &e); err != nil {
				log.Printf("connect: decode event: %v", err)
				continue
			}
			ch <- e
		}
	}()
	return ch
}

// refreshWorkerPIDs reads nginx worker PIDs from /proc and upserts them
// into the BPF worker_pids map.
func (c *Collector) refreshWorkerPIDs(m *ebpf.Map) error {
	pids, err := workerPIDs(c.ProcRoot, c.PIDFile)
	if err != nil {
		return err
	}
	one := uint8(1)
	for _, pid := range pids {
		if err := m.Put(pid, one); err != nil {
			return fmt.Errorf("put pid %d: %w", pid, err)
		}
	}
	return nil
}
