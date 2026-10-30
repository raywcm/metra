package tui

import (
	"math"
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type metricPanel struct {
	heading       string
	labels        []string
	numericLabels []string
	keys          []string
	limit         float64
	style         lipgloss.Style
	priority      int
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// Divide all available cells, including remainders. The last row can have
// fewer columns, so its remaining panels still span the entire terminal.
func splitSpace(total, count, gap int) []int {
	if count <= 0 {
		return nil
	}
	available := max(0, total-(count-1)*gap)
	out := make([]int, count)
	for i := range out {
		out[i] = available / count
		if i < available%count {
			out[i]++
		}
	}
	return out
}

func panelLabels(p metricPanel, width int) []string {
	var lines []string
	if p.heading != "" {
		lines = append(lines, fit(p.heading, width))
	}
	return append(lines, metricLines(p.labels, width)...)
}

func panelNumbers(panels []metricPanel, width int) []string {
	var values []string
	for _, p := range panels {
		labels := p.labels
		if p.numericLabels != nil {
			labels = p.numericLabels
		}
		text := strings.Join(labels, " · ")
		if p.heading != "" {
			text = p.heading + "  " + text
		}
		values = append(values, text)
	}
	// One device per row group: a changing rate must never push the next
	// device onto a new line or change the space left for charts above it.
	var rows []string
	for _, value := range values {
		rows = append(rows, metricLines([]string{value}, width)...)
	}
	return rows
}

var metricValue = regexp.MustCompile(`(?:[0-9]+(?:\.[0-9]+)?|—)(?: (?:[KMGTPE]i)?B(?:/s)?|%| MHz| °C| W)?`)

// Budget for the display format rather than this sample's digit count.
// For example, 0 B/s and 1023.9 GiB/s occupy the same rate slot. The actual
// text is not reformatted, so values, colors and units retain their precision.
func metricWidth(item string) int {
	plain := ansi.Strip(item)
	budget := metricValue.ReplaceAllStringFunc(plain, func(value string) string {
		width := 8
		switch {
		case strings.HasSuffix(value, "%"), value == "—":
			width = 6
		case strings.HasSuffix(value, "B/s"):
			width = 12
		case strings.HasSuffix(value, "B"):
			width = 10
		case strings.HasSuffix(value, " MHz"):
			width = 12
		case strings.HasSuffix(value, " °C"):
			width = 11
		case strings.HasSuffix(value, " W"):
			width = 10
		}
		return strings.Repeat(" ", max(width, ansi.StringWidth(value)))
	})
	return max(ansi.StringWidth(plain), ansi.StringWidth(budget))
}

// Graphs use an explicit cell budget. Less important graphs become numeric
// summaries before any frame could be clipped. Numeric overflow alone may
// scroll; a graph is always displayed with its complete border.
func (m *Model) panelLayout(panels []metricPanel, width, height, maxColumns int) string {
	if len(panels) == 0 {
		return ""
	}
	panels = append([]metricPanel(nil), panels...)
	slices.SortStableFunc(panels, func(a, b metricPanel) int {
		if (len(a.keys) == 0) != (len(b.keys) == 0) {
			if len(a.keys) == 0 {
				return 1
			}
			return -1
		}
		return a.priority - b.priority
	})
	if width < 24 || width > 250000/max(1, height) {
		return strings.Join(panelNumbers(panels, width), "\n")
	}
	drawable := 0
	for _, p := range panels {
		if len(p.keys) > 0 {
			drawable++
		}
	}
	maxVisible := min(drawable, max(0, (height-1)/5)*max(1, min(maxColumns, width/34)))
	for visible := maxVisible; visible > 0; visible-- {
		columns := max(1, min(maxColumns, visible, width/34))
		rows := (visible + columns - 1) / columns
		var widths [][]int
		var headers [][]string
		headerHeights := make([]int, rows)
		for row := 0; row < rows; row++ {
			start, end := row*columns, min(visible, (row+1)*columns)
			ws := splitSpace(width, end-start, 2)
			widths = append(widths, ws)
			var hs []string
			for i := start; i < end; i++ {
				lines := panelLabels(panels[i], ws[i-start])
				headerHeights[row] = max(headerHeights[row], len(lines))
				hs = append(hs, strings.Join(lines, "\n"))
			}
			headers = append(headers, hs)
		}
		numbers := panelNumbers(panels[visible:], width)
		fixed := len(numbers) + 1 // Time axis is shared by every visible graph.
		for _, n := range headerHeights {
			fixed += n
		}
		if fixed+rows*4 > height {
			continue
		}
		sizes := splitSpace(height-fixed, rows, 0)
		var rendered []string
		for row := 0; row < rows; row++ {
			start, end := row*columns, min(visible, (row+1)*columns)
			var cells []string
			for i := start; i < end; i++ {
				p := panels[i]
				header := headers[row][i-start]
				for lineCount(header) < headerHeights[row] {
					header += "\n"
				}
				cell := m.framedGraph(p.keys, widths[row][i-start], sizes[row], p.limit, p.style)
				if header != "" {
					cell = header + "\n" + cell
				}
				cells = append(cells, cell)
			}
			rendered = append(rendered, joinPanels(cells))
		}
		rendered = append(rendered, m.axis(width))
		rendered = append(rendered, numbers...)
		return strings.Join(rendered, "\n")
	}
	return strings.Join(panelNumbers(panels, width), "\n")
}

func roundedBarWidths(values []float64, total float64, width int) []int {
	out := make([]int, len(values))
	if total <= 0 || !finite(total) || width <= 0 {
		return out
	}
	previous, cumulative := 0, 0.0
	pixels := make([]float64, len(values))
	for i, value := range values {
		if finite(value) && value > 0 {
			cumulative += value
			pixels[i] = value / total * float64(width)
		}
		boundary := int(math.Round(math.Min(1, cumulative/total) * float64(width)))
		out[i] = max(0, boundary-previous)
		previous = boundary
	}
	// A tiny segment can straddle a rounded boundary even when its own width
	// is below half a cell. Merge that pixel into a visible neighbor instead
	// of inventing a minimum-width colored sliver.
	for i, count := range out {
		if count == 0 || pixels[i] >= 0.5 {
			continue
		}
		target := -1
		for j := i - 1; j >= 0; j-- {
			if pixels[j] >= 0.5 {
				target = j
				break
			}
		}
		if target < 0 {
			for j := i + 1; j < len(out); j++ {
				if pixels[j] >= 0.5 {
					target = j
					break
				}
			}
		}
		if target >= 0 {
			out[target] += count
			out[i] = 0
		}
	}
	return out
}

func (m *Model) compactValue() string {
	s := m.snapshot
	t := m.current()
	p := m.palette()
	switch t.Kind {
	case "overview", "cpu":
		return "CPU " + m.heatPercent(s.CPU.Percent, p.cpu) + " · " + m.tr("Memory") + " " + m.heatPercent(s.Memory.Percent, p.memory)
	case "memory":
		return m.tr("Memory") + " " + m.heatPercent(s.Memory.Percent, p.memory) + " · " + bytes(float64(s.Memory.Used))
	case "gpu":
		if t.Index < len(s.GPUs) {
			return m.tabLabel(t) + " " + m.heatPercent(s.GPUs[t.Index].Utilization, p.gpu)
		}
	case "npu":
		if t.Index < len(s.NPUs) {
			return m.tabLabel(t) + " " + m.heatPercent(s.NPUs[t.Index].Utilization, p.cpu)
		}
	case "disk":
		if t.Index < len(s.Disks) {
			d := s.Disks[t.Index]
			return m.tabLabel(t) + " " + m.heatPercent(d.CombinedBusy, p.disk) + " R " + speed(d.ReadRate) + " W " + speed(d.WriteRate)
		}
	case "network":
		return "↓ " + speed(networkRate(s.Networks, true)) + " ↑ " + speed(networkRate(s.Networks, false))
	case "processes":
		return m.tr("Processes") + " " + number(float64(s.ProcessCount))
	}
	return m.tabLabel(t)
}

func bodySize(width, height int) (int, int, int) {
	if width >= 40 && height >= 12 {
		return max(1, width-2), max(1, height-5), 1
	}
	return max(1, width), max(1, height-3), 0
}

func (m *Model) processRowsVisible() int {
	_, height, _ := bodySize(m.width, m.height)
	if height <= 2 {
		return height
	}
	if m.searching || m.query != "" {
		height--
	}
	return max(1, height-2)
}

func (m *Model) hardwareScrollMax() int {
	width, height, _ := bodySize(m.width, m.height)
	return max(0, lineCount(m.page(width, height))-height)
}

func wrapItem(item string, width int) []string {
	return strings.Split(ansi.Hardwrap(item, max(1, width), true), "\n")
}
