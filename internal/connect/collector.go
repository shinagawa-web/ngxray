package connect

import (
	"context"
	"io"
	"time"
)

// Collector drives BPF-based upstream connect latency collection.
// Collect is implemented per-platform: collector_linux.go on Linux,
// collector_stub.go on all other systems.
type Collector struct {
	ProcRoot string
	PIDFile  string
	Out      io.Writer
	Interval time.Duration
}

// Collect runs until ctx is cancelled, periodically writing connect_summary
// NDJSON lines to Out. On non-Linux systems it returns an error immediately.
func (c *Collector) Collect(ctx context.Context) error {
	return c.collect(ctx)
}
