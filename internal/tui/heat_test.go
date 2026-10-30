package tui

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNumericHeatContrastAndUnknownValues(t *testing.T) {
	m, _ := model(t)
	for _, theme := range []string{"light", "dark", "forest"} {
		m.settings.Theme = theme
		p := m.palette()
		for _, base := range []lipgloss.Style{p.cpu, p.memory, p.gpu, p.network, p.disk} {
			for value := 0.0; value <= 100; value++ {
				style := m.heatStyle(value, base)
				r, g, b, _ := style.GetBackground().RGBA()
				fr, fg, fb, _ := style.GetForeground().RGBA()
				a := luminance(float64(r>>8), float64(g>>8), float64(b>>8))
				z := luminance(float64(fr>>8), float64(fg>>8), float64(fb>>8))
				if (math.Max(a, z)+0.05)/(math.Min(a, z)+0.05) < 4.5 {
					t.Fatal("numeric background lost legibility", theme, value)
				}
			}
			if m.heatStyle(0, base).GetBackground() != m.heatStyle(0.04, base).GetBackground() {
				t.Fatal("displayed zero acquired a busy tint")
			}
			if m.heatStyle(0, base).GetBackground() == m.heatStyle(100, base).GetBackground() {
				t.Fatal("background does not respond to utilization")
			}
			for _, v := range []float64{math.NaN(), math.Inf(1), -1} {
				if _, absent := m.heatStyle(v, base).GetBackground().(lipgloss.NoColor); !absent {
					t.Fatal("unavailable reading was colored as zero")
				}
			}
		}
	}
}

func TestHeatOnlyAppearsInNumericPanels(t *testing.T) {
	m, _ := model(t)
	p := m.palette()
	panel := metricPanel{labels: []string{"CPU 75.0%"}, numericLabels: []string{"CPU " + m.heatPercent(75, p.cpu)}, keys: []string{"cpu"}, style: p.cpu}
	background := regexp.MustCompile("\\x1b\\[[0-9;]*48;(2|5);")
	numeric := m.panelLayout([]metricPanel{panel}, 40, 2, 1)
	if !background.MatchString(numeric) || ansi.Strip(numeric) != "CPU 75.0%" {
		t.Fatal("numeric heat changed the value or failed to render", numeric)
	}
	graph := m.panelLayout([]metricPanel{panel}, 40, 12, 1)
	if background.MatchString(strings.Split(graph, "\n")[0]) {
		t.Fatal("numeric fallback styling leaked into graph labels")
	}
	for _, v := range []float64{0, 0.04, 12.5, 99.9, 100, math.NaN()} {
		if ansi.Strip(m.heatPercent(v, p.cpu)) != percent(v) {
			t.Fatal("heat styling changed the reading")
		}
	}
}
