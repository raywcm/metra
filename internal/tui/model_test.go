package tui

import (
	"context"
	"image/color"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raywcm/metra/internal/collector"
	"github.com/raywcm/metra/internal/config"
	"github.com/raywcm/metra/internal/platform"
)

type fakeSource struct{ killed int }

func (s *fakeSource) Sample(context.Context, bool) collector.Snapshot { return fixture() }
func (s *fakeSource) Detail(_ context.Context, p collector.Process) (collector.Detail, error) {
	return collector.Detail{Process: p, Command: "sleep 10"}, nil
}
func (s *fakeSource) Terminate(context.Context, collector.Process) error { s.killed++; return nil }
func fixture() collector.Snapshot {
	now := time.Now()
	s := collector.Snapshot{At: now, Host: "test-host", CPU: collector.CPU{Name: "Example CPU", Logical: 16, Physical: 8, Percent: 31, Cores: make([]float64, 16)}, Memory: collector.Memory{Total: 32 << 30, Used: 10 << 30, Available: 22 << 30, Modified: 1 << 30, Reclaimable: 8 << 30, Free: 14 << 30, Percent: 31.25}, Disks: []collector.Disk{{ID: "disk0", Name: "disk0", ReadRate: 2 << 20, WriteRate: 1 << 20}}, Networks: []collector.Network{{Name: "en0", Up: true, ReceiveRate: 42, SendRate: 23}}, GPUs: []platform.GPU{platform.EmptyGPU("g0", "Example GPU", "test")}, NPUs: []platform.NPU{platform.EmptyNPU("ane0", "Apple Neural Engine", "test")}, ProcessCount: 3, ProcessesAt: now}
	s.Processes = []collector.Process{{PID: 1, Name: "init", CPU: 0.1}, {PID: 2, PPID: 1, Name: "worker", CPU: 12, RSS: 40 << 20, Created: 100}, {PID: 3, PPID: 2, Name: "child", CPU: 2}}
	return s
}
func key(s string) tea.KeyPressMsg {
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	codes := map[string]rune{"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown, "enter": tea.KeyEnter, "esc": tea.KeyEscape}
	return tea.KeyPressMsg{Code: codes[s]}
}
func model(t *testing.T) (*Model, *fakeSource) {
	t.Helper()
	f := &fakeSource{}
	m := New(context.Background(), f, config.Defaults(), t.TempDir()+"/settings.json")
	m.Update(sampleMsg{snapshot: fixture(), elapsed: time.Millisecond})
	return m, f
}
func TestViewFitsAllPagesAndSizes(t *testing.T) {
	m, _ := model(t)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 12}, {20, 5}, {200, 60}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for i := range m.tabs {
			m.tab = i
			for _, mode := range []string{"", "confirm", "detail"} {
				m.modal = mode
				view := m.View().Content
				lines := strings.Split(view, "\n")
				if len(lines) > size[1] {
					t.Fatalf("%s %v height %d", m.current().Kind, size, len(lines))
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("%s %v width %d: %q", m.current().Kind, size, ansi.StringWidth(line), line)
					}
				}
			}
		}
	}
}
func TestDynamicTabsKeepDeviceIdentity(t *testing.T) {
	m, _ := model(t)
	m.tab = 4
	s := fixture()
	s.Disks = append([]collector.Disk{{ID: "before", Name: "before"}}, s.Disks...)
	m.Update(sampleMsg{snapshot: s})
	if m.current().ID != "disk:disk0" || m.current().Label != "Disk1" {
		t.Fatal(m.current())
	}
	s.Disks = nil
	m.Update(sampleMsg{snapshot: s})
	if m.current().Kind != "overview" {
		t.Fatal("removed device tab retained")
	}
	m.tab = len(m.tabs) - 1
	m.Update(key("right"))
	if m.tab != 0 {
		t.Fatal("tab wrap failed")
	}
}
func TestTerminateConfirmationAndDetails(t *testing.T) {
	m, f := model(t)
	m.tab = 1
	m.refreshRows()
	m.cursor = 0
	m.Update(key("x"))
	if m.modal != "confirm" || f.killed != 0 {
		t.Fatal("unconfirmed terminate")
	}
	_, cmd := m.Update(key("enter"))
	if cmd != nil || f.killed != 0 || m.modal != "" {
		t.Fatal("Enter must cancel by default")
	}
	m.Update(key("x"))
	_, cmd = m.Update(key("y"))
	if cmd == nil {
		t.Fatal("missing confirmed action")
	}
	m.Update(cmd())
	if f.killed != 1 {
		t.Fatal("terminate missing")
	}
	_, cmd = m.Update(key("enter"))
	m.Update(cmd())
	if m.modal != "detail" {
		t.Fatal(m.modal)
	}
}
func TestSearchConsumesNavigationAndQuitKeys(t *testing.T) {
	m, _ := model(t)
	m.tab = 1
	m.Update(key("/"))
	m.Update(key("q"))
	if !m.searching || m.query != "q" {
		t.Fatal("search did not consume text")
	}
	m.Update(key("esc"))
	m.Update(key("esc"))
	if m.query != "" {
		t.Fatal("filter not cleared")
	}
	m.Update(key("t"))
	if !m.tree {
		t.Fatal("tree toggle")
	}
}
func TestTreeFilteringSortingAndCycles(t *testing.T) {
	s := fixture()
	rows := processRows(s.Processes, "child", "cpu", false, true)
	if len(rows) != 3 || rows[2].PID != 3 || rows[2].Depth != 2 {
		t.Fatal(rows)
	}
	ps := []collector.Process{{PID: 1, PPID: 2, Name: "one", CPU: math.NaN()}, {PID: 2, PPID: 1, Name: "two", CPU: 4}}
	rows = processRows(ps, "", "cpu", false, true)
	if len(rows) != 2 {
		t.Fatal("cycle lost processes")
	}
	rows = processRows(ps, "", "cpu", false, false)
	if rows[0].PID != 2 {
		t.Fatal("NaN sorted before measured CPU")
	}
}
func TestPauseAndSelectionPersistence(t *testing.T) {
	m, _ := model(t)
	m.tab = 1
	m.cursor = 1
	pid := m.rows[1].PID
	m.Update(sampleMsg{snapshot: fixture()})
	if m.rows[m.cursor].PID != pid {
		t.Fatal("selection moved on refresh")
	}
	m.Update(key(" "))
	if !m.paused {
		t.Fatal("space did not pause")
	}
	m.inFlight = false
	_, cmd := m.Update(tickMsg{})
	if cmd == nil || m.inFlight {
		t.Fatal("pause launched sample")
	}
}
func TestTerminalInjectionRemoved(t *testing.T) {
	got := clean("name\x1b[31mred\x1b[0m\n\x07\x1b]0;bad\x07")
	if strings.ContainsAny(got, "\x1b\n\x07") {
		t.Fatal(got)
	}
}
func BenchmarkOverview(b *testing.B) {
	m := New(context.Background(), &fakeSource{}, config.Defaults(), "")
	m.width = 140
	m.height = 40
	m.snapshot = fixture()
	m.ready = true
	m.rebuildTabs()
	for i := 0; i < 120; i++ {
		m.remember()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.View()
	}
}

