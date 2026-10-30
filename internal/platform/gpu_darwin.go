//go:build darwin

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
)

type appleGPU struct {
	fallback   []GPU
	discovered bool
}

func NewGPU() GPUBackend { return &appleGPU{} }
func (b *appleGPU) Sample(ctx context.Context) ([]GPU, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := nativeIOKit()
	if err == nil {
		var out []GPU
		err = a.objects("IOAccelerator", func(entry uint32) {
			if ctx.Err() != nil {
				return
			}
			model := a.prop(entry, "model")
			name := a.text(model)
			if model != 0 {
				a.release(model)
			}
			if name == "" {
				name = "GPU"
			}
			g := EmptyGPU(a.id(entry, "gpu"), name, "IORegistry")
			stats := a.prop(entry, "PerformanceStatistics")
			if stats != 0 {
				defer a.release(stats)
				g.Utilization = a.num(a.value(stats, "Device Utilization %"))
				g.Renderer = a.num(a.value(stats, "Renderer Utilization %"))
				g.Tiler = a.num(a.value(stats, "Tiler Utilization %"))
				g.Engines = addEngine(g.Engines, "renderer", "Renderer", g.Renderer)
				g.Engines = addEngine(g.Engines, "tiler", "Tiler", g.Tiler)
				g.MemoryUsed = a.num(a.value(stats, "In use system memory"))
				if math.IsNaN(g.MemoryUsed) {
					g.MemoryUsed = a.num(a.value(stats, "vramUsedBytes"))
				}
				g.UnifiedMemory = !math.IsNaN(a.num(a.value(stats, "In use system memory")))
			}
			config := a.prop(entry, "GPUConfigurationVariable")
			if config != 0 {
				v := a.num(a.value(config, "num_cores"))
				if !math.IsNaN(v) {
					g.Cores = int(v)
				}
				a.release(config)
			}
			g.Utilization = PeakUtilization(g.Utilization, g.Engines)
			out = append(out, g)
		})
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}
	// Discovery fallback for drivers without an IOAccelerator service.
	if b.discovered {
		return append([]GPU(nil), b.fallback...), nil
	}
	data, e := exec.CommandContext(ctx, "/usr/sbin/system_profiler", "SPDisplaysDataType", "-json", "-detailLevel", "mini").Output()
	if e != nil {
		return nil, fmt.Errorf("GPU discovery: %w", e)
	}
	var result struct {
		Displays []struct {
			Name string `json:"sppci_model"`
		} `json:"SPDisplaysDataType"`
	}
	if e = json.Unmarshal(data, &result); e != nil {
		return nil, e
	}
	for i, d := range result.Displays {
		if d.Name != "" {
			b.fallback = append(b.fallback, EmptyGPU(fmt.Sprintf("apple-%d", i), d.Name, "system_profiler"))
		}
	}
	b.discovered = true
	return append([]GPU(nil), b.fallback...), nil
}
