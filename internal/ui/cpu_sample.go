package ui

import (
	"context"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
)

// Keep the same bounded worker until a first CPU delta is ready. At most one
// extra collection is made: a broken counter cannot monopolize the queue.
func probeWithCPUSample(ctx context.Context, previous probe.Result, interval time.Duration, collect func() probe.Result) probe.Result {
	first := collect()
	if first.Status != probe.StatusOnline || first.CPUTotal == 0 {
		return first
	}
	first.ApplyPrevious(previous)
	if first.CPUValid {
		return first
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return first
	case <-timer.C:
	}
	if ctx.Err() != nil {
		return first
	}
	second := collect()
	if second.Status == probe.StatusOnline {
		second.ApplyPrevious(first)
	}
	return second
}