func TestImmediateProcessSamplingDoesNotDuplicateTicks(t *testing.T) {
	m, _ := model(t)
	m.inFlight = false
	_, cmd := m.Update(key("right"))
	if cmd == nil || !m.inFlight {
		t.Fatal("entering Processes did not sample immediately")
	}
	_, old := m.Update(tickMsg{generation: m.generation - 1})
	if old != nil {
		t.Fatal("stale timer was not ignored")
	}
	m.Update(cmd())
	if m.snapshot.ProcessCount != 3 {
		t.Fatal("sample missing")
	}
}

func TestResumeMarksHistoryGap(t *testing.T) {
	m, _ := model(t)
	m.Update(key(" "))
	m.Update(key(" "))
	m.Update(sampleMsg{snapshot: fixture()})
	points := m.history.Points("cpu")
	if len(points) < 3 || !math.IsNaN(points[len(points)-2].Value) {
		t.Fatal("pause history was connected")
	}
}

func TestLanguageSwitchAndAutomaticTheme(t *testing.T) {
	m, _ := model(t)
	m.settings.Theme = "auto"
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if !m.light() {
		t.Fatal("white terminal was not detected")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if m.light() {
		t.Fatal("dark terminal was not detected")
	}
	m.settings.Theme = "light"
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if !m.light() {
		t.Fatal("explicit light theme was overridden")
	}
	m.settings.Language = "en"
	m.setting = 0
	m.changeSetting(1)
	if m.settings.Language != "zh-CN" || m.tr("Memory") != "内存" || !strings.Contains(m.settingsView(), "界面语言") {
		t.Fatal("language did not update live")
	}
	m.help = true
	if !strings.Contains(m.View().Content, "快捷键") {
		t.Fatal("help not translated")
	}
}
func TestLabelsStayOutsideFilledFrames(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	s := fixture()
	for i := 0; i < 20; i++ {
		s.At = s.At.Add(time.Second)
		s.CPU.Percent = 100
		s.Memory.Percent = 100
		m.Update(sampleMsg{snapshot: s})
	}
	out := ansi.Strip(m.overview(118, 29))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "│") && (strings.Contains(line, "CPU") || strings.Contains(line, "Memory") || strings.Contains(line, "100%")) {
			t.Fatalf("label inside graph: %q", line)
		}
	}
	frame := ansi.Strip(m.framedGraph([]string{"cpu"}, 20, 6, 100, m.palette().cpu))
	if !strings.Contains(frame, "████") {
		t.Fatalf("100%% not solid: %q", frame)
	}
}
func TestChineseLayoutsFit(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "zh-CN"
	for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 36}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for i := range m.tabs {
			m.tab = i
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) > size[1] {
				t.Fatal("Chinese layout too tall")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("Chinese layout overflow: %q", line)
				}
			}
		}
	}
}

