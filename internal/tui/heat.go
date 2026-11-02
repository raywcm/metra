package tui

import (
	"fmt"
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
)

func luminance(r, g, b float64) float64 {
	linear := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// Use the hardware's graph hue, with enough foreground contrast in either
// terminal theme. Unknown readings never acquire the background of a real 0%.
func (m *Model) heatStyle(value float64, base lipgloss.Style) lipgloss.Style {
	if !finite(value) || value < 0 {
		return base.UnsetBackground()
	}
	var background color.Color = lipgloss.Color(m.blockBackground())
	accent := base.GetForeground()
	if _, absent := accent.(lipgloss.NoColor); accent == nil || absent {
		accent = m.palette().accent.GetForeground()
	}
	r0, g0, b0, _ := background.RGBA()
	r1, g1, b1, _ := accent.RGBA()
	value = math.Round(value*10) / 10
	t := math.Sqrt(math.Min(100, value) / 100)
	mix := func(a, b uint32) float64 { return math.Round(float64(a>>8)*(1-t) + float64(b>>8)*t) }
	r, g, b := mix(r0, r1), mix(g0, g1), mix(b0, b1)
	foreground := "#FFFFFF"
	l := luminance(r, g, b)
	if (l+0.05)/0.05 >= 1.05/(l+0.05) {
		foreground = "#000000"
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", int(r), int(g), int(b)))).Foreground(lipgloss.Color(foreground)).Bold(true)
}

func (m *Model) heatPercent(value float64, base lipgloss.Style) string {
	return m.heatStyle(value, base).Render(percent(value))
}

// Swap changes only the text color: green through amber to red as it fills.
func (m *Model) usageTextStyle(value float64) lipgloss.Style {
	p := m.palette()
	if !finite(value) || value < 0 {
		return p.text.UnsetBackground()
	}
	value = math.Min(100, value)
	low, high, fraction := p.network, p.disk, value/50
	if value > 50 {
		low, high, fraction = p.disk, p.danger, (value-50)/50
	}
	r0, g0, b0, _ := low.GetForeground().RGBA()
	r1, g1, b1, _ := high.GetForeground().RGBA()
	mix := func(a, b uint32) int {
		return int(math.Round(float64(a>>8)*(1-fraction) + float64(b>>8)*fraction))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", mix(r0, r1), mix(g0, g1), mix(b0, b1))))
}
