//go:build darwin

package platform

import (
	"context"
	"golang.org/x/sys/unix"
	"math"
)

func ReadCPUFrequency(ctx context.Context, ids []int) CPUFrequency {
	result := CPUFrequency{MHz: math.NaN(), MinMHz: math.NaN(), MaxMHz: math.NaN(), Reported: true}
	if ctx.Err() != nil {
		return result
	}
	if hz, err := unix.SysctlUint64("hw.cpufrequency"); err == nil && hz > 0 {
		result.MHz = float64(hz) / 1e6
	}
	return result
}
