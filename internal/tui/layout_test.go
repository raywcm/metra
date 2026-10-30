package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raywcm/metra/internal/platform"
)

func TestResponsivePanelsFillAndDegrade(t *testing.T) {
	m, _ := model(t)
	var panels []metricPanel
	for i := 0; i < 6; i++ {
		panels = append(panels, metricPanel{labels: []string{fmt.Sprintf("Metric %d 12.5%%", i)}, keys: []string{"cpu"}, style: m.palette().cpu, priority: i})
	}
	previous := 0
	for _, height := range []int{3, 8, 12, 20, 36, 59} {
		out := ansi.Strip(m.panelLayout(panels, 101, height, 2))
		graphs := strings.Count(out, "┌")
		if graphs < previous {
			t.Fatal("larger viewport hid graphs", height, graphs, previous)
		}
		previous = graphs
		if graphs > 0 && lineCount(out) != height {
			t.Fatalf("height %d did not consume its exact budget: %d", height, lineCount(out))
		}
		if graphs != strings.Count(out, "└") {
			t.Fatal("partial graph frame", out)
		}
		for i := 0; i < 6; i++ {
			if !strings.Contains(out, fmt.Sprintf("Metric %d", i)) {
				t.Fatal("hidden graph lost its numeric value", i, out)
			}
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "┌") && ansi.StringWidth(line) != 101 {
				t.Fatal("row did not fill odd terminal width", line)
			}
		}
	}
	if previous != 6 {
		t.Fatal("large viewport failed to restore every graph")
	}
	if strings.Contains(m.panelLayout(panels, 20, 50, 2), "┌") {
		t.Fatal("very narrow screen should use numeric metrics")
	}
	if out := m.panelLayout(panels[:1], 120, 380, 1); lineCount(out) != 380 {
		t.Fatal("tall terminal was silently clipped", lineCount(out))
	}
}

func TestExactScreenAndCompleteFramesWithRichHardware(t *testing.T) {
	m, _ := model(t)
	s := fixture()
	s.CPU.MHz, s.CPU.Temperature, s.CPU.TemperatureKnown = 2400, 65, true
	g := platform.EmptyGPU("rich", "Long device name NVIDIA H200", "test")
	g.MemoryUsed, g.MemoryTotal, g.Utilization = 3<<30, 8<<30, 70
	g.GraphicsMHz, g.SMMHz, g.MemoryMHz, g.VideoMHz = 1440, 1440, 9000, 1000
	g.Temperature, g.Power = 60, 150
	g.Engines = []platform.Engine{{ID: "compute", Name: "GPU / compute", Utilization: 70}, {ID: "memory", Name: "Memory controller", Utilization: 40}, {ID: "hbm", Name: "HBM bandwidth", Utilization: 30}, {ID: "encoder", Name: "Encoder", Utilization: 0}, {ID: "decoder", Name: "Decoder", Utilization: 0}, {ID: "jpeg", Name: "JPEG", Utilization: 0}, {ID: "ofa", Name: "Optical flow", Utilization: 0}, {ID: "pcie-rx", Name: "PCIe receive", Utilization: 10}, {ID: "pcie-tx", Name: "PCIe send", Utilization: 2}}
	g.PCIeReceive, g.PCIeSend, g.PCIeCapacity = 1<<30, 1<<20, 10<<30
	s.GPUs = nil
	for i := 0; i < 8; i++ {
		device := g
		device.ID = fmt.Sprint(i)
		s.GPUs = append(s.GPUs, device)
	}
	m.Update(sampleMsg{snapshot: s})
	for _, lang := range []string{"en", "zh-CN"} {
		m.settings.Language = lang
		for _, size := range [][2]int{{1, 1}, {12, 3}, {20, 5}, {24, 8}, {40, 12}, {41, 19}, {70, 24}, {80, 24}, {101, 37}, {120, 36}, {200, 60}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := range m.tabs {
				m.tab = i
				out := ansi.Strip(m.View().Content)
				if lineCount(out) != size[1] {
					t.Fatalf("%s %v: screen height %d", m.current().Kind, size, lineCount(out))
				}
				for _, line := range strings.Split(out, "\n") {
					if ansi.StringWidth(line) != size[0] {
						t.Fatalf("%s %v: row width %d: %q", m.current().Kind, size, ansi.StringWidth(line), line)
					}
				}
				if strings.Count(out, "┌") != strings.Count(out, "└") {
					t.Fatalf("%s %v: graph clipped\n%s", m.current().Kind, size, out)
				}
				if size[0] < 24 && strings.ContainsAny(out, "┌└") {
					t.Fatal("tiny viewport retained graphics")
				}
				if strings.Contains(out, "enlarge terminal") {
					t.Fatal("small viewport should retain live numbers")
				}
			}
		}
	}
}

