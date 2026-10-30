package platform

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Unavailable metrics are NaN; discovering a device never implies telemetry support.
type GPU struct {
	PCIeReceive, PCIeSend, PCIeCapacity                      float64
	Engines                                                  []Engine
	AbsentEngines                                            []string
	GraphicsMHz, SMMHz, MemoryMHz, VideoMHz                  float64
	Renderer, Tiler                                          float64
	Cores                                                    int
	UnifiedMemory                                            bool
	ID, Name, Backend                                        string
	Utilization, MemoryUsed, MemoryTotal, Temperature, Power float64
}
type GPUBackend interface {
	Sample(context.Context) ([]GPU, error)
}

func EmptyGPU(id, name, backend string) GPU {
	n := math.NaN()
	return GPU{ID: id, Name: name, Backend: backend, Utilization: n, MemoryUsed: n, MemoryTotal: n, Temperature: n, Power: n, Renderer: n, Tiler: n, GraphicsMHz: n, SMMHz: n, MemoryMHz: n, VideoMHz: n, PCIeReceive: n, PCIeSend: n, PCIeCapacity: n}
}

type nvidia struct {
	path       string
	query      []string
	retryAfter time.Time
	extra      nvidiaExtra
}

type nvidiaExtra interface {
	Collect(context.Context, []GPU)
	Close()
}

var nvidiaBaseQuery = []string{"uuid", "name", "utilization.gpu", "memory.used", "memory.total", "temperature.gpu", "power.draw"}
var nvidiaOptionalQuery = []string{"utilization.memory", "utilization.encoder", "utilization.decoder", "utilization.jpeg", "utilization.ofa", "clocks.current.graphics", "clocks.current.sm", "clocks.current.memory", "clocks.current.video"}

func nvidiaBackend() GPUBackend {
	p, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	return &nvidia{path: p, extra: newNvidiaExtra()}
}
func (n *nvidia) Sample(ctx context.Context) ([]GPU, error) {
	// Bound external commands independently of per-device native reads. With
	// many GPUs, NVML must retain the overall collection budget.
	commandCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	if n.query == nil || (!n.retryAfter.IsZero() && time.Now().After(n.retryAfter)) {
		n.retryAfter = time.Time{}
		n.query = append([]string(nil), nvidiaBaseQuery...)
		if help, err := exec.CommandContext(commandCtx, n.path, "--help-query-gpu").Output(); err == nil {
			for _, field := range nvidiaOptionalQuery {
				if strings.Contains(string(help), strconv.Quote(field)) {
					n.query = append(n.query, field)
				}
			}
		}
	}
	b, err := exec.CommandContext(commandCtx, n.path, "--query-gpu="+strings.Join(n.query, ","), "--format=csv,noheader,nounits").Output()
	if commandCtx.Err() != nil {
		n.query = nil
		return nil, commandCtx.Err()
	}
	var devices []GPU
	if err == nil {
		devices, err = parseNVIDIAFields(string(b), n.query)
	}
	// Preserve basic monitoring if an older driver or virtualized tool rejects
	// optional fields that it advertised in its help output.
	if err != nil && len(n.query) > len(nvidiaBaseQuery) {
		n.retryAfter = time.Now().Add(30 * time.Second)
		n.query = append([]string(nil), nvidiaBaseQuery...)
		b, err = exec.CommandContext(commandCtx, n.path, "--query-gpu="+strings.Join(n.query, ","), "--format=csv,noheader,nounits").Output()
		if err == nil {
			devices, err = parseNVIDIAFields(string(b), n.query)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}
	if n.extra != nil {
		n.extra.Collect(ctx, devices)
	}
	for i := range devices {
		devices[i].Utilization = PeakUtilization(devices[i].Utilization, devices[i].Engines)
	}
	return devices, nil
}

func (n *nvidia) Close() {
	if n.extra != nil {
		n.extra.Close()
	}
}
func parseNVIDIA(s string) ([]GPU, error) {
	return parseNVIDIAFields(s, nvidiaBaseQuery)
}

func parseNVIDIAFields(s string, query []string) ([]GPU, error) {
	rows, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]GPU, 0, len(rows))
	for _, r := range rows {
		if len(r) != len(query) || len(r) < 7 {
			return nil, fmt.Errorf("invalid GPU row: expected %d fields", len(query))
		}
		g := EmptyGPU(strings.TrimSpace(r[0]), strings.TrimSpace(r[1]), "nvidia-smi")
		vals := []*float64{&g.Utilization, &g.MemoryUsed, &g.MemoryTotal, &g.Temperature, &g.Power}
		for i, p := range vals {
			v, e := strconv.ParseFloat(strings.TrimSpace(r[i+2]), 64)
			if e == nil && v >= 0 {
				*p = v
			}
		}
		if !math.IsNaN(g.MemoryUsed) {
			g.MemoryUsed *= 1024 * 1024
		}
		if !math.IsNaN(g.MemoryTotal) {
			g.MemoryTotal *= 1024 * 1024
		}
		g.Engines = addEngine(g.Engines, "compute", "GPU / compute", g.Utilization)
		for i, field := range query {
			v := metricNumber(r[i])
			switch field {
			case "utilization.memory":
				g.Engines = addEngine(g.Engines, "memory", "Memory controller", v)
			case "utilization.encoder":
				g.Engines = addEngine(g.Engines, "encoder", "Encoder", v)
			case "utilization.decoder":
				g.Engines = addEngine(g.Engines, "decoder", "Decoder", v)
			case "utilization.jpeg":
				g.Engines = addEngine(g.Engines, "jpeg", "JPEG", v)
			case "utilization.ofa":
				g.Engines = addEngine(g.Engines, "ofa", "Optical flow", v)
			case "clocks.current.graphics":
				g.GraphicsMHz = v
			case "clocks.current.sm":
				g.SMMHz = v
			case "clocks.current.memory":
				g.MemoryMHz = v
			case "clocks.current.video":
				g.VideoMHz = v
			}
		}
		g.Utilization = PeakUtilization(g.Utilization, g.Engines)
		out = append(out, g)
	}
	return out, nil
}