func TestMtaskSettingsEditSaveCancelAndNavigation(t *testing.T) {
	m, _ := model(t)
	m.width = 120
	m.version = "0.1.0-test"
	m.settings.Language = "en"
	m.savedSettings = m.settings
	original := m.settings
	m.Update(key(","))
	if !m.settingsEditing || m.current().Kind != "settings" {
		t.Fatal("comma did not enter settings editor")
	}
	m.setting = 0
	tab := m.tab
	m.Update(key("right"))
	if m.settings.Language != "zh-CN" || m.tab != tab {
		t.Fatal("arrows should edit in settings")
	}
	m.Update(key("esc"))
	if m.settings != original || m.settingsEditing || m.current().Kind != "settings" {
		t.Fatal("Esc did not cancel preview")
	}
	m.Update(key("enter"))
	m.Update(key("right"))
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("Enter did not save")
	}
	m.Update(cmd())
	saved, err := config.Load(m.configPath)
	if err != nil || saved.Language != "zh-CN" || m.settingsEditing {
		t.Fatal(saved, err)
	}
	m.Update(key("enter"))
	m.setting = 2
	m.Update(key("right"))
	if m.settings.IntervalMS == saved.IntervalMS {
		t.Fatal("interval did not preview")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.current().Kind == "settings" || m.settings != saved {
		t.Fatal("Tab did not discard draft")
	}
	m.Update(key(","))
	m.Update(key("q"))
	if m.settingsEditing || m.settings != saved {
		t.Fatal("q did not cancel editing")
	}
	view := strings.Split(m.View().Content, "\n")
	if !strings.Contains(view[0], "Enter") || strings.Contains(view[len(view)-1], "Enter") {
		t.Fatal("settings tutorial is not at the top")
	}
	if !strings.Contains(m.settingsView(), "0.1.0-test") {
		t.Fatal("version is missing")
	}
	for _, line := range strings.Split(m.settingsView(), "\n") {
		if strings.TrimSpace(line) == "" {
			t.Fatal("settings rows should be compact")
		}
	}
}
func TestMemoryCompositionAndDiskGraphs(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	bar := ansi.Strip(m.memoryBar(80))
	lines := strings.Split(bar, "\n")
	if ansi.StringWidth(lines[1]) != 80 || !strings.Contains(bar, "Reclaimable") {
		t.Fatal("memory composition missing or wrong width")
	}
	disk := ansi.Strip(m.diskView(0, 100, 29))
	if strings.Count(disk, "┌") != 2 || !strings.Contains(disk, "Combined read/write") || !strings.Contains(disk, "Read ") {
		t.Fatal("disk needs utilization and throughput graphs")
	}
	npu := m.npuView(0, 100, 29)
	if !strings.Contains(npu, "Apple Neural Engine") || !strings.Contains(npu, "No readable live counters") {
		t.Fatal("NPU device or capability explanation missing")
	}
}