func TestRoundedBarsDoNotInventTinyUsage(t *testing.T) {
	for _, width := range []int{1, 17, 40, 118} {
		for _, tc := range []struct {
			used float64
			want int
		}{{0, 0}, {0.00001, 0}, {0.49 * 100 / float64(width), 0}, {0.5 * 100 / float64(width), 1}, {100, width}} {
			parts := roundedBarWidths([]float64{tc.used, 100 - tc.used}, 100, width)
			if parts[0] != tc.want || parts[0]+parts[1] != width {
				t.Fatal("incorrect rounded composition", width, tc, parts)
			}
		}
	}
	if got := roundedBarWidths([]float64{33.3, 33.3, 33.4}, 100, 10); got[0]+got[1]+got[2] != 10 {
		t.Fatal("bar widths lost cells", got)
	}
	if got := roundedBarWidths([]float64{math.NaN(), 100}, 100, 10); got[0] != 0 || got[1] != 10 {
		t.Fatal("unknown usage colored the bar", got)
	}
	if got := roundedBarWidths([]float64{26.499999, 0.000002, 73.499999}, 100, 100); got[1] != 0 || got[0]+got[1]+got[2] != 100 {
		t.Fatal("a tiny internal segment became a colored sliver", got)
	}
}

func TestCPUReadingsSurviveNumericMode(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	m.snapshot.CPU.Temperature, m.snapshot.CPU.TemperatureKnown = 65, true
	if !strings.Contains(ansi.Strip(m.cpuView(118, 30)), "CPU temperature 65.0 °C") {
		t.Fatal("readable CPU temperature missing")
	}
	m.snapshot.CPU.Cores = make([]float64, 192)
	if out := ansi.Strip(m.cpuView(24, 5)); !strings.Contains(out, "CPU 191") || strings.Contains(out, "┌") {
		t.Fatal("small numeric mode lost CPU readings", out)
	}
	m.snapshot.CPU.TemperatureKnown = false
	if strings.Contains(m.cpuView(118, 30), "65.0 °C") {
		t.Fatal("unavailable temperature retained stale data")
	}
}

