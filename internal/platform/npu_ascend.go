package platform

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ascendNPU struct {
	path        string
	libraryDirs []string
}

func newAscendNPU() NPUBackend {
	path, err := exec.LookPath("npu-smi")
	if err != nil {
		for _, candidate := range []string{"/usr/local/bin/npu-smi", "/usr/local/Ascend/driver/tools/npu-smi"} {
			if st, e := os.Stat(candidate); e == nil && !st.IsDir() && st.Mode()&0111 != 0 {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		return nil
	}
	return &ascendNPU{path: path, libraryDirs: []string{"/usr/local/Ascend/driver/lib64/common", "/usr/local/Ascend/driver/lib64/driver", "/usr/local/Ascend/driver/lib64"}}
}

func (a *ascendNPU) command(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, a.path, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = ascendEnvironment(os.Environ(), a.libraryDirs)
	b, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(b))
		if len(message) > 240 {
			message = message[:240]
		}
		return nil, fmt.Errorf("npu-smi: %w: %s", err, message)
	}
	return b, nil
}

// Vendor libraries are added only to the child command, never to the shell,
// ldconfig, or the metra process. A fresh SSH session therefore needs no setup.
func ascendEnvironment(env, dirs []string) []string {
	out := make([]string, 0, len(env)+1)
	var paths []string
	for _, item := range env {
		if value, ok := strings.CutPrefix(item, "LD_LIBRARY_PATH="); ok {
			if value != "" {
				paths = append(paths, value)
			}
		} else {
			out = append(out, item)
		}
	}
	for _, dir := range dirs {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			paths = append(paths, dir)
		}
	}
	if len(paths) > 0 {
		out = append(out, "LD_LIBRARY_PATH="+strings.Join(paths, string(os.PathListSeparator)))
	}
	return out
}

func (a *ascendNPU) Sample(ctx context.Context) ([]NPU, error) {
	b, err := a.command(ctx, "info")
	if err != nil {
		return nil, err
	}
	devices, err := parseAscendSummary(string(b))
	if err != nil {
		return nil, err
	}
	// A small worker pool bounds collection time on multi-card systems.
	jobs := make(chan int)
	queryErrors := make([]error, len(devices))
	var workers sync.WaitGroup
	for worker := 0; worker < min(4, len(devices)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				ids := strings.Split(strings.TrimPrefix(devices[i].ID, "ascend:"), ":")
				queryCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
				data, e := a.command(queryCtx, "info", "-t", "usages", "-i", ids[0], "-c", ids[1])
				cancel()
				if e == nil {
					applyAscendUsages(&devices[i], string(data))
				} else {
					queryErrors[i] = fmt.Errorf("%s engines: %w", devices[i].Name, e)
				}
			}
		}()
	}
	for i := range devices {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	return devices, errors.Join(queryErrors...)
}

var ascendPairs = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?|N/A|NA)\s*/\s*([0-9]+(?:\.[0-9]+)?|N/A|NA)`)

func tableCells(line string) []string {
	if !strings.HasPrefix(strings.TrimSpace(line), "|") {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(line), "|")
	var cells []string
	for _, part := range parts[1 : len(parts)-1] {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func parseAscendSummary(s string) ([]NPU, error) {
	var out []NPU
	var pending *NPU
	board := ""
	for _, line := range strings.Split(s, "\n") {
		cells := tableCells(line)
		if len(cells) < 3 {
			continue
		}
		first := strings.Fields(cells[0])
		if len(first) == 0 {
			continue
		}
		if _, err := strconv.Atoi(first[0]); err != nil {
			continue
		}
		// Device rows have a name after the physical card ID. The next row
		// begins with the chip ID and has a PCI bus address in the middle.
		if !strings.Contains(cells[1], ":") && len(first) >= 2 {
			board = first[0]
			name := strings.Join(first[1:], " ")
			if strings.EqualFold(name, "Mcu") {
				pending = nil
				continue
			}
			if !strings.HasPrefix(name, "Ascend ") {
				name = "Ascend " + name
			}
			n := EmptyNPU("", name, "npu-smi")
			values := strings.Fields(cells[2])
			if len(values) >= 2 {
				n.Power, n.Temperature = metricNumber(values[0]), metricNumber(values[1])
				if n.Power < 0 {
					n.Power = math.NaN()
				}
			}
			pending = &n
			continue
		}
		if pending == nil || !strings.Contains(cells[1], ":") {
			continue
		}
		n := *pending
		n.ID = "ascend:" + board + ":" + first[0]
		n.Name += " · " + board + ":" + first[0]
		values := strings.Fields(cells[2])
		if len(values) > 0 {
			n.Utilization = metricNumber(values[0])
		}
		if n.Utilization < 0 || n.Utilization > 100 {
			n.Utilization = math.NaN()
		}
		n.Engines = addEngine(n.Engines, "aicore", "AI Core", n.Utilization)
		pairs := ascendPairs.FindAllStringSubmatch(cells[2], -1)
		if len(pairs) > 0 {
			pair := pairs[0]
			n.MemoryKind = "Device"
			if len(pairs) > 1 && metricNumber(pairs[len(pairs)-1][2]) > 0 {
				pair = pairs[len(pairs)-1]
				n.MemoryKind = "HBM"
			}
			n.MemoryUsed, n.MemoryTotal = metricNumber(pair[1])*1024*1024, metricNumber(pair[2])*1024*1024
		}
		out = append(out, n)
	}
	if len(out) == 0 && !strings.Contains(strings.ToLower(s), "no device") && !strings.Contains(strings.ToLower(s), "no npu") {
		return nil, fmt.Errorf("unrecognized npu-smi device table")
	}
	return out, nil
}

func applyAscendUsages(n *NPU, s string) {
	engines := []Engine{}
	values := map[string]float64{}
	for _, line := range strings.Split(s, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = metricNumber(value)
		}
	}
	for _, spec := range [][3]string{{"Aicore Usage Rate(%)", "aicore", "AI Core"}, {"Aivector Usage Rate(%)", "aivector", "AI Vector"}, {"Aicpu Usage Rate(%)", "aicpu", "AI CPU"}, {"Ctrlcpu Usage Rate(%)", "control", "Control CPU"}, {"HBM Bandwidth Usage Rate(%)", "hbm", "HBM bandwidth"}} {
		if value, ok := values[spec[0]]; ok {
			engines = addEngine(engines, spec[1], spec[2], value)
		}
	}
	if len(engines) > 0 {
		n.Engines = engines
	}
	n.Utilization = PeakUtilization(n.Utilization, n.Engines)
}
