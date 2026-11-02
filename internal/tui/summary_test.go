package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raywcm/metra/internal/collector"
)

func TestCompactFooterAndProcessSummary(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		m, _ := model(t)
		m.settings.Language = language
		m.width, m.height = 160, 36
		footer := func() string {
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			return lines[len(lines)-1]
		}
		if strings.Contains(footer(), m.tr("LIVE")) {
			t.Fatal("redundant live label", language, footer())
		}
		m.paused = true
		if !strings.Contains(footer(), m.tr("PAUSED")) {
			t.Fatal("paused state must remain visible", language)
		}
		m.paused = false
		if strings.Contains(footer(), m.tr("PAUSED")) {
			t.Fatal("paused state did not clear", language)
		}
		heading := strings.Split(ansi.Strip(m.processView(158, 31)), "\n")[0]
		if strings.Contains(heading, "3/3") || strings.Contains(heading, m.tr("list")) || !strings.Contains(heading, "3 · ") || !strings.Contains(heading, "RSS ↓") {
			t.Fatal("process heading was not simplified", language, heading)
		}
		m.rows = m.rows[:1]
		heading = strings.Split(ansi.Strip(m.processView(158, 31)), "\n")[0]
		if !strings.Contains(heading, "1/3") {
			t.Fatal("filtered process count missing", language, heading)
		}
		m.tree = true
		if !strings.Contains(ansi.Strip(m.processView(158, 31)), m.tr("tree (siblings sorted)")) {
			t.Fatal("tree mode lost its indicator", language)
		}
	}
}

func TestSwapUsageColorAndMemorySummary(t *testing.T) {
	background := regexp.MustCompile(`\x1b\[[0-9;]*48;(2|5);[0-9;]*m`)
	foreground := regexp.MustCompile(`\x1b\[[0-9;]*38;(2|5);[0-9;]*m`)
	for _, language := range []string{"en", "zh-CN"} {
		m, _ := model(t)
		m.settings.Language = language
		memory := &m.snapshot.Memory
		memory.UsageKnown, memory.AvailableKnown, memory.CachedKnown, memory.SwapKnown = true, true, true, true
		memory.Cached = 7 << 30
		var previous string
		for _, used := range []uint64{0, 4 << 30, 8 << 30} {
			memory.SwapUsed, memory.SwapTotal = used, 8<<30
			output := m.memoryView(118, 31)
			plain := ansi.Strip(output)
			for _, removed := range []string{m.tr("Used %s", bytes(float64(memory.Used))), m.tr("Available %s", bytes(float64(memory.Available))), m.tr("Cached %s", bytes(float64(memory.Cached)))} {
				if strings.Contains(plain, removed) {
					t.Fatal("duplicate memory detail retained", language, removed)
				}
			}
			expected := m.tr("Swap %s / %s", bytes(float64(used)), "8.0 GiB")
			found := false
			for _, line := range strings.Split(output, "\n") {
				if !strings.Contains(ansi.Strip(line), expected) {
					continue
				}
				if background.MatchString(line) {
					t.Fatal("swap must not have a usage background", language, line)
				}
				color := foreground.FindString(line)
				if color == "" || color == previous {
					t.Fatal("swap text color did not reflect usage", language, used, line)
				}
				previous, found = color, true
			}
			if !found {
				t.Fatal("swap values missing", language, expected)
			}
		}
		memory.SwapUsed, memory.SwapTotal = 0, 0
		output := m.memoryView(118, 31)
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(ansi.Strip(line), m.tr("Swap %s / %s", "0 B", "0 B")) && background.MatchString(line) {
				t.Fatal("unknown swap capacity received a usage background", language)
			}
		}
		memory.SwapKnown = false
		if strings.Contains(ansi.Strip(m.memoryView(118, 31)), m.tr("Swap %s / %s", "0 B", "0 B")) {
			t.Fatal("unavailable swap shown", language)
		}
	}
}

func TestDiskDetailsOmitFreeSpaceAndServiceTimes(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		m, _ := model(t)
		m.settings.Language = language
		disk := &m.snapshot.Disks[0]
		disk.Busy, disk.ReadBusy, disk.WriteBusy, disk.CombinedBusy = 3, 1, 2, 3
		disk.Mounts = []collector.Mount{{Path: "/", FSType: "ext4", Used: 781 << 30, Total: 878 << 30, Free: 52 << 30}}
		plain := ansi.Strip(m.diskView(0, 118, 31))
		if !strings.Contains(plain, "ext4") || !strings.Contains(plain, "781.0 GiB / 878.0 GiB") || !strings.Contains(plain, m.tr("Combined read/write utilization")) {
			t.Fatal("useful disk details missing", language, plain)
		}
		for _, removed := range []string{"52.0 GiB", m.tr("Read time %s", "1.0%"), m.tr("Write time %s", "2.0%"), m.tr("Active %s", "3.0%")} {
			if strings.Contains(plain, removed) {
				t.Fatal("redundant disk detail retained", language, removed)
			}
		}
	}
}