func TestProcessViewportUsesAvailableRows(t *testing.T) {
	m, _ := model(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.snapshot.ProcessesAt = time.Now()
	for i := 0; i < 100; i++ {
		r := m.rows[0]
		r.PID = int32(i + 1)
		m.rows = append(m.rows, r)
	}
	_, height, _ := bodySize(80, 24)
	if got := lineCount(m.processView(78, height)); got != height {
		t.Fatal("process table leaves unused rows", got, height)
	}
	m.cursor = len(m.rows) - 1
	m.clampCursor()
	if m.cursor-m.offset >= m.processRowsVisible() {
		t.Fatal("selected process left the visible viewport")
	}
}

// Value growth must not change the graph count, header rows or frame heights.
func TestLiveNumbersKeepLayoutStable(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		for _, width := range []int{40, 56, 70, 84, 100, 120, 160} {
			for _, height := range []int{24, 36, 45} {
				m, _ := model(t)
				m.settings.Language = lang
				s := fixture()
				s.CPU.TemperatureKnown = true
				s.CPU.MHz, s.CPU.MinMHz, s.CPU.MaxMHz = 2400, 800, 3400
				s.CPU.Load = [3]float64{9.99, 9.99, 9.99}
				s.Memory.UsageKnown, s.Memory.AvailableKnown = true, true
				s.Memory.CachedKnown, s.Memory.SwapKnown, s.Memory.ModifiedKnown = true, true, true
				s.Memory.SwapTotal = 16 << 30
				s.GPUs = nil
				for i := 0; i < 8; i++ {
					g := platform.EmptyGPU(fmt.Sprint(i), "NVIDIA GeForce RTX 5090", "nvidia-smi")
					g.MemoryTotal, g.MemoryUsed, g.Utilization = 32<<30, 1<<30, 9.9
					g.GraphicsMHz, g.SMMHz, g.MemoryMHz, g.VideoMHz = 999.9, 999.9, 999.9, 999.9
					g.Temperature, g.Power = 9.9, 99.9
					g.PCIeCapacity = 64 << 30
					g.Engines = []platform.Engine{{ID: "compute", Name: "GPU / compute", Utilization: 9.9}, {ID: "memory", Name: "Memory controller", Utilization: 9.9}, {ID: "hbm", Name: "HBM bandwidth", Utilization: 9.9}, {ID: "pcie-rx", Name: "PCIe receive", Utilization: 9.9}, {ID: "pcie-tx", Name: "PCIe send", Utilization: 9.9}}
					s.GPUs = append(s.GPUs, g)
				}
				m.Update(sampleMsg{snapshot: s})
				want := map[string]string{}
				for _, reading := range []float64{9.9, 10, 99.9, 100} {
					s.CPU.Percent, s.CPU.Temperature = reading, reading
					s.CPU.MHz = reading * 100
					s.CPU.Load = [3]float64{reading, reading, reading}
					s.Memory.Percent = reading
					s.Memory.Used = uint64(reading * float64(1<<28))
					s.Memory.Available = s.Memory.Total - s.Memory.Used
					s.Memory.Free, s.Memory.Reclaimable, s.Memory.Cached = uint64(reading*100), uint64(reading*1024), uint64(reading*1024*1024)
					s.Disks[0].CombinedBusy, s.Disks[0].ReadBusy, s.Disks[0].WriteBusy = reading, reading, reading
					s.Disks[0].ReadRate, s.Disks[0].WriteRate = reading*1024, reading*1024*1024
					s.Networks[0].ReceiveRate, s.Networks[0].SendRate = reading*1024, reading*1024*1024
					for i := range s.GPUs {
						g := &s.GPUs[i]
						g.Utilization, g.MemoryUsed = reading, reading*float64(1<<28)
						g.GraphicsMHz, g.SMMHz, g.MemoryMHz, g.VideoMHz = reading*100, reading*100, reading*100, reading*100
						g.Temperature, g.Power = reading, reading*10
						g.PCIeReceive, g.PCIeSend = reading*1024, reading*1024*1024
						for j := range g.Engines {
							g.Engines[j].Utilization = reading
						}
					}
					m.snapshot = s
					views := map[string]string{"overview": m.overview(width, height), "cpu": m.cpuView(width, height), "memory": m.memoryView(width, height), "disk": m.diskView(0, width, height), "gpu": m.gpuView(0, width, height), "network": m.networkView(width, height)}
					for page, out := range views {
						var frameRows []string
						for row, line := range strings.Split(ansi.Strip(out), "\n") {
							if strings.ContainsAny(line, "┌└") {
								frameRows = append(frameRows, fmt.Sprintf("%d:%d:%d", row, strings.Count(line, "┌"), strings.Count(line, "└")))
							}
						}
						signature := strings.Join(frameRows, ",") + fmt.Sprintf("/lines:%d", lineCount(out))
						if previous, ok := want[page]; ok && signature != previous {
							t.Fatalf("%s %s %dx%d at %.1f changed layout: %s -> %s", lang, page, width, height, reading, previous, signature)
						}
						want[page] = signature
					}
				}
			}
		}
	}
}

func TestRateUnitsKeepMetricRowsStable(t *testing.T) {
	for width := 20; width <= 120; width++ {
		want := -1
		for _, value := range []float64{0, 9, 999, 1023, 1024, 1024*1024 - 1, 1024 * 1024, 10.0 * (1 << 30), 100.0 * (1 << 30)} {
			lines := metricLines([]string{"Read " + speed(value), "Write " + speed(value)}, width)
			if want >= 0 && len(lines) != want {
				t.Fatalf("width %d, rate %s: rows %d -> %d", width, speed(value), want, len(lines))
			}
			want = len(lines)
		}
	}
}
