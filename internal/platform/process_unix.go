//go:build linux || darwin

package platform

import (
	"context"
	"errors"
	"github.com/shirou/gopsutil/v4/process"
	"os"
	"syscall"
)

// Revalidate PID creation time immediately before signalling to reject stale selections.
func Terminate(ctx context.Context, pid int32, created int64) error {
	if pid <= 1 || pid == int32(os.Getpid()) {
		return errors.New("refusing to terminate PID 1 or metra itself")
	}
	if created <= 0 {
		return errors.New("process identity unavailable; refusing to signal")
	}
	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return err
	}
	now, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return err
	}
	if now != created {
		return errors.New("process exited or PID was reused; refresh and retry")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return p.SendSignalWithContext(ctx, syscall.SIGTERM)
}
