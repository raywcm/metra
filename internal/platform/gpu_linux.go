//go:build linux

package platform

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type linuxGPU struct{ nvidia GPUBackend }

func (b linuxGPU) Close() {
	if closer, ok := b.nvidia.(interface{ Close() }); ok {
		closer.Close()
	}
}

func NewGPU() GPUBackend { return linuxGPU{nvidiaBackend()} }
func readNumber(path string) float64 {
	b, e := os.ReadFile(path)
	if e != nil {
		return EmptyGPU("", "", "").Utilization
	}
	v, e := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if e != nil {
		return EmptyGPU("", "", "").Utilization
	}
	return v
}
func (b linuxGPU) Sample(ctx context.Context) ([]GPU, error) {
	var out []GPU
	var err error
	if b.nvidia != nil {
		out, err = b.nvidia.Sample(ctx)
	}
	paths, _ := filepath.Glob("/sys/class/drm/card[0-9]*")
	for _, p := range paths {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if strings.Contains(filepath.Base(p), "-") {
			continue
		}
		vendor, e := os.ReadFile(filepath.Join(p, "device/vendor"))
		if e != nil {
			continue
		}
		v := strings.TrimSpace(string(vendor))
		driver, _ := filepath.EvalSymlinks(filepath.Join(p, "device/driver"))
		if !monitoringGPU(v, filepath.Base(driver)) {
			continue
		}
		if v == "0x10de" && b.nvidia != nil {
			continue
		}
		name := map[string]string{"0x1002": "AMD", "0x8086": "Intel", "0x10de": "NVIDIA"}[v]
		if name == "" {
			name = v
		}
		device, _ := os.ReadFile(filepath.Join(p, "device/device"))
		real, _ := filepath.EvalSymlinks(filepath.Join(p, "device"))
		g := EmptyGPU(real, name+" "+strings.TrimSpace(string(device)), "DRM sysfs")
		g.Utilization = readNumber(filepath.Join(p, "device/gpu_busy_percent"))
		g.Engines = addEngine(g.Engines, "compute", "GPU / compute", g.Utilization)
		g.Engines = addEngine(g.Engines, "memory", "Memory controller", readNumber(filepath.Join(p, "device/mem_busy_percent")))
		g.GraphicsMHz = readDRMClock(filepath.Join(p, "device/pp_dpm_sclk"))
		g.MemoryMHz = readDRMClock(filepath.Join(p, "device/pp_dpm_mclk"))
		if math.IsNaN(g.GraphicsMHz) {
			g.GraphicsMHz = readNumber(filepath.Join(p, "gt_cur_freq_mhz"))
		}
		g.MemoryUsed = readNumber(filepath.Join(p, "device/mem_info_vram_used"))
		g.MemoryTotal = readNumber(filepath.Join(p, "device/mem_info_vram_total"))
		temps, _ := filepath.Glob(filepath.Join(p, "device/hwmon/hwmon*/temp1_input"))
		if len(temps) > 0 {
			g.Temperature = readNumber(temps[0]) / 1000
		}
		g.Utilization = PeakUtilization(g.Utilization, g.Engines)
		out = append(out, g)
	}
	return out, err
}
