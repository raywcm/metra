package platform

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ascendTable = `+---------------------------+---------------+----------------------------------------------------+
| npu-smi 25.5.0 Version: 25.5.0 |
| NPU   Name                | Health        | Power(W)    Temp(C)           Hugepages-Usage(page)|
| Chip                      | Bus-Id        | AICore(%)   Memory-Usage(MB)  HBM-Usage(MB)        |
| 4     910B3               | OK            | 92.1        38                0    / 0             |
| 0                         | 0000:0A:00.0  | 0           0    / 0          3454 / 65536         |
+---------------------------+---------------+----------------------------------------------------+
| NPU     Chip              | Process id    | Process name             | Process memory(MB)      |
| No running processes found in NPU 4 |
`

const ascendUsages = `HBM Capacity(MB) : 65536
Aicore Usage Rate(%) : 32
Aivector Usage Rate(%) : 12
Aicpu Usage Rate(%) : 7
Ctrlcpu Usage Rate(%) : 2
HBM Bandwidth Usage Rate(%) : 4
`

func TestAscendTableAndEngines(t *testing.T) {
	devices, err := parseAscendSummary(ascendTable)
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	n := &devices[0]
	if n.ID != "ascend:4:0" || !strings.Contains(n.Name, "910B3") || n.MemoryKind != "HBM" || n.MemoryUsed != 3454*1024*1024 || n.MemoryTotal != 65536*1024*1024 || n.Power != 92.1 || n.Temperature != 38 {
		t.Fatalf("wrong Ascend metrics: %+v", n)
	}
	applyAscendUsages(n, ascendUsages)
	if n.Utilization != 32 || len(n.Engines) != 5 || n.Engines[1].ID != "aivector" || n.Engines[1].Utilization != 12 {
		t.Fatal(n)
	}
	if _, err := parseAscendSummary("npu-smi: driver unavailable"); err == nil {
		t.Fatal("driver failure accepted as no devices")
	}
	multi := strings.Replace(ascendTable, "+---------------------------+---------------+----------------------------------------------------+\n| NPU     Chip", "| 1 | 0000:0B:00.0 | 14 0 / 0 100 / 65536 |\n| NPU     Chip", 1)
	devices, err = parseAscendSummary(multi)
	if err != nil || len(devices) != 2 || devices[1].ID != "ascend:4:1" || devices[1].Utilization != 14 {
		t.Fatal(devices, err)
	}
	ddr := strings.Replace(ascendTable, "3454 / 65536", "0 / 0", 1)
	ddr = strings.Replace(ddr, "0    / 0          0 / 0", "1024 / 8192       0 / 0", 1)
	devices, err = parseAscendSummary(ddr)
	if err != nil || devices[0].MemoryTotal != 8192*1024*1024 || devices[0].MemoryKind != "Device" {
		t.Fatal(devices, err)
	}
}

func TestAscendChildLibrariesAndSampling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "npu-smi")
	script := "#!/bin/sh\ncase \"$LD_LIBRARY_PATH\" in *\"$METRA_TEST_LIBRARY_DIR\"*) ;; *) exit 19;; esac\nif [ \"$3\" = usages ]; then\ncat <<'USAGES'\n" + ascendUsages + "USAGES\nelse\ncat <<'TABLE'\n" + ascendTable + "TABLE\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("METRA_TEST_LIBRARY_DIR", dir)
	t.Setenv("LD_LIBRARY_PATH", "/existing/vendor/lib")
	b := ascendNPU{path: path, libraryDirs: []string{dir}}
	devices, err := b.Sample(context.Background())
	if err != nil || len(devices) != 1 || len(devices[0].Engines) != 5 {
		t.Fatal(devices, err)
	}
	if os.Getenv("LD_LIBRARY_PATH") != "/existing/vendor/lib" {
		t.Fatal("parent environment changed")
	}
}

