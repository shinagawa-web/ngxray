//go:build !linux

package connect

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestCollector_CollectStub(t *testing.T) {
	c := &Collector{
		ProcRoot: "/proc",
		PIDFile:  "/run/nginx.pid",
		Interval: time.Second,
		Out:      io.Discard,
	}
	err := c.Collect(context.Background())
	if err == nil {
		t.Error("expected error from stub on non-linux")
	}
}
