//go:build linux

package platform

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego"
)

// ABI definitions follow NVIDIA's nvml.h. GPM uses literal version 1,
// not the size-encoded versions used by many other NVML structures.
type gpmSupport struct{ Version, Supported uint32 }
type gpmMetric struct {
	ID                        uint32
	Status                    int32
	Value                     float64
	ShortName, LongName, Unit uintptr
}
type gpmGet struct {
	Version, Count    uint32
	Previous, Current uintptr
	Metrics           [477]gpmMetric
}
type nvmlDevice struct {
	handle            uintptr
	previous, current uintptr
	sampled           time.Time
	media             *mediaCapabilities
	mediaChecked      time.Time
}

type mediaCapabilities struct{ Decoder, Encoder, JPEG, OFA uint32 }
type nvmlAttributes struct {
	SM, Copy, Decoder, Encoder, JPEG, OFA, GPUSlices, ComputeSlices uint32
	MemoryMB                                                        uint64
}
type nvmlInstanceProfile struct {
	ID, P2P, Slices, Instances, SM, Copy, Decoder, Encoder, JPEG, OFA uint32
	MemoryMB                                                          uint64
}

type nvmlExtra struct {
	mu            sync.Mutex
	closing       atomic.Bool
	closed        bool
	lib           uintptr
	devices       map[string]*nvmlDevice
	init          func() int32
	shutdown      func() int32
	handle        func(string, *uintptr) int32
	pcie          func(uintptr, uint32, *uint32) int32
	maxGen, width func(uintptr, *uint32) int32
	support       func(uintptr, *gpmSupport) int32
	alloc         func(*uintptr) int32
	free          func(uintptr) int32
	sample        func(uintptr, uintptr) int32
	metrics       func(*gpmGet) int32
	attributes    func(uintptr, *nvmlAttributes) int32
	profile       func(uintptr, uint32, *nvmlInstanceProfile) int32
	encoder       func(uintptr, *uint32, *uint32) int32
	decoder       func(uintptr, *uint32, *uint32) int32
	jpeg          func(uintptr, *uint32, *uint32) int32
	ofa           func(uintptr, *uint32, *uint32) int32
}