func TestAscendPartialUsagesKeepSummaryCore(t *testing.T) {
	for _, tc := range []struct {
		detail string
		core   float64
		peak   float64
	}{{"", 78, 78}, {"N/A", 78, 78}, {"112", 78, 78}, {"0", 0, 7}} {
		summary := strings.Replace(ascendTable, "| 0           0    / 0", "| 78          0    / 0", 1)
		devices, err := parseAscendSummary(summary)
		if err != nil || len(devices) != 1 || devices[0].Utilization != 78 {
			t.Fatal("invalid test summary", devices, err)
		}
		usages := "Aicpu Usage Rate(%) : 7\n"
		if tc.detail != "" {
			usages += "Aicore Usage Rate(%) : " + tc.detail
		}
		applyAscendUsages(&devices[0], usages)
		g := devices[0]
		if len(g.Engines) != 2 || g.Engines[0].ID != "aicore" || g.Engines[0].Utilization != tc.core || g.Utilization != tc.peak {
			t.Errorf("detail %q lost the current core reading: %+v", tc.detail, g)
		}
	}
}

func TestSlowAscendSummaryLeavesTimeForOtherMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "npu-smi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 10\n"), 0700); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	b := ascendNPU{path: path}
	_, err := b.Sample(parent)
	if !errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
		t.Fatalf("summary timeout = %v, overall sampling deadline = %v", err, parent.Err())
	}
}

func TestNVIDIAExtendedEnginesAndClocks(t *testing.T) {
	query := append(append([]string(nil), nvidiaBaseQuery...), nvidiaOptionalQuery...)
	devices, err := parseNVIDIAFields("GPU-1, NVIDIA RTX, 25, 1024, 8192, 60, 150, 50, 7, 8, 9, 10, 1440, 1410, 9000, 1000\n", query)
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	g := devices[0]
	if g.Utilization != 50 || g.Engines[0].Utilization != 25 || len(g.Engines) != 6 || g.Engines[1].Utilization != 50 || g.Engines[5].ID != "ofa" || g.GraphicsMHz != 1440 || g.SMMHz != 1410 || g.MemoryMHz != 9000 || g.VideoMHz != 1000 {
		t.Fatal(g)
	}
	unknown, err := parseNVIDIAFields("GPU-1, NVIDIA RTX, 25, 1024, 8192, 60, 150, N/A, N/A, N/A, N/A, N/A, N/A, N/A, N/A, N/A\n", query)
	if err != nil || len(unknown[0].Engines) != 1 || !math.IsNaN(unknown[0].GraphicsMHz) {
		t.Fatal(unknown, err)
	}
	u := UnavailableGPU(g)
	if !math.IsNaN(u.Utilization) || !math.IsNaN(u.Engines[0].Utilization) || !math.IsNaN(u.GraphicsMHz) || u.ID != g.ID {
		t.Fatal("stale metrics became live values")
	}
}

func TestNVIDIADiscoversOptionalFieldsAndFallsBack(t *testing.T) {
	for _, fail := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "nvidia-smi")
		extended := "echo 'GPU-1, RTX, 25, 1024, 8192, 60, 150, 42, 1500'"
		if fail {
			extended = "exit 2"
		}
		script := "#!/bin/sh\ncase \"$1\" in\n--help-query-gpu) echo '\"utilization.memory\" \"clocks.current.sm\"';;\n*utilization.memory*) " + extended + ";;\n*) echo 'GPU-1, RTX, 25, 1024, 8192, 60, 150';;\nesac\n"
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		backend := nvidia{path: path}
		devices, err := backend.Sample(context.Background())
		expected := 42.0
		if fail {
			expected = 25
		}
		if err != nil || len(devices) != 1 || devices[0].Utilization != expected {
			t.Fatal(devices, err)
		}
		if !fail && (len(devices[0].Engines) != 2 || devices[0].SMMHz != 1500) {
			t.Fatal("optional fields were not queried", devices)
		}
		if fail && len(devices[0].Engines) != 1 {
			t.Fatal("invented fallback engines")
		}
	}
}

