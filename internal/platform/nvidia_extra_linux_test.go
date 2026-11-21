//go:build linux

package platform

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestNVMLGPMABI(t *testing.T) {
	if unsafe.Sizeof(gpmSupport{}) != 8 || unsafe.Sizeof(gpmMetric{}) != 40 || unsafe.Offsetof(gpmGet{}.Metrics) != 24 {
		t.Fatal("NVML GPM ABI layout mismatch")
	}
	if unsafe.Sizeof(nvmlAttributes{}) != 40 || unsafe.Sizeof(nvmlInstanceProfile{}) != 48 {
		t.Fatal("NVML engine capability ABI layout mismatch")
	}
}

func TestNVMLMediaCapabilitiesHideAbsentButKeepIdle(t *testing.T) {
	reads := 0
	zero := func(_ uintptr, value, period *uint32) int32 { reads++; *value = 0; return 0 }
	queries := 0
	a := nvmlExtra{encoder: zero, decoder: zero, jpeg: zero, ofa: zero,
		profile: func(_ uintptr, id uint32, p *nvmlInstanceProfile) int32 {
			queries++
			if id != 4 {
				return 3
			}
			*p = nvmlInstanceProfile{Slices: 7, Decoder: 7, JPEG: 7, OFA: 1}
			return 0
		}}
	d := &nvmlDevice{}
	g := EmptyGPU("id", "GPU", "test")
	g.Engines = []Engine{{ID: "encoder", Utilization: 0}, {ID: "decoder", Utilization: 0}}
	a.collectMedia(&g, d)
	if len(g.AbsentEngines) != 1 || g.AbsentEngines[0] != "encoder" || len(g.Engines) != 3 {
		t.Fatal("absent encoder shown or idle decoder hidden", g)
	}
	for _, engine := range g.Engines {
		if engine.ID == "encoder" || engine.Utilization != 0 {
			t.Fatal(engine)
		}
	}
	a.collectMedia(&g, d)
	if queries != 3 || reads != 2 {
		t.Fatal("static capabilities or readable CSV counters were repeatedly queried", queries, reads)
	}
	a.attributes = func(_ uintptr, p *nvmlAttributes) int32 { *p = nvmlAttributes{}; return 0 }
	d = &nvmlDevice{}
	g = EmptyGPU("mig", "Assigned GPU", "test")
	a.collectMedia(&g, d)
	if len(g.Engines) != 0 || len(g.AbsentEngines) != 4 || queries != 3 {
		t.Fatal("assigned device attributes should override full-chip profiles", g)
	}
}

func TestNVIDIAAbsentEngineCannotSupplyOverallUtilization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nvidia-smi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'GPU-1, Card, N/A, 128, 1024, 42, 85, 0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	backend := nvidia{
		path:  path,
		query: append(append([]string(nil), nvidiaBaseQuery...), "utilization.encoder"),
		extra: &nvmlExtra{
			devices:    make(map[string]*nvmlDevice),
			handle:     func(string, *uintptr) int32 { return 0 },
			attributes: func(uintptr, *nvmlAttributes) int32 { return 0 },
		},
	}
	devices, err := backend.Sample(context.Background())
	if err != nil || len(devices) != 1 || len(devices[0].Engines) != 0 || !math.IsNaN(devices[0].Utilization) {
		t.Fatalf("an absent encoder produced a live overall reading: %v, %v", devices, err)
	}
}

func TestLiveNVIDIABandwidth(t *testing.T) {
	if os.Getenv("METRA_TEST_NVIDIA") != "1" {
		t.Skip("explicit NVIDIA hardware verification")
	}
	b := nvidiaBackend()
	if b == nil {
		t.Fatal("nvidia-smi missing")
	}
	defer b.(interface{ Close() }).Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := b.Sample(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(180 * time.Millisecond)
	devices, err := b.Sample(ctx)
	if err != nil || len(devices) == 0 {
		t.Fatal(devices, err)
	}
	for _, g := range devices {
		if math.IsNaN(g.PCIeReceive) || math.IsNaN(g.PCIeSend) || g.PCIeCapacity <= 0 || math.IsNaN(g.PCIeCapacity) {
			t.Fatalf("PCIe counters missing: %+v", g)
		}
		if strings.Contains(g.Name, "H200") || strings.Contains(g.Name, "H100") {
			found := false
			for _, engine := range g.Engines {
				if engine.ID == "hbm" && engine.Utilization >= 0 && engine.Utilization <= 100 {
					found = true
				}
			}
			if !found {
				t.Fatalf("HBM GPM counter missing on %s: %+v", g.Name, g.Engines)
			}
		}
		t.Logf("%s: PCIe RX %.1f MiB/s, TX %.1f MiB/s, engines %d", g.Name, g.PCIeReceive/(1<<20), g.PCIeSend/(1<<20), len(g.Engines))
	}
}
