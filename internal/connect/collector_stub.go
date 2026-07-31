//go:build !linux

package connect

import (
	"context"
	"errors"
)

func (c *Collector) collect(_ context.Context) error {
	return errors.New("connect latency collection requires Linux")
}
