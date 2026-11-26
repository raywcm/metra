package platform

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
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

// Bound each subprocess separately so a slow optional probe cannot consume
// the basic query's timeout. The caller's deadline still bounds the sample.
func (n *nvidia) command(ctx context.Context, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, n.path, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if commandCtx.Err() != nil {
		return output, commandCtx.Err()
	}
	return output, err
}

func (n *nvidia) readDevices(ctx context.Context) ([]GPU, error) {
	output, commandErr := n.command(ctx, "--query-gpu="+strings.Join(n.query, ","), "--format=csv,noheader,nounits")
	if commandErr != nil {
		// A stopped command can leave a plausible but truncated final value.
		output = output[:bytes.LastIndexByte(output, '\n')+1]
	}
	devices, parseErr := parseNVIDIAFields(string(output), n.query)
	return devices, errors.Join(commandErr, parseErr)
}

func (n *nvidia) Sample(ctx context.Context) ([]GPU, error) {
	if n.query == nil || (!n.retryAfter.IsZero() && time.Now().After(n.retryAfter)) {
		n.retryAfter = time.Time{}
		n.query = append([]string(nil), nvidiaBaseQuery...)
		help, err := n.command(ctx, "--help-query-gpu")
		if err != nil {
			n.retryAfter = time.Now().Add(30 * time.Second)
		} else {
			for _, field := range nvidiaOptionalQuery {
				if strings.Contains(string(help), strconv.Quote(field)) {
					n.query = append(n.query, field)
				}
			}
		}
	}
	devices, err := n.readDevices(ctx)
	if err != nil && ctx.Err() == nil && len(n.query) > len(nvidiaBaseQuery) {
		n.retryAfter = time.Now().Add(30 * time.Second)
		n.query = append([]string(nil), nvidiaBaseQuery...)
		basic, basicErr := n.readDevices(ctx)
		// Keep optional counters from successful extended rows while recovering
		// other devices from the basic query.
		seen := make(map[string]bool, len(devices))
		for _, g := range devices {
			seen[g.ID] = true
		}
		for _, g := range basic {
			if !seen[g.ID] {
				devices = append(devices, g)
			}
		}
		err = basicErr
	}
	if err != nil {
		err = fmt.Errorf("nvidia-smi: %w", err)
	}
	if n.extra != nil && (len(devices) > 0 || err == nil) {
		n.extra.Collect(ctx, devices)
		// NVML may have removed an engine reported by the CSV query.
		// The GPU/compute reading is already an engine in its own right.
		for i := range devices {
			devices[i].Utilization = PeakUtilization(math.NaN(), devices[i].Engines)
		}
	}
	return devices, err
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
	if len(query) < len(nvidiaBaseQuery) {
		return nil, errors.New("incomplete GPU query")
	}
	reader := csv.NewReader(strings.NewReader(s))
	reader.FieldsPerRecord = len(query)
	reader.TrimLeadingSpace = true
	var out []GPU
	var firstErr error
	seen := make(map[string]bool)
	for {
		r, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err == nil {
			id := strings.TrimSpace(r[0])
			switch {
			case id == "" || id == "N/A" || strings.HasPrefix(id, "["):
				err = errors.New("GPU row has no UUID")
			case seen[id]:
				err = fmt.Errorf("duplicate GPU UUID %q", id)
			default:
				seen[id] = true
				out = append(out, parseNVIDIARow(id, r, query))
			}
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return out, firstErr
}

func parseNVIDIARow(id string, r, query []string) GPU {
	g := EmptyGPU(id, strings.TrimSpace(r[1]), "nvidia-smi")
	for i, field := range query[2:] {
		raw := strings.TrimSpace(r[i+2])
		switch {
		case strings.HasPrefix(field, "utilization."):
			raw = strings.TrimSuffix(raw, "%")
		case strings.HasPrefix(field, "clocks."):
			raw = strings.TrimSuffix(strings.ToLower(raw), "mhz")
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || v < 0 || math.IsInf(v, 0) {
			v = math.NaN()
		}
		switch field {
		case "utilization.gpu":
			g.Engines = addEngine(g.Engines, "compute", "GPU / compute", v)
		case "memory.used":
			g.MemoryUsed = nvidiaMemoryBytes(v)
		case "memory.total":
			g.MemoryTotal = nvidiaMemoryBytes(v)
		case "temperature.gpu":
			g.Temperature = v
		case "power.draw":
			g.Power = v
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
	g.Utilization = PeakUtilization(math.NaN(), g.Engines)
	return g
}

func nvidiaMemoryBytes(mib float64) float64 {
	if mib > math.MaxFloat64/(1024*1024) {
		return math.NaN()
	}
	return mib * (1024 * 1024)
}