func TestBlocksAreUsedOnAllPages(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	if strings.Contains(m.settingsView(), "Chart characters") {
		t.Fatal("obsolete style selector remains")
	}
	m.snapshot.GPUs[0].MemoryUsed, m.snapshot.GPUs[0].MemoryTotal = 3<<30, 8<<30
	for _, theme := range []string{"dark", "light"} {
		m.settings.Theme = theme
		for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 36}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := range m.tabs {
				m.tab = i
				view := ansi.Strip(m.View().Content)
				for _, r := range view {
					if r >= 0x2800 && r <= 0x28ff {
						t.Fatalf("Braille in block mode on %s", m.current().Kind)
					}
				}
				lines := strings.Split(view, "\n")
				if len(lines) > size[1] {
					t.Fatal("block layout too tall")
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("block layout too wide")
					}
				}
			}
		}
	}
	for _, bar := range []string{m.memoryBar(40), m.usageBar(3, 8, 40, m.palette().gpu)} {
		emptyColor := strings.SplitN(m.blockEmptyStyle().Render("█"), "█", 2)[0]
		if !strings.Contains(bar, "█") || emptyColor == "" || !strings.Contains(bar, emptyColor) {
			t.Fatal("resource bar missing occupied or empty blocks")
		}
	}
}

func TestOccupiedAndEmptyBlocksHaveDistinctColors(t *testing.T) {
	m, _ := model(t)
	m.settings.Theme = "light"
	out := m.graph([]string{"cpu"}, 40, 12, 100, m.palette().cpu)
	colors := regexp.MustCompile(`\x1b\[[0-9;]*m`).FindAllString(out, -1)
	if len(colors) < 2 || !strings.Contains(out, "215;224;235") || !strings.Contains(out, "3;105;161") {
		t.Fatalf("missing occupied/empty colors: %q", out)
	}
}

