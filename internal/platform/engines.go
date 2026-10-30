package platform

import (
	"math"
	"strconv"
	"strings"
)

type Engine struct {
	ID, Name    string
	Utilization float64
}

// Each engine measures a different resource. The busiest valid engine defines
// the overall percentage; percentages are never added or averaged together.
func PeakUtilization(fallback float64, engines []Engine) float64 {
	peak := math.NaN()
	for _, engine := range engines {
		v := engine.Utilization
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return math.NaN()
		}
		if math.IsNaN(peak) || v > peak {
			peak = v
		}
	}
	if !math.IsNaN(peak) {
		return peak
	}
	return fallback
}

// Preserve the identity and order of previously readable engines through a
// temporary query failure. Unknown counters remain gaps, rather than zero.
func KeepKnownEngines(current, previous []Engine, absent ...string) []Engine {
	excluded := map[string]bool{}
	for _, id := range absent {
		excluded[id] = true
	}
	remaining := map[string]Engine{}
	for _, engine := range current {
		if !excluded[engine.ID] {
			remaining[engine.ID] = engine
		}
	}
	out := make([]Engine, 0, max(len(current), len(previous)))
	for _, old := range previous {
		if excluded[old.ID] {
			continue
		}
		if engine, ok := remaining[old.ID]; ok {
			out = append(out, engine)
			delete(remaining, old.ID)
		} else {
			old.Utilization = math.NaN()
			out = append(out, old)
		}
	}
	for _, engine := range current {
		if _, ok := remaining[engine.ID]; ok {
			out = append(out, engine)
			delete(remaining, engine.ID)
		}
	}
	return out
}

func metricNumber(s string) float64 {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return math.NaN()
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], "%"), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}

func addEngine(out []Engine, id, name string, value float64) []Engine {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return out
	}
	return append(out, Engine{ID: id, Name: name, Utilization: value})
}

func unavailableEngines(in []Engine) []Engine {
	out := append([]Engine(nil), in...)
	for i := range out {
		out[i].Utilization = math.NaN()
	}
	return out
}

func UnavailableGPU(g GPU) GPU {
	n := EmptyGPU(g.ID, g.Name, g.Backend)
	n.Cores, n.UnifiedMemory = g.Cores, g.UnifiedMemory
	n.AbsentEngines = append([]string(nil), g.AbsentEngines...)
	n.Engines = unavailableEngines(g.Engines)
	return n
}

func UnavailableNPU(n NPU) NPU {
	out := EmptyNPU(n.ID, n.Name, n.Backend)
	out.Cores, out.MemoryKind = n.Cores, n.MemoryKind
	out.Engines = unavailableEngines(n.Engines)
	return out
}
