package collector

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/disk"
)

const volumeRefreshInterval = 15 * time.Second

type mountTarget struct {
	path, fsType, device string
	disk, diskID         string
	fsDevice             string // Linux major:minor, shared by bind mounts.
	mountID              uint64 // Linux mount instance, distinct even for the same source.
	remote               bool
}

type volumeEntry struct {
	mountTarget
	mount   Mount
	readAt  time.Time
	probing bool
}

type volumeList func(context.Context, []string) ([]mountTarget, error)
type volumeUsage func(context.Context, string, string) Mount

// Each pool can have two calls inside statfs. A blocked network filesystem
// cannot consume a local volume's slots, even after it has been unmounted.
type volumeProbes struct {
	pending []*volumeEntry
	active  int
}

type volumeCache struct {
	mu              sync.Mutex
	byPath          map[string]*volumeEntry
	disks           map[string]string
	diskGeneration  uint64
	listing         bool
	listedAt, cycle time.Time
	local, remote   volumeProbes
	err             error
}

func (v *volumeCache) sample(ctx context.Context, disks map[string]string, list volumeList, usage volumeUsage) (map[string][]Mount, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	changed := !maps.Equal(disks, v.disks)
	if changed {
		v.disks = maps.Clone(disks)
		v.diskGeneration++
		v.listedAt = time.Time{}
		for path, e := range v.byPath {
			if id, ok := disks[e.disk]; e.disk != "" && (!ok || id != e.diskID) {
				delete(v.byPath, path)
			}
		}
	}
	if !v.listing && now.Sub(v.listedAt) >= volumeRefreshInterval && ctx.Err() == nil {
		v.listing, v.listedAt = true, now
		go v.discover(ctx, v.disks, v.diskGeneration, list, usage)
	}
	newCycle := now.Sub(v.cycle) >= volumeRefreshInterval
	if newCycle {
		v.cycle = now
	}
	if newCycle || changed {
		v.queue(ctx, usage)
	}
	result := make(map[string][]Mount)
	for _, e := range v.byPath {
		m := e.mount
		if now.Sub(e.readAt) >= 2*volumeRefreshInterval {
			m = Mount{Path: e.path, FSType: e.fsType, CapacityUnavailable: true}
		}
		result[e.disk] = append(result[e.disk], m)
	}
	for _, mounts := range result {
		sort.Slice(mounts, func(i, j int) bool { return mounts[i].Path < mounts[j].Path })
	}
	return result, v.err
}

func (v *volumeCache) discover(ctx context.Context, disks map[string]string, generation uint64, list volumeList, usage volumeUsage) {
	targets, err := list(ctx, slices.Sorted(maps.Keys(disks)))
	v.mu.Lock()
	defer v.mu.Unlock()
	v.listing = false
	if generation != v.diskGeneration || ctx.Err() != nil {
		return
	}
	v.err = err
	if err != nil {
		return
	}
	current := make(map[string]*volumeEntry, len(targets))
	for _, target := range targets {
		target.diskID = disks[target.disk]
		entry := v.byPath[target.path]
		if entry == nil || entry.mountTarget != target {
			entry = &volumeEntry{mountTarget: target}
		}
		current[target.path] = entry
	}
	v.byPath = current
	v.queue(ctx, usage)
}

// Rebuild the pending lists on rediscovery or a new refresh cycle. Prefer
// mounts that have never returned a reading, then the oldest reading.
func (v *volumeCache) queue(ctx context.Context, usage volumeUsage) {
	v.local.pending, v.remote.pending = nil, nil
	for _, e := range v.byPath {
		if e.probing || !e.readAt.Before(v.cycle) {
			continue
		}
		q := &v.local
		if e.remote {
			q = &v.remote
		}
		q.pending = append(q.pending, e)
	}
	for _, q := range []*volumeProbes{&v.local, &v.remote} {
		sort.Slice(q.pending, func(i, j int) bool {
			a, b := q.pending[i], q.pending[j]
			if a.readAt.Equal(b.readAt) {
				return a.path < b.path
			}
			return a.readAt.Before(b.readAt)
		})
		v.start(ctx, q, usage)
	}
}

func (v *volumeCache) start(ctx context.Context, q *volumeProbes, usage volumeUsage) {
	for q.active < 2 && len(q.pending) > 0 && ctx.Err() == nil {
		e := q.pending[0]
		q.pending = q.pending[1:]
		q.active++
		e.probing = true
		go func() {
			m := usage(ctx, e.path, e.fsType)
			v.mu.Lock()
			defer v.mu.Unlock()
			q.active--
			e.probing = false
			// A replacement at the same path has a different entry. A late
			// response can never publish the detached volume's capacity.
			if v.byPath[e.path] == e && ctx.Err() == nil {
				e.mount, e.readAt = m, time.Now()
			}
			v.start(ctx, q, usage)
		}()
	}
}

func readMounts(ctx context.Context, names []string) ([]mountTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parts, err := mountPartitions(ctx)
	if err != nil {
		return nil, err
	}
	var targets []mountTarget
	seen := map[string]bool{}
	for _, target := range usefulPartitions(parts) {
		if seen[target.path] {
			continue
		}
		seen[target.path] = true
		if strings.HasPrefix(target.device, "/dev/") {
			for _, n := range names {
				if platform.MountBelongs(target.device, n) {
					target.disk = n
					break
				}
			}
		}
		targets = append(targets, target)
	}
	// Some container roots are absent from the visible partition list.
	if !seen["/"] {
		targets = append(targets, mountTarget{path: "/"})
	}
	return targets, nil
}

func mountUsage(ctx context.Context, path, fsType string) Mount {
	m := Mount{Path: path, FSType: fsType, CapacityUnavailable: true}
	if ctx.Err() != nil {
		return m
	}
	u, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return m
	}
	m.CapacityUnavailable = false
	if m.FSType == "" {
		m.FSType = u.Fstype
	}
	m.Total, m.Used, m.Free, m.Percent = u.Total, u.Used, u.Free, u.UsedPercent
	return m
}