func TestAcceleratorEnginesMemoryAndFrequencies(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	s := fixture()
	s.GPUs[0].Utilization = 23
	s.GPUs[0].Backend = "nvidia-smi"
	s.GPUs[0].MemoryUsed, s.GPUs[0].MemoryTotal = 3<<30, 8<<30
	s.GPUs[0].GraphicsMHz, s.GPUs[0].SMMHz, s.GPUs[0].MemoryMHz, s.GPUs[0].VideoMHz = 1440, 1410, 9000, 1000
	s.GPUs[0].Engines = []platform.Engine{{ID: "compute", Name: "GPU / compute", Utilization: 23}, {ID: "memory", Name: "Memory controller", Utilization: 50}, {ID: "encoder", Name: "Encoder", Utilization: 7}, {ID: "decoder", Name: "Decoder", Utilization: 8}, {ID: "jpeg", Name: "JPEG", Utilization: 9}, {ID: "ofa", Name: "Optical flow", Utilization: 10}}
	s.NPUs[0] = platform.EmptyNPU("ascend:4:0", "Ascend 910B3 · 4:0", "npu-smi")
	s.NPUs[0].Utilization, s.NPUs[0].MemoryUsed, s.NPUs[0].MemoryTotal = 32, 3454*1024*1024, 64<<30
	s.NPUs[0].MemoryKind = "HBM"
	s.NPUs[0].Engines = []platform.Engine{{ID: "aicore", Name: "AI Core", Utilization: 32}, {ID: "aivector", Name: "AI Vector", Utilization: 12}, {ID: "aicpu", Name: "AI CPU", Utilization: 7}, {ID: "control", Name: "Control CPU", Utilization: 2}, {ID: "hbm", Name: "HBM bandwidth", Utilization: 4}}
	s.GPUs[0].Utilization = platform.PeakUtilization(s.GPUs[0].Utilization, s.GPUs[0].Engines)
	s.CPU.MHz, s.CPU.MinMHz, s.CPU.MaxMHz = 2550, 2100, 3000
	m.Update(sampleMsg{snapshot: s})
	gpu := ansi.Strip(m.gpuView(0, 118, 30))
	for _, text := range []string{"Overall 50.0%", "GPU / compute 23.0%", "Memory controller 50.0%", "Encoder", "Decoder", "JPEG", "Optical flow", "Graphics clock 1440.0 MHz", "SM clock 1410.0 MHz", "Memory clock 9000.0 MHz", "Video clock 1000.0 MHz"} {
		if !strings.Contains(gpu, text) {
			t.Fatal("missing GPU metric", text)
		}
	}
	if strings.Count(gpu, "┌") != 6 {
		t.Fatal("expected five grouped engine panels and one memory bar")
	}
	npu := ansi.Strip(m.npuView(0, 118, 30))
	for _, text := range []string{"AI Core", "AI Vector", "AI CPU", "Control CPU", "HBM bandwidth", "HBM memory", "64.0 GiB"} {
		if !strings.Contains(npu, text) {
			t.Fatal("missing NPU metric", text)
		}
	}
	if len(m.history.Points("gpu:g0:engine:encoder")) != 1 || len(m.history.Points("npu:ascend:4:0:engine:aivector")) != 1 {
		t.Fatal("engine histories missing")
	}
	s.CPU.Cores = make([]float64, 192)
	m.Update(sampleMsg{snapshot: s})
	if !strings.Contains(ansi.Strip(m.cpuView(118, 30)), "Frequency 2550.0 MHz") {
		t.Fatal("frequency hidden on a large-core CPU")
	}
	for _, lang := range []string{"en", "zh-CN"} {
		m.settings.Language = lang
		for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 36}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := range m.tabs {
				m.tab = i
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) > size[1] {
					t.Fatal("engine layout overflow")
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("engine layout width overflow")
					}
				}
			}
		}
	}
}

func TestAcceleratorGroupingAndTemporaryGaps(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	g := &m.snapshot.GPUs[0]
	g.Utilization = 70
	g.Engines = []platform.Engine{{ID: "compute", Name: "GPU / compute", Utilization: 20}, {ID: "memory", Name: "Memory controller", Utilization: 40}, {ID: "hbm", Name: "HBM bandwidth", Utilization: 70}, {ID: "encoder", Name: "Encoder", Utilization: 12}, {ID: "decoder", Name: "Decoder", Utilization: 21}, {ID: "pcie-rx", Name: "PCIe receive", Utilization: 10}, {ID: "pcie-tx", Name: "PCIe send", Utilization: 20}}
	g.PCIeReceive, g.PCIeSend, g.PCIeCapacity = 1<<30, 2<<30, 10<<30
	view := ansi.Strip(m.gpuView(0, 118, 30))
	if strings.Count(view, "┌") != 4 {
		t.Fatal("related engines should share panels")
	}
	for _, text := range []string{"Overall 70.0%", "Memory controller 40.0%", "HBM bandwidth 70.0%", "Encoder 12.0%", "Decoder 21.0%", "PCIe RX 1.0 GiB/s", "TX 2.0 GiB/s"} {
		if !strings.Contains(view, text) {
			t.Fatal("missing grouped metric", text, view)
		}
	}
	for _, width := range []int{40, 70, 118} {
		narrow := ansi.Strip(m.gpuView(0, width, 30))
		for _, text := range []string{"HBM bandwidth 70.0%", "Decoder 21.0%", "TX 2.0 GiB/s"} {
			if !strings.Contains(narrow, text) {
				t.Fatalf("width %d lost second series: %s", width, text)
			}
		}
	}
	for i := range g.Engines {
		g.Engines[i].Utilization = math.NaN()
	}
	g.Utilization = math.NaN()
	view = ansi.Strip(m.gpuView(0, 118, 30))
	if strings.Count(view, "┌") != 4 || !strings.Contains(view, "Overall —") {
		t.Fatal("temporary counter failure changed panel layout")
	}
}

