package platform

import (
	"context"
	"math"
	"strings"

	"github.com/shirou/gopsutil/v4/sensors"
)

type Temperature struct {
	Name    string
	Celsius float64
}

// Keep only identifiable CPU sensors: a GPU, DIMM or disk reading must never
// become the CPU temperature simply because it is the hottest sensor.
func cpuTemperatures(readings []sensors.TemperatureStat) []Temperature {
	var out []Temperature
	seen := map[string]bool{}
	for _, s := range readings {
		name := strings.ToLower(s.SensorKey)
		cpu := strings.Contains(name, "cpu") || strings.HasPrefix(name, "coretemp") ||
			strings.HasPrefix(name, "k10temp") || strings.HasPrefix(name, "zenpower") ||
			strings.HasPrefix(name, "pacc") || strings.HasPrefix(name, "eacc") || strings.HasPrefix(name, "tc0")
		if !cpu || seen[name] || math.IsNaN(s.Temperature) || math.IsInf(s.Temperature, 0) || s.Temperature <= 0 || s.Temperature > 150 {
			continue
		}
		seen[name] = true
		out = append(out, Temperature{Name: s.SensorKey, Celsius: s.Temperature})
	}
	return out
}

func CPUTemperatures(ctx context.Context) []Temperature {
	readings, _ := sensors.TemperaturesWithContext(ctx)
	out := cpuTemperatures(readings)
	if len(out) == 0 && ctx.Err() == nil {
		out = nativeCPUTemperatures(ctx)
	}
	return out
}
