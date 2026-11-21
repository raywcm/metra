package collector

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestBlockedVolumeKeepsOtherCapacitiesAndInventoryRefreshing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var rootReads, stalledReads atomic.Int32
		targets := []mountTarget{{path: "/", disk: "disk0"}, {path: "/nfs", remote: true}}
		list := func(context.Context, []string) ([]mountTarget, error) { return targets, nil }
		usage := func(ctx context.Context, path, _ string) Mount {
			m := Mount{Path: path, Total: 100}
			switch path {
			case "/":
				m.Total = uint64(rootReads.Add(1))
			case "/nfs":
				if stalledReads.Add(1) > 1 {
					<-ctx.Done()
				}
			}
			return m
		}
		var v volumeCache
		sample := func() map[string][]Mount {
			return sampleVolumes(t, &v, ctx, map[string]string{"disk0": "A"}, list, usage)
		}
		first := sample()
		if len(first["disk0"]) != 1 || first["disk0"][0].Total != 1 || first[""][0].Total != 100 {
			t.Fatal("missing initial readings", first)
		}
		first["disk0"][0].Total = 0
		if sample()["disk0"][0].Total != 1 {
			t.Fatal("returned snapshot mutated the cache")
		}
		targets = append(targets, mountTarget{path: "/new", remote: true})
		for i := 2; i <= 5; i++ {
			time.Sleep(volumeRefreshInterval)
			got := sample()
			if len(got["disk0"]) != 1 || got["disk0"][0].Total != uint64(i) || len(got[""]) != 2 ||
				got[""][0].Path != "/new" || got[""][0].Total != 100 || got[""][1].Path != "/nfs" {
				t.Fatalf("round %d: healthy volumes or inventory stopped refreshing: %+v", i, got)
			}
			if i >= 3 && !got[""][1].CapacityUnavailable {
				t.Fatal("last known capacity presented as current for a stalled mount", got)
			}
		}
		if stalledReads.Load() != 2 {
			t.Fatal("stalled capacity probe started more than once", stalledReads.Load())
		}
	})
}

func sampleVolumes(t *testing.T, v *volumeCache, ctx context.Context, disks map[string]string, list volumeList, usage volumeUsage) map[string][]Mount {
	t.Helper()
	v.sample(ctx, disks, list, usage)
	synctest.Wait()
	got, err := v.sample(ctx, disks, list, usage)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestVolumeProbeLimitLeavesLocalCapacityAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var active, calls atomic.Int32
		release := make(chan struct{})
		var targets []mountTarget
		for i := range 20 {
			targets = append(targets, mountTarget{path: fmt.Sprintf("/share-%02d", i), fsType: "nfs4", remote: true})
		}
		list := func(context.Context, []string) ([]mountTarget, error) { return targets, nil }
		usage := func(ctx context.Context, path, fs string) Mount {
			if fs == "nfs4" {
				calls.Add(1)
				if n := active.Add(1); n > 2 {
					t.Errorf("%d network probes started concurrently", n)
				}
				select {
				case <-ctx.Done():
				case <-release:
				}
				active.Add(-1)
			}
			return Mount{Path: path, FSType: fs, Total: 100}
		}
		var v volumeCache
		got := sampleVolumes(t, &v, ctx, nil, list, usage)
		if len(got[""]) != 20 || !got[""][0].CapacityUnavailable || got[""][0].FSType != "nfs4" {
			t.Fatal("waiting mounts should already be visible", got)
		}
		targets = append(targets, mountTarget{path: "/"})
		time.Sleep(3 * volumeRefreshInterval)
		got = sampleVolumes(t, &v, ctx, nil, list, usage)
		if len(got[""]) != 21 || got[""][0].Total != 100 || calls.Load() != 2 {
			t.Fatal("blocked mounts exhausted slots or duplicated work", calls.Load(), got)
		}
		close(release)
		synctest.Wait()
		got = sampleVolumes(t, &v, ctx, nil, list, usage)
		if calls.Load() != 20 || got[""][20].Total != 100 {
			t.Fatal("queued capacities did not drain within the limit", calls.Load(), got)
		}
	})
}

