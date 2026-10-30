package tui

import (
	"math"
	"testing"
	"time"

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
	m.Update(sampleMsg{snapshot: s})
	if m.rows[m.cursor].PID != 20 || m.offset != 0 {
		t.Fatal("initial CPU sample pinned an idle process instead of following the ranking")
	}
	m.Update(key("down"))
	s.Processes[0].CPU, s.Processes[1].CPU = 99, 0
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
	if total := networkRate([]collector.Network{{ReceiveRate: 20}, {ReceiveRate: 20, ExcludeFromTotals: true}}, true); total != 20 {
		t.Fatal("VPN traffic was counted twice", total)
	}
}
