//go:build linux

package connect

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestCollector_Collect_Linux(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: if BPF loads successfully, select exits immediately

	c := &Collector{
		ProcRoot: t.TempDir(),
		PIDFile:  "/nonexistent/nginx.pid",
		Interval: time.Second,
		Out:      io.Discard,
	}
	// Without CAP_BPF (CI), returns an error from BPF load.
	// With CAP_BPF, returns nil after select exits on ctx.Done().
	// Either way, Collect() is exercised and returns without hanging.
	_ = c.Collect(ctx)
}