func TestNVIDIARecordsSurvivePartialDriverOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nvidia-smi")
	script := `#!/bin/sh
case "$1" in
--help-query-gpu) echo '"clocks.current.sm"' ;;
*clocks.current.sm*)
    echo 'GPU-A, "Model, A", 35, 128, 1024, 42, 90, 1400'
    echo 'unreadable device'
    echo 'GPU-B, Model B, 45, 128, 1024, 42, 90, 1250' ;;
*)
    echo 'GPU-A, Model A, 36, 128, 1024, 42, 90'
    echo 'GPU-C, Model C, 55, 128, 1024, 42, 90'
    printf 'GPU-D, Incomplete, 70, 128, 1024, 42, 1'
    exit 15 ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	backend := nvidia{path: path}
	devices, err := backend.Sample(context.Background())
	if err == nil || len(devices) != 3 {
		t.Fatalf("partial scan returned %v, %v", devices, err)
	}
	byID := map[string]GPU{}
	for _, g := range devices {
		byID[g.ID] = g
	}
	if byID["GPU-A"].Name != "Model, A" || byID["GPU-A"].SMMHz != 1400 || byID["GPU-B"].SMMHz != 1250 || byID["GPU-C"].Utilization != 55 {
		t.Fatalf("lost a healthy row or its extended counters: %+v", devices)
	}
	devices, err = backend.Sample(context.Background())
	if err == nil || len(devices) != 2 || devices[0].ID != "GPU-A" || devices[1].ID != "GPU-C" {
		t.Fatalf("basic query lost complete records or accepted an interrupted record: %v, %v", devices, err)
	}
}

func TestNVIDIABoundsAndMissingReadings(t *testing.T) {
	query := append(append([]string(nil), nvidiaBaseQuery...), "clocks.current.sm")
	devices, err := parseNVIDIAFields("GPU-A, Model, 103, inf, 1e308, 42, 0, -1", query)
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	g := devices[0]
	if !math.IsNaN(g.Utilization) || !math.IsNaN(g.SMMHz) || !math.IsNaN(g.MemoryUsed) || !math.IsNaN(g.MemoryTotal) || g.Power != 0 || g.Temperature != 42 {
		t.Fatalf("invalid or zero readings mishandled: %+v", g)
	}
	for _, bad := range []float64{math.Inf(1), math.Inf(-1), -1, 101} {
		if !math.IsNaN(PeakUtilization(bad, nil)) {
			t.Errorf("invalid aggregate utilization accepted: %v", bad)
		}
	}
}

func TestNVIDIARowsRequireDistinctDeviceIdentities(t *testing.T) {
	records := []string{
		" , unknown, 4, 10, 20, 30, 40",
		"[N/A], unknown, 8, 10, 20, 30, 40",
		"GPU-1, one, 15, 10, 20, 30, 40",
		"GPU-1, duplicate, 70, 10, 20, 30, 40",
		"GPU-2, two, 38, 10, 20, 30, 40",
	}
	devices, err := parseNVIDIA(strings.Join(records, "\n"))
	if err == nil || len(devices) != 2 || devices[0].Utilization != 15 || devices[1].ID != "GPU-2" {
		t.Fatal(devices, err)
	}
}

func TestVirtualAndManagementDisplaysAreNotGPUs(t *testing.T) {
	for _, item := range [][2]string{{"0x1af4", "virtio_gpu"}, {"0x1234", "bochs"}, {"0x1a03", "ast"}, {"0x102b", "mgag200"}, {"0x1414", "hyperv_drm"}, {"0x80ee", "vboxvideo"}, {"0x1111", "qxl"}} {
		if monitoringGPU(item[0], item[1]) {
			t.Fatal("display-only adapter exposed", item)
		}
	}
	for _, vendor := range []string{"0x10de", "0x1002", "0x8086"} {
		if !monitoringGPU(vendor, "physical") {
			t.Fatal("physical GPU filtered", vendor)
		}
	}
}

func TestOverallUsesTheBusiestEngine(t *testing.T) {
	engines := []Engine{{ID: "compute", Utilization: 13}, {ID: "memory", Utilization: 67}, {ID: "encoder", Utilization: 21}}
	if PeakUtilization(90, engines) != 67 {
		t.Fatal("overall should be the maximum engine, not the sum or driver aggregate")
	}
	engines[2].Utilization = 98
	if PeakUtilization(1, engines) != 98 {
		t.Fatal("media engines were ignored")
	}
	if !math.IsNaN(PeakUtilization(math.NaN(), []Engine{{Utilization: math.NaN()}, {Utilization: 120}})) {
		t.Fatal("unavailable engines became zero")
	}
	devices, err := parseAscendSummary(ascendTable)
	if err != nil {
		t.Fatal(err)
	}
	applyAscendUsages(&devices[0], strings.Replace(ascendUsages, "Ctrlcpu Usage Rate(%) : 2", "Ctrlcpu Usage Rate(%) : 83", 1))
	if devices[0].Utilization != 83 {
		t.Fatal("NPU overall ignored the busiest unit", devices[0])
	}
}

func TestMissingEngineCountersPreserveIdentityAndRemainUnknown(t *testing.T) {
	previous := []Engine{{ID: "aicore", Name: "AI Core", Utilization: 20}, {ID: "control", Name: "Control CPU", Utilization: 83}}
	current := []Engine{{ID: "aicore", Name: "AI Core", Utilization: 0}}
	merged := KeepKnownEngines(current, previous)
	if len(merged) != 2 || merged[1].ID != "control" || !math.IsNaN(merged[1].Utilization) {
		t.Fatal(merged)
	}
	if !math.IsNaN(PeakUtilization(0, merged)) {
		t.Fatal("missing busiest engine became zero")
	}
	if previous[1].Utilization != 83 {
		t.Fatal("previous sample mutated")
	}
	merged = KeepKnownEngines(current, previous, "control")
	if len(merged) != 1 || merged[0].ID != "aicore" {
		t.Fatal("confirmed absent engine restored from history", merged)
	}
}

func TestPCIeCapacityAndFullDuplexUtilization(t *testing.T) {
	if pcieCapacity(2, 16) != 8e9 {
		t.Fatal("PCIe 2 encoding incorrect")
	}
	capacity := pcieCapacity(5, 16)
	if math.Abs(capacity-63.015384615e9) > 1e3 {
		t.Fatal(capacity)
	}
	if linkPercent(capacity/2, capacity) != 50 {
		t.Fatal("PCIe occupancy incorrect")
	}
	if !math.IsNaN(pcieCapacity(6, 16)) || !math.IsNaN(linkPercent(100, math.NaN())) {
		t.Fatal("unknown link capacity guessed")
	}
}

func TestNVIDIASlowProbeStillCollectsBasicMetrics(t *testing.T) {
	for _, slow := range []string{"help", "extended"} {
		t.Run(slow, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "nvidia-smi")
			t.Setenv("PROBE_LOG", filepath.Join(dir, "calls"))
			t.Setenv("SLOW_PROBE", slow)
			script := `#!/bin/sh
case "$1" in
--help-query-gpu)
	echo help >> "$PROBE_LOG"
	if [ "$SLOW_PROBE" = help ]; then exec sleep 4; fi
	echo '"utilization.memory"' ;;
*utilization.memory*)
	echo extended >> "$PROBE_LOG"
	exec sleep 4 ;;
*) echo 'GPU-1, RTX, 25, 1024, 8192, 60, 150' ;;
esac
`
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			n := nvidia{path: path}
			for range 2 {
				devices, err := n.Sample(context.Background())
				if err != nil || len(devices) != 1 || devices[0].Utilization != 25 {
					t.Fatalf("basic reading lost: %+v %v", devices, err)
				}
			}
			log, err := os.ReadFile(os.Getenv("PROBE_LOG"))
			if err != nil || strings.Count(string(log), "help") != 1 || strings.Count(string(log), "extended") > 1 {
				t.Fatalf("failed probe repeated: %q, %v", log, err)
			}
		})
	}
}
