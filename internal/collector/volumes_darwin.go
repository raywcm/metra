package collector

import (
	"context"

	"golang.org/x/sys/unix"
)

// Read the cached mount table. MNT_WAIT also refreshes capacity and can
// block discovery of every other volume when a network share is offline.
func mountPartitions(ctx context.Context) ([]mountTarget, error) {
	count, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stats := make([]unix.Statfs_t, count+1)
		n, err := unix.Getfsstat(stats, unix.MNT_NOWAIT)
		if err != nil {
			return nil, err
		}
		if n >= len(stats) {
			count = n * 2
			continue
		}
		parts := make([]mountTarget, 0, n)
		for _, s := range stats[:n] {
			parts = append(parts, mountTarget{
				device: unix.ByteSliceToString(s.Mntfromname[:]),
				path:   unix.ByteSliceToString(s.Mntonname[:]),
				fsType: unix.ByteSliceToString(s.Fstypename[:]),
				remote: s.Flags&unix.MNT_LOCAL == 0,
			})
		}
		return parts, nil
	}
}