func TestResourceSummaryAndFullScaleLabelsAreHidden(t *testing.T) {
	m, _ := model(t)
	s := fixture()
	s.CPU.Capacity = 1.5
	s.CPU.Scoped = true
	s.Memory.Limited, s.Memory.UsageKnown, s.Memory.AvailableKnown = true, true, true
	s.Memory.HostTotal = 64 << 30
	s.Memory.AllocatableKnown = true
	s.Limits.Scope = "allocation"
	s.Limits.AddressKnown = true
	m.Update(sampleMsg{snapshot: s})
	for _, lang := range []string{"en", "zh-CN"} {
		m.settings.Language = lang
		for i := range m.tabs {
			m.tab = i
			body := ansi.Strip(m.page(118, 30))
			for _, text := range []string{"CPU capacity", "allocation", "Shared host", "Memory limit", "scale ", "100%", "配额", "共享主机", "满量程"} {
				if strings.Contains(body, text) {
					t.Fatalf("%s still displays %q", m.current().Kind, text)
				}
			}
		}
	}
}

func TestAllPageHintsAreAtTop(t *testing.T) {
	m, _ := model(t)
	m.width = 180
	for i := range m.tabs {
		m.tab = i
		view := strings.Split(ansi.Strip(m.View().Content), "\n")
		if !strings.Contains(view[0], m.pageHint()) {
			t.Fatalf("hint not on first line: %s", m.current().Kind)
		}
		for _, line := range view[1:] {
			if strings.Contains(line, m.pageHint()) {
				t.Fatal("duplicate hint in footer")
			}
		}
	}
}
func TestVRAMBarAndUnknownGPUFields(t *testing.T) {
	m, _ := model(t)
	g := &m.snapshot.GPUs[0]
	g.MemoryUsed = 3 << 30
	g.MemoryTotal = 8 << 30
	out := ansi.Strip(m.gpuView(0, 100, 30))
	if !strings.Contains(out, "GPU memory 3.0 GiB / 8.0 GiB") || !strings.Contains(out, "█") || !strings.Contains(out, "37.5%") {
		t.Fatal("VRAM usage bar missing")
	}
	if strings.Contains(out, "N/A") || strings.Contains(out, "Temperature") || strings.Contains(out, "Power") {
		t.Fatalf("unknown optional fields shown: %q", out)
	}
	disk := m.diskView(0, 100, 30)
	if strings.Contains(disk, "service time / elapsed") {
		t.Fatal("implementation tip still on disk page")
	}
}
func Test192CoresUseNumericTableWithoutCoreHistories(t *testing.T) {
	m, _ := model(t)
	s := fixture()
	s.CPU.Cores = make([]float64, 192)
	s.CPU.IDs = make([]int, 192)
	s.CPU.Logical = 192
	s.CPU.HostLogical = 192
	s.CPU.Capacity = 192
	for i := range s.CPU.Cores {
		s.CPU.Cores[i] = float64(i % 100)
		s.CPU.IDs[i] = i
	}
	m.Update(sampleMsg{snapshot: s})
	out := ansi.Strip(m.cpuView(118, 30))
	if strings.Contains(out, "┌") || !strings.Contains(out, "CPU 191") {
		t.Fatal("large CPU set should be a complete numeric table")
	}
	for key := range m.history {
		if strings.HasPrefix(key, "core:") {
			t.Fatal("large server allocated per-core histories")
		}
	}
}

func TestLargeCoreScrollingClampsAtLastRow(t *testing.T) {
	m, _ := model(t)
	m.width = 80
	m.height = 24
	m.snapshot.CPU.Cores = make([]float64, 192)
	m.snapshot.CPU.Percent = 50
	m.tab = 2
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	end := m.scroll
	if end <= 0 {
		t.Fatal("End did not scroll large core table")
	}
	for i := 0; i < 10; i++ {
		m.Update(key("down"))
	}
	if m.scroll != end {
		t.Fatal("scroll escaped table bounds")
	}
	m.Update(key("up"))
	if m.scroll != end-1 {
		t.Fatal("scroll became stuck at end")
	}
}