func TestVolumeRefreshRemovesUnmountedPathsOnlyAfterSuccessfulScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		targets := []mountTarget{{path: "/keep"}, {path: "/removed", remote: true}, {path: "/reused", device: "server:/export", mountID: 7, remote: true}}
		var listErr error
		list := func(context.Context, []string) ([]mountTarget, error) { return targets, listErr }
		removed, oldSource := make(chan struct{}), make(chan struct{})
		var reused atomic.Int32
		usage := func(_ context.Context, path, _ string) Mount {
			switch {
			case path == "/removed":
				<-removed
				return Mount{Path: path, Total: 111}
			case path == "/reused" && reused.Add(1) == 1:
				<-oldSource
				return Mount{Path: path, Total: 111}
			}
			return Mount{Path: path, Total: 222}
		}
		var v volumeCache
		first := sampleVolumes(t, &v, ctx, nil, list, usage)
		if len(first[""]) != 3 || !first[""][2].CapacityUnavailable {
			t.Fatal("mount with a blocked first reading is missing", first)
		}
		listErr = errors.New("mount table temporarily unreadable")
		targets = []mountTarget{{path: "/keep"}, {path: "/reused", device: "server:/export", mountID: 9, remote: true}}
		time.Sleep(volumeRefreshInterval)
		v.sample(ctx, nil, list, usage)
		synctest.Wait()
		got, err := v.sample(ctx, nil, list, usage)
		if err != listErr || len(got[""]) != 3 {
			t.Fatal("inventory discarded on discovery failure", got, err)
		}
		listErr = nil
		time.Sleep(volumeRefreshInterval)
		got = sampleVolumes(t, &v, ctx, nil, list, usage)
		if len(got[""]) != 2 || got[""][1].Path != "/reused" || !got[""][1].CapacityUnavailable {
			t.Fatal("removed mount or capacity survived rediscovery", got)
		}
		close(removed) // Free a probe slot for the new source.
		synctest.Wait()
		got = sampleVolumes(t, &v, ctx, nil, list, usage)
		if len(got[""]) != 2 || got[""][1].Total != 222 {
			t.Fatal("replacement did not publish while its old source was blocked", got)
		}
		close(oldSource)
		synctest.Wait()
		got = sampleVolumes(t, &v, ctx, nil, list, usage)
		if len(got[""]) != 2 || got[""][1].Total != 222 || reused.Load() != 2 {
			t.Fatal("late results resurrected a detached mount", got)
		}
	})
}

func TestReattachedDiskDoesNotInheritCachedMount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		path := "/original"
		hold := make(chan struct{})
		lists := 0
		list := func(context.Context, []string) ([]mountTarget, error) {
			lists++
			target := mountTarget{path: path, disk: "sda"}
			if lists == 2 {
				<-hold
			}
			return []mountTarget{target}, nil
		}
		usage := func(_ context.Context, path, _ string) Mount { return Mount{Path: path, Total: 100} }
		var v volumeCache
		old, replacement := map[string]string{"sda": "sda@1"}, map[string]string{"sda": "sda@2"}
		first := sampleVolumes(t, &v, ctx, old, list, usage)
		if len(first["sda"]) != 1 {
			t.Fatal("original disk missing", first)
		}
		time.Sleep(volumeRefreshInterval)
		v.sample(ctx, old, list, usage)
		synctest.Wait()
		path = "/replacement"
		got, _ := v.sample(ctx, replacement, list, usage)
		if len(got["sda"]) != 0 {
			t.Fatal("new disk inherited capacity", got)
		}
		close(hold)
		synctest.Wait()
		got, _ = v.sample(ctx, replacement, list, usage)
		if len(got["sda"]) != 0 {
			t.Fatal("late inventory reattached the old disk", got)
		}
		synctest.Wait()
		got = sampleVolumes(t, &v, ctx, replacement, list, usage)
		if lists != 3 || len(got["sda"]) != 1 || got["sda"][0].Path != path {
			t.Fatal("new disk waited for cache expiry", got)
		}
	})
}

func TestVolumeCloseStopsQueuedProbes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var calls atomic.Int32
		list := func(context.Context, []string) ([]mountTarget, error) {
			return []mountTarget{{path: "/a"}, {path: "/b"}, {path: "/c"}}, nil
		}
		release := make(chan struct{})
		usage := func(context.Context, string, string) Mount {
			calls.Add(1)
			<-release // A kernel call can outlive context cancellation.
			return Mount{Total: 100}
		}
		var v volumeCache
		sampleVolumes(t, &v, ctx, nil, list, usage)
		cancel()
		close(release)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("queued work started after shutdown")
		}
	})
}
