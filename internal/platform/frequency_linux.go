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

func ReadCPUFrequency(ctx context.Context, ids []int) CPUFrequency {
	return readCPUFrequency(ctx, "/sys/devices/system/cpu/cpufreq", "/proc/cpuinfo", ids)
}

func readCPUFrequency(ctx context.Context, root, procInfo string, ids []int) CPUFrequency {
	allowed := map[int]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	values := map[int]float64{}
	policies, _ := filepath.Glob(filepath.Join(root, "policy*"))
	for _, policy := range policies {
		if ctx.Err() != nil {
			break
		}
		current := readNumber(filepath.Join(policy, "scaling_cur_freq")) / 1000
		if math.IsNaN(current) || current <= 0 {
			current = readNumber(filepath.Join(policy, "cpuinfo_cur_freq")) / 1000
		}
		if math.IsNaN(current) || current <= 0 {
			continue
		}
		cpus, err := os.ReadFile(filepath.Join(policy, "affected_cpus"))
		if err != nil {
			cpus, _ = os.ReadFile(filepath.Join(policy, "related_cpus"))
		}
		for _, field := range strings.Fields(string(cpus)) {
			id, err := strconv.Atoi(field)
			if err == nil && (len(allowed) == 0 || allowed[id]) {
				values[id] = current
			}
		}
	}
	// x86 kernels often expose current MHz here when cpufreq is unavailable.
	if len(values) == 0 {
		b, _ := os.ReadFile(procInfo)
		id := -1
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "processor":
				id, _ = strconv.Atoi(strings.TrimSpace(value))
			case "cpu MHz":
				mhz := metricNumber(value)
				if id >= 0 && (len(allowed) == 0 || allowed[id]) && mhz > 0 {
					values[id] = mhz
				}
			}
		}
	}
	result := CPUFrequency{MHz: math.NaN(), MinMHz: math.NaN(), MaxMHz: math.NaN()}
	var total float64
	for _, value := range values {
		total += value
		if math.IsNaN(result.MinMHz) || value < result.MinMHz {
			result.MinMHz = value
		}
		if math.IsNaN(result.MaxMHz) || value > result.MaxMHz {
			result.MaxMHz = value
		}
	}
	if len(values) > 0 {
		result.MHz = total / float64(len(values))
	}
	return result
}