func newNvidiaExtra() nvidiaExtra {
	lib, err := purego.Dlopen("libnvidia-ml.so.1", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	a := &nvmlExtra{lib: lib, devices: map[string]*nvmlDevice{}}
	bind := func(name string, target any) bool {
		addr, e := purego.Dlsym(lib, name)
		if e != nil {
			return false
		}
		purego.RegisterFunc(target, addr)
		return true
	}
	if !bind("nvmlInit_v2", &a.init) || !bind("nvmlShutdown", &a.shutdown) || !bind("nvmlDeviceGetHandleByUUID", &a.handle) || a.init() != 0 {
		purego.Dlclose(lib)
		return nil
	}
	bind("nvmlDeviceGetPcieThroughput", &a.pcie)
	bind("nvmlDeviceGetMaxPcieLinkGeneration", &a.maxGen)
	bind("nvmlDeviceGetCurrPcieLinkWidth", &a.width)
	bind("nvmlGpmQueryDeviceSupport", &a.support)
	bind("nvmlGpmSampleAlloc", &a.alloc)
	bind("nvmlGpmSampleFree", &a.free)
	bind("nvmlGpmSampleGet", &a.sample)
	bind("nvmlGpmMetricsGet", &a.metrics)
	bind("nvmlDeviceGetAttributes_v2", &a.attributes)
	bind("nvmlDeviceGetGpuInstanceProfileInfo", &a.profile)
	bind("nvmlDeviceGetEncoderUtilization", &a.encoder)
	bind("nvmlDeviceGetDecoderUtilization", &a.decoder)
	bind("nvmlDeviceGetJpgUtilization", &a.jpeg)
	bind("nvmlDeviceGetOfaUtilization", &a.ofa)
	return a
}

func (a *nvmlExtra) Collect(ctx context.Context, devices []GPU) {
	a.mu.Lock()
	defer func() {
		if a.closing.Load() {
			a.closeLocked()
		}
		a.mu.Unlock()
	}()
	if a.closed || a.closing.Load() {
		return
	}
	seen := map[string]bool{}
	for i := range devices {
		if ctx.Err() != nil {
			return
		}
		g := &devices[i]
		seen[g.ID] = true
		d := a.devices[g.ID]
		if d == nil {
			d = &nvmlDevice{}
			if a.handle(g.ID, &d.handle) != 0 {
				continue
			}
			a.devices[g.ID] = d
			if a.support != nil && a.alloc != nil && a.free != nil && a.sample != nil && a.metrics != nil {
				s := gpmSupport{Version: 1}
				if a.support(d.handle, &s) == 0 && s.Supported != 0 {
					if a.alloc(&d.previous) != 0 || a.alloc(&d.current) != 0 {
						a.freeSamples(d)
					}
				}
			}
		}
		a.collectMedia(g, d)
		if a.pcie != nil {
			var receive, send uint32
			if a.pcie(d.handle, 1, &receive) == 0 {
				g.PCIeReceive = float64(receive) * 1024
			}
			if a.pcie(d.handle, 0, &send) == 0 {
				g.PCIeSend = float64(send) * 1024
			}
		}
		if a.maxGen != nil && a.width != nil {
			var generation, lanes uint32
			if a.maxGen(d.handle, &generation) == 0 && a.width(d.handle, &lanes) == 0 {
				g.PCIeCapacity = pcieCapacity(generation, lanes)
			}
		}
		g.Engines = addEngine(g.Engines, "pcie-rx", "PCIe receive", linkPercent(g.PCIeReceive, g.PCIeCapacity))
		g.Engines = addEngine(g.Engines, "pcie-tx", "PCIe send", linkPercent(g.PCIeSend, g.PCIeCapacity))
		if d.current == 0 || (!d.sampled.IsZero() && time.Since(d.sampled) < 100*time.Millisecond) {
			continue
		}
		if a.sample(d.handle, d.current) != 0 {
			d.sampled = time.Time{}
			continue
		}
		if !d.sampled.IsZero() {
			query := gpmGet{Version: 1, Count: 1, Previous: d.previous, Current: d.current}
			query.Metrics[0].ID = 10 // DRAM bandwidth vs theoretical maximum.
			if a.metrics(&query) == 0 && query.Metrics[0].Status == 0 {
				name, id := "Memory bandwidth", "bandwidth"
				if strings.Contains(g.Name, "H100") || strings.Contains(g.Name, "H200") || strings.Contains(g.Name, "A100") {
					name, id = "HBM bandwidth", "hbm"
				}
				g.Engines = addEngine(g.Engines, id, name, query.Metrics[0].Value)
			}
		}
		d.previous, d.current = d.current, d.previous
		d.sampled = time.Now()
	}
	for id, d := range a.devices {
		if !seen[id] {
			a.freeSamples(d)
			delete(a.devices, id)
		}
	}
}

// Utilization APIs can return success and zero even when an engine is absent.
// MIG attributes describe the assigned device; full-chip MIG profiles describe
// physical GPU capabilities without enabling MIG or creating an instance.
func (a *nvmlExtra) mediaCapabilities(handle uintptr) *mediaCapabilities {
	if a.attributes != nil {
		var v nvmlAttributes
		if a.attributes(handle, &v) == 0 {
			return &mediaCapabilities{v.Decoder, v.Encoder, v.JPEG, v.OFA}
		}
	}
	if a.profile != nil {
		var best nvmlInstanceProfile
		for _, id := range []uint32{4, 5, 6} { // Full-chip 7, 8 and 6 slice profiles.
			var v nvmlInstanceProfile
			if a.profile(handle, id, &v) == 0 && v.Slices > best.Slices {
				best = v
			}
		}
		if best.Slices > 0 {
			return &mediaCapabilities{best.Decoder, best.Encoder, best.JPEG, best.OFA}
		}
	}
	return nil
}

func (a *nvmlExtra) collectMedia(g *GPU, d *nvmlDevice) {
	if d.mediaChecked.IsZero() || (d.media == nil && time.Since(d.mediaChecked) >= 30*time.Second) {
		d.media = a.mediaCapabilities(d.handle)
		d.mediaChecked = time.Now()
	}
	for i, item := range []struct {
		id, name string
		query    func(uintptr, *uint32, *uint32) int32
	}{{"encoder", "Encoder", a.encoder}, {"decoder", "Decoder", a.decoder}, {"jpeg", "JPEG", a.jpeg}, {"ofa", "Optical flow", a.ofa}} {
		absent := false
		if d.media != nil {
			absent = [4]uint32{d.media.Encoder, d.media.Decoder, d.media.JPEG, d.media.OFA}[i] == 0
		}
		if !absent {
			readable := false
			for _, engine := range g.Engines {
				if engine.ID == item.id {
					readable = true
					break
				}
			}
			if readable {
				continue // Reuse the CSV sample instead of polling each engine twice.
			}
		}
		if !absent && item.query == nil {
			continue // Retain the CSV counter when the older NVML lacks this API.
		}
		var value, period uint32
		status := int32(3) // NVML_ERROR_NOT_SUPPORTED
		if !absent {
			status = item.query(d.handle, &value, &period)
		}
		filtered := g.Engines[:0]
		for _, engine := range g.Engines {
			if engine.ID != item.id {
				filtered = append(filtered, engine)
			}
		}
		g.Engines = filtered
		if absent || status == 3 {
			g.AbsentEngines = append(g.AbsentEngines, item.id)
		} else if status == 0 {
			g.Engines = addEngine(g.Engines, item.id, item.name, float64(value))
		}
	}
}

func (a *nvmlExtra) freeSamples(d *nvmlDevice) {
	if a.free != nil {
		if d.previous != 0 {
			a.free(d.previous)
		}
		if d.current != 0 {
			a.free(d.current)
		}
	}
	d.previous, d.current = 0, 0
}
func (a *nvmlExtra) closeLocked() {
	if a.closed {
		return
	}
	for _, d := range a.devices {
		a.freeSamples(d)
	}
	a.shutdown()
	purego.Dlclose(a.lib)
	a.closed = true
}
func (a *nvmlExtra) Close() {
	a.closing.Store(true)
	// A failing driver must not prevent the terminal from exiting. An active
	// read performs cleanup when it returns; otherwise close immediately.
	if a.mu.TryLock() {
		a.closeLocked()
		a.mu.Unlock()
	}
}
