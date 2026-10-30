package tui

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"math"
	"strings"
	"unicode"
)

type palette struct{ accent, muted, text, border, danger, cpu, memory, disk, network, gpu lipgloss.Style }

func (m *Model) blockBackground() string {
	if m.light() {
		return "#D7E0EB"
	}
	return "#2B3A50"
}

func (m *Model) blockEmptyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.blockBackground()))
}

func (m *Model) palette() palette {
	memory, disk, network, gpu, danger := "#A78BFA", "#FBBF24", "#34D399", "#F472B6", "#F87171"
	accent, muted, text, border := "#7DD3FC", "#8995AB", "#E2E8F0", "#354156"
	if m.settings.Theme == "forest" {
		accent = "#6EE7B7"
		border = "#36544B"
	}
	if m.light() {
		accent = "#0369A1"
		muted = "#52627A"
		text = "#182234"
		border = "#A4B2C7"
		memory = "#7C3AED"
		disk = "#B45309"
		network = "#047857"
		gpu = "#BE185D"
		danger = "#B91C1C"
	}
	style := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	return palette{style(accent), style(muted), style(text), style(border), style(danger), style(accent), style(memory), style(disk), style(network), style(gpu)}
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
func fit(s string, w int) string {
	w = max(0, w)
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func number(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.1f", v)
}
func percent(v float64) string {
	if !finite(v) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", v)
}
func bytes(v float64) string {
	if !finite(v) || v < 0 {
		return "—"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
func speed(v float64) string {
	if !finite(v) {
		return "—"
	}
	return bytes(v) + "/s"
}
func uptime(seconds uint64) string {
	return fmt.Sprintf("%dd %02d:%02d:%02d", seconds/86400, seconds/3600%24, seconds/60%60, seconds%60)
}

func knownBytes(v uint64, known bool) string {
	if !known {
		return "—"
	}
	return bytes(float64(v))
}
func knownInt(v int32, known bool) string {
	if !known {
		return "—"
	}
	return fmt.Sprint(v)
}
