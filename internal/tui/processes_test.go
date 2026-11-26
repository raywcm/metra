package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/raywcm/metra/internal/collector"
)

func TestProcessCPUThenMemoryAndUnknownLast(t *testing.T) {
	ps := []collector.Process{{PID: 5, CPU: 0.04, RSS: 100, MemoryKnown: true}, {PID: 2, CPU: 0.03, RSS: 300, MemoryKnown: true}, {PID: 1, CPU: 10, RSS: 10, MemoryKnown: true}, {PID: 3, CPU: math.NaN(), RSS: 9999, MemoryKnown: true}, {PID: 4, CPU: 0, RSS: 300, MemoryKnown: true}}
	rows := processRows(ps, "", "cpu", false, false)
	for i, pid := range []int32{1, 2, 4, 5, 3} {
		if rows[i].PID != pid {
			t.Fatal("CPU display ties must use memory then PID", rows)
		}
	}
	rows = processRows(ps, "", "cpu", true, false)
	if rows[0].PID != 5 || rows[len(rows)-1].PID != 3 {
		t.Fatal("reverse sorting moved unknown CPU to the top", rows)
	}
	ps = append(ps, collector.Process{PID: 6, CPU: 50, RSS: 99999, MemoryKnown: false})
	rows = processRows(ps, "", "memory", true, false)
	if rows[len(rows)-1].PID != 6 {
		t.Fatal("unknown memory must remain last in either direction")
	}
}

func TestProcessLiveTopAndExplicitSelection(t *testing.T) {
	m, _ := model(t)
	m.tab = 1
	s := fixture()
	s.ProcessesAt = time.Now()
	s.Processes = []collector.Process{{PID: 10, Created: 100, CPU: math.NaN(), RSS: 1000, MemoryKnown: true}, {PID: 20, Created: 200, CPU: math.NaN(), RSS: 100, MemoryKnown: true}}
	m.Update(sampleMsg{snapshot: s})
	s.Processes[0].CPU, s.Processes[1].CPU = 0, 90
	s.ProcessesAt = s.ProcessesAt.Add(time.Second)
	m.Update(sampleMsg{snapshot: s})
	if m.rows[m.cursor].PID != 20 || m.offset != 0 {
		t.Fatal("initial CPU sample pinned an idle process instead of following the ranking")
	}
	m.Update(key("down"))
	s.Processes[0].CPU, s.Processes[1].CPU = 99, 0
	s.ProcessesAt = s.ProcessesAt.Add(time.Second)
	m.Update(sampleMsg{snapshot: s})
	if m.rows[m.cursor].PID != 10 {
		t.Fatal("explicitly selected process was lost")
	}
	m.Update(key("s"))
	if m.cursor != 0 || m.offset != 0 || m.processPinned {
		t.Fatal("new sorting should start at the top")
	}
	m.Update(key("down"))
	selected := m.rows[m.cursor].PID
	for i := range s.Processes {
		if s.Processes[i].PID == selected {
			s.Processes[i].Created++
		}
	}
	s.ProcessesAt = s.ProcessesAt.Add(time.Second)
	m.Update(sampleMsg{snapshot: s})
	if m.processPinned {
		t.Fatal("reused PID retained the old process selection")
	}
}

func TestNetworkSelectionSurvivesInterfaceReordering(t *testing.T) {
	m, _ := model(t)
	s := fixture()
	s.Networks = []collector.Network{{Name: "eth0"}, {Name: "wg0"}}
	m.Update(sampleMsg{snapshot: s})
	for i, tab := range m.tabs {
		if tab.Kind == "network" {
			m.tab = i
		}
	}
	m.Update(key("down"))
	s.Networks = []collector.Network{{Name: "wg0"}, {Name: "eth0"}}
	m.Update(sampleMsg{snapshot: s})
	if m.snapshot.Networks[m.network].Name != "wg0" {
		t.Fatal("selected network changed identity")
	}
	s.Networks = []collector.Network{{ReceiveRate: 20}, {ReceiveRate: 20, ExcludeFromTotals: true}}
	if total := networkRate(&s, true); total != 20 {
		t.Fatal("VPN traffic was counted twice", total)
	}
}

func TestUnavailableNetworkReadings(t *testing.T) {
	m, _ := model(t)
	m.settings.Language = "en"
	m.tab = m.tabIndex("network")
	s := fixture()
	s.Networks = []collector.Network{
		{Name: "eth0", Up: true, ReceiveRate: 20, SendRate: 10},
		{Name: "eth1", ReceiveRate: math.NaN(), SendRate: math.NaN(), MetadataUnavailable: true, CountersUnavailable: true},
	}
	m.Update(sampleMsg{snapshot: s})
	if total := networkRate(&s, true); !math.IsNaN(total) {
		t.Fatal("partial traffic counted as a complete total", total)
	}
	m.Update(key("down"))
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "link state unknown") || !strings.Contains(view, "Traffic counters unavailable") || strings.Contains(view, "Errors 0") {
		t.Fatal("missing interface information reported as down or zero", view)
	}
	s.Networks[1].ExcludeFromTotals = true
	if total := networkRate(&s, true); total != 20 {
		t.Fatal("an excluded tunnel suppressed the physical total", total)
	}
	s.NetworkInventoryUnavailable = true
	if total := networkRate(&s, true); !math.IsNaN(total) {
		t.Fatal("incomplete inventory produced a total", total)
	}
	s.Networks = nil
	m.Update(sampleMsg{snapshot: s})
	for _, view := range []string{m.compactValue(), m.networkView(80, 18)} {
		if !strings.Contains(view, "Network interfaces unavailable") {
			t.Fatal("failed inventory reported as an empty network", view)
		}
	}
}

func BenchmarkDeepProcessTree(b *testing.B) {
	processes := make([]collector.Process, 2000)
	for i := range processes {
		processes[i] = collector.Process{PID: int32(i + 1), PPID: int32(i), Name: "worker"}
	}
	for b.Loop() {
		processRows(processes, "worker", "pid", false, true)
	}
}

func TestPinRequiresProcessStartTimeAcrossRefresh(t *testing.T) {
	m, _ := model(t)
	m.tab = m.tabIndex("processes")
	s := fixture()
	s.Processes = []collector.Process{{PID: 10, Created: 100, CPU: 10}, {PID: 20, CPU: 1}}
	m.Update(sampleMsg{snapshot: s, withProcesses: true})
	m.Update(key("down"))
	m.Update(sampleMsg{snapshot: s, withProcesses: true})
	if !m.processPinned || m.rows[m.cursor].PID != 20 {
		t.Fatal("hardware-only refresh moved the cursor within the same process list")
	}
	s.ProcessesAt = s.ProcessesAt.Add(time.Second)
	m.Update(sampleMsg{snapshot: s, withProcesses: true})
	if m.processPinned || m.rows[m.cursor].PID != 10 {
		t.Fatal("kept a PID pinned across samples without a verified start time")
	}
}
