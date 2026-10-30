package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raywcm/metra/internal/chart"
	"github.com/raywcm/metra/internal/platform"
)

func (m *Model) View() tea.View {
	w, h := m.width, m.height
	p := m.palette()
	var content string
	if h < 4 {
		lines := []string{fit(m.compactValue(), w)}
		for len(lines) < h {
			lines = append(lines, strings.Repeat(" ", w))
		}
		content = strings.Join(lines, "\n")
	} else {
		full := w >= 40 && h >= 12
		bodyW, areaH, padding := bodySize(w, h)
		host := clean(m.snapshot.Host)
		if host == "" {
			host = m.tr("local system")
		}
		state := m.tr("LIVE")
		if m.paused {
			state = m.tr("PAUSED")
		}
		header := fit(p.accent.Render(" metra ")+p.text.Render(m.pageHint()), w)
		if m.current().Kind == "settings" {
			header = fit(p.accent.Render(" metra v"+m.version)+" · "+p.text.Render(m.pageHint())+" "+p.muted.Render(m.status), w)
		}
		body := m.tr("Collecting hardware information…")
		if m.ready || m.current().Kind == "settings" {
			body = m.page(bodyW, areaH)
		}
		if m.help {
			body = m.helpView()
		}
		if m.modal != "" {
			body = m.modalView(bodyW)
		}
		lines := strings.Split(body, "\n")
		if len(lines) > areaH {
			start := 0
			if m.help || m.modal != "" {
				start = min(m.overlayScroll, len(lines)-areaH)
			} else if m.current().Kind == "settings" {
				start = min(max(0, 1+m.setting-areaH+1), len(lines)-areaH)
			} else if m.current().Kind != "processes" && m.current().Kind != "network" {
				start = min(m.scroll, len(lines)-areaH)
			}
			lines = lines[start : start+areaH]
		}
		for i := range lines {
			lines[i] = strings.Repeat(" ", padding) + fit(lines[i], bodyW) + strings.Repeat(" ", padding)
		}
		for len(lines) < areaH {
			lines = append(lines, strings.Repeat(" ", w))
		}
		info := m.status
		if info == "" {
			if len(m.snapshot.Warnings) > 0 {
				info = "⚠ " + clean(m.snapshot.Warnings[0])
			} else if m.ready {
				info = m.tr("%s · %s/%s · up %s · %d processes", clean(m.snapshot.OS), clean(m.snapshot.Kernel), m.snapshot.Arch, uptime(m.snapshot.Uptime), m.snapshot.ProcessCount)
			}
		}
		if m.current().Kind == "settings" {
			info = m.tr("Settings file: %s", clean(m.configPath))
		} else {
			info = host + " · " + state + " · " + info
		}
		parts := []string{header, m.tabBar(w)}
		if full {
			parts = append(parts, p.border.Render(strings.Repeat("─", w)))
		}
		parts = append(parts, strings.Join(lines, "\n"))
		if full {
			parts = append(parts, p.border.Render(strings.Repeat("─", w)))
		}
		parts = append(parts, fit(p.muted.Render(" "+info), w))
		content = strings.Join(parts, "\n")
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.ReportFocus = true
	v.WindowTitle = "metra · " + m.tr("system monitor")
	v.ForegroundColor = p.text.GetForeground()
	if m.settings.Theme != "auto" {
		bg := "#101724"
		if m.light() {
			bg = "#FFFFFF"
		}
		v.BackgroundColor = lipgloss.Color(bg)
	}
	return v
}
func (m *Model) tabLabel(t Tab) string {
	switch t.Kind {
	case "disk":
		return m.tr("Disk%d", t.Index)
	case "gpu":
		return fmt.Sprintf("GPU%d", t.Index)
	case "npu":
		return fmt.Sprintf("NPU%d", t.Index)
	default:
		return m.tr(t.Label)
	}
}
func (m *Model) tabBar(w int) string {
	p := m.palette()
	if w < 20 {
		return fit(p.accent.Bold(true).Render(m.tabLabel(m.current())), w)
	}
	labels := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		style := p.muted
		if i == m.tab {
			style = p.accent.Bold(true).Reverse(true)
		}
		labels[i] = style.Render(" " + m.tabLabel(t) + " ")
	}
	start, used := 0, 0
	for i := 0; i <= m.tab; i++ {
		used += ansi.StringWidth(labels[i])
		for used > w-2 && start < i {
			used -= ansi.StringWidth(labels[start])
			start++
		}
	}
	out := ""
	if start > 0 {
		out = p.muted.Render("‹ ")
	}
	for i := start; i < len(labels); i++ {
		if ansi.StringWidth(out)+ansi.StringWidth(labels[i]) > w-2 {
			out += p.muted.Render(" ›")
			break
		}
		out += labels[i]
	}
	return fit(out, w)
}
func (m *Model) page(w, h int) string {
	t := m.current()
	switch t.Kind {
	case "overview":
		return m.overview(w, h)
	case "processes":
		return m.processView(w, h)
	case "cpu":
		return m.cpuView(w, h)
	case "memory":
		return m.memoryView(w, h)
	case "disk":
		if t.Index < len(m.snapshot.Disks) {
			return m.diskView(t.Index, w, h)
		}
	case "network":
		return m.networkView(w, h)
	case "gpu":
		if t.Index < len(m.snapshot.GPUs) {
			return m.gpuView(t.Index, w, h)
		}
	case "npu":
		if t.Index < len(m.snapshot.NPUs) {
			return m.npuView(t.Index, w, h)
		}
	case "settings":
		return m.settingsView()
	}
	return m.tr("Device disconnected.")
}
func (m *Model) graph(keys []string, w, h int, limit float64, style lipgloss.Style) string {
	series := make([][]chart.Point, len(keys))
	for i, key := range keys {
		series[i] = m.history.Points(key)
	}
	if limit <= 0 {
		visible := make([][]chart.Point, len(keys))
		for i, key := range keys {
			visible[i] = m.visiblePoints(key)
		}
		limit = chart.Maximum(visible...)
	}
	now := m.snapshot.At
	if now.IsZero() {
		now = time.Now()
	}
	style = style.Background(lipgloss.Color(m.blockBackground()))
	options := chart.Options{Width: w, Height: h, Max: limit, Now: now, Range: time.Duration(m.settings.RangeSeconds) * time.Second, Grid: m.settings.Grid, Fill: true, MaxGap: max(2*time.Second, 3*m.settings.Interval()), StyleLine: style.Render, StyleGrid: style.Render, StyleFill: style.Render}
	options.StyleEmpty = m.blockEmptyStyle().Render
	gridColor := "#26354A"
	if m.light() {
		gridColor = "#DBE3ED"
	}
	options.StyleGrid = lipgloss.NewStyle().Foreground(lipgloss.Color(gridColor)).Background(lipgloss.Color(m.blockBackground())).Render
	if len(keys) == 2 {
		second := m.palette().network
		if strings.HasPrefix(keys[0], "net:") {
			second = m.palette().cpu
		}
		second = second.Background(lipgloss.Color(m.blockBackground()))
		options.SeriesLine = []func(...string) string{style.Render, second.Render}
		options.SeriesFill = []func(...string) string{style.Render, second.Render}
	}
	return chart.Render(series, options)
}

// The frame contains only data. No padding steals pixels from the 0–100% scale.
func (m *Model) framedGraph(keys []string, w, height int, limit float64, style lipgloss.Style) string {
	w = max(4, w)
	height = max(4, height)
	border := m.palette().border
	inner := m.graph(keys, w-2, height-2, limit, style)
	lines := strings.Split(inner, "\n")
	for i := range lines {
		lines[i] = border.Render("│") + fit(lines[i], w-2) + border.Render("│")
	}
	return border.Render("┌"+strings.Repeat("─", w-2)+"┐") + "\n" + strings.Join(lines, "\n") + "\n" + border.Render("└"+strings.Repeat("─", w-2)+"┘")
}
func caption(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	if rw+2 >= w {
		return fit(left, w)
	}
	return fit(left, w-rw-1) + " " + right
}
func (m *Model) axis(w int) string {
	return m.palette().muted.Render(caption(fmt.Sprintf("−%ds", m.settings.RangeSeconds), m.tr("now"), w))
}
func (m *Model) title(label, subtitle string) string {
	return m.palette().accent.Bold(true).Render(label) + "  " + m.palette().muted.Render(clean(subtitle))
}
func joinPanels(panels []string) string {
	all := make([]string, 0, len(panels)*2)
	for _, p := range panels {
		if len(all) > 0 {
			all = append(all, "  ")
		}
		all = append(all, p)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, all...)
}
func (m *Model) overview(w, h int) string {
	p := m.palette()
	s := m.snapshot
	type card struct {
		label, value string
		keys         []string
		color        lipgloss.Style
		limit        float64
		usage        float64
	}
	cpuValue := percent(s.CPU.Percent)
	if s.CPU.TemperatureKnown {
		cpuValue += " · " + number(s.CPU.Temperature) + " °C"
	}
	cards := []card{{"CPU", cpuValue, []string{"cpu"}, p.cpu, 100, s.CPU.Percent}, {m.tr("Memory"), percent(s.Memory.Percent) + " · " + bytes(float64(s.Memory.Used)), []string{"memory"}, p.memory, 100, s.Memory.Percent}}
	for i, d := range s.Disks {
		cards = append(cards, card{m.tr("Disk%d", i) + " · " + d.Name, percent(d.CombinedBusy) + " · R " + speed(d.ReadRate) + " W " + speed(d.WriteRate), []string{"disk:" + d.ID + ":combined"}, p.disk, 100, d.CombinedBusy})
	}
	networkKeys := []string{"net:total:rx", "net:total:tx"}
	if len(s.Networks) == 0 {
		networkKeys = nil
	}
	cards = append(cards, card{m.tr("Network · %d interfaces", len(s.Networks)), "↓ " + speed(networkRate(s.Networks, true)) + "  ↑ " + speed(networkRate(s.Networks, false)), networkKeys, p.network, 0, math.NaN()})
	for i, g := range s.GPUs {
		value := m.liveValue(g.Utilization)
		if finite(g.MemoryUsed) {
			value += " · " + bytes(g.MemoryUsed)
			if finite(g.MemoryTotal) && g.MemoryTotal > 0 {
				value += " / " + bytes(g.MemoryTotal)
			}
		}
		keys := []string{"gpu:" + g.ID}
		if !finite(g.Utilization) && len(g.Engines) == 0 {
			keys = nil
		}
		cards = append(cards, card{fmt.Sprintf("GPU%d · %s", i, g.Name), value, keys, p.gpu, 100, g.Utilization})
	}
	for i, n := range s.NPUs {
		value := m.liveValue(n.Utilization)
		if finite(n.MemoryUsed) && finite(n.MemoryTotal) && n.MemoryTotal > 0 {
			value += " · " + bytes(n.MemoryUsed) + " / " + bytes(n.MemoryTotal)
		}
		keys := []string{"npu:" + n.ID}
		if !finite(n.Utilization) && len(n.Engines) == 0 {
			keys = nil
		}
		cards = append(cards, card{fmt.Sprintf("NPU%d · %s", i, n.Name), value, keys, p.cpu, 100, n.Utilization})
	}
	var panels []metricPanel
	for _, c := range cards {
		priority := 3
		switch {
		case len(c.keys) == 0:
			priority = 5
		case c.keys[0] == "cpu":
			priority = 0
		case c.keys[0] == "memory":
			priority = 1
		case strings.HasPrefix(c.keys[0], "gpu:"), strings.HasPrefix(c.keys[0], "npu:"):
			priority = 2
		case strings.HasPrefix(c.keys[0], "net:"):
			priority = 4
		}
		numericValue := p.text.Render(c.value)
		if finite(c.usage) && strings.HasPrefix(c.value, percent(c.usage)) {
			numericValue = m.heatPercent(c.usage, c.color) + p.text.Render(strings.TrimPrefix(c.value, percent(c.usage)))
		}
		panels = append(panels, metricPanel{heading: c.color.Bold(true).Render(clean(c.label)), labels: []string{p.text.Render(c.value)}, numericLabels: []string{numericValue}, keys: c.keys, style: c.color, limit: c.limit, priority: priority})
	}
	return m.title(m.tr("Performance"), s.CPU.Name) + "\n" + m.panelLayout(panels, w, h-1, 3)
}

func (m *Model) cpuView(w, h int) string {
	s := m.snapshot.CPU
	p := m.palette()
	var metrics []string
	if s.TemperatureKnown {
		metrics = append(metrics, m.tr("CPU temperature %s °C", number(s.Temperature)))
	}
	metrics = append(metrics, m.tr("Logical CPUs %d", len(s.Cores)))
	if finite(s.MHz) && s.MHz > 0 {
		frequency := m.tr("Frequency %s MHz", number(s.MHz))
		if s.FrequencyReported {
			frequency = m.tr("Reported frequency %s MHz", number(s.MHz))
		}
		metrics = append(metrics, frequency)
		if finite(s.MinMHz) && finite(s.MaxMHz) && s.MaxMHz > s.MinMHz {
			metrics = append(metrics, m.tr("Range %s–%s MHz", number(s.MinMHz), number(s.MaxMHz)))
		}
	}
	if finite(s.Load[0]) && finite(s.Load[1]) && finite(s.Load[2]) {
		metrics = append(metrics, m.tr("Host load %.2f  %.2f  %.2f", s.Load[0], s.Load[1], s.Load[2]))
	}
	if h < 10 || w < 32 {
		metrics = append([]string{m.tr("%s utilization", m.heatPercent(s.Percent, p.cpu))}, metrics...)
		compact := m.title("CPU", s.Name) + "\n" + strings.Join(metricLines(metrics, w), "\n")
		if m.cores || len(s.Cores) >= largeCoreThreshold {
			compact += "\n" + m.coreTable(w)
		}
		return compact
	}
	heading := []string{m.title("CPU", s.Name)}
	heading = append(heading, metricLines(metrics, w)...)
	if len(s.Cores) >= largeCoreThreshold {
		heading = append(heading, p.cpu.Render(m.tr("%s utilization", m.heatPercent(s.Percent, p.cpu))), m.coreTable(w))
		return strings.Join(heading, "\n")
	}
	var panels []metricPanel
	if m.cores {
		for j, v := range s.Cores {
			id := j
			if j < len(s.IDs) {
				id = s.IDs[j]
			}
			panels = append(panels, metricPanel{labels: []string{fmt.Sprintf("CPU %d  %s", id, percent(v))}, keys: []string{fmt.Sprintf("core:%d", id)}, numericLabels: []string{fmt.Sprintf("CPU %d  %s", id, m.heatPercent(v, p.cpu))}, limit: 100, style: p.cpu})
		}
	} else {
		panels = []metricPanel{{labels: []string{p.cpu.Bold(true).Render(m.tr("%s utilization", percent(s.Percent)))}, numericLabels: []string{m.tr("%s utilization", m.heatPercent(s.Percent, p.cpu))}, keys: []string{"cpu"}, limit: 100, style: p.cpu}}
	}
	heading = append(heading, m.panelLayout(panels, w, h-len(heading), 4))
	return strings.Join(heading, "\n")
}
func (m *Model) coreTable(w int) string {
	s := m.snapshot.CPU
	p := m.palette()
	cols := max(1, w/19)
	cell := w / cols
	var out []string
	for i := 0; i < len(s.Cores); i += cols {
		line := ""
		for j := i; j < min(len(s.Cores), i+cols); j++ {
			id := j
			if j < len(s.IDs) {
				id = s.IDs[j]
			}
			text := fmt.Sprintf("CPU %-4d ", id) + fit(m.heatPercent(s.Cores[j], p.cpu), 7)
			if cell < 19 {
				text = fmt.Sprintf("%d %s", id, m.heatPercent(s.Cores[j], p.cpu))
			}
			line += fit(text, cell)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
func (m *Model) memoryView(w, h int) string {
	s := m.snapshot.Memory
	p := m.palette()
	if s.Total == 0 && !m.snapshot.Limits.MemoryLimitKnown {
		return m.title(m.tr("Memory"), "") + "\n" + m.tr("No readable live counters")
	}
	known := s.UsageKnown || (!s.Limited && s.Total > 0 && finite(s.Percent))
	var values []string
	if known {
		values = append(values, m.tr("Used %s", bytes(float64(s.Used))))
	}
	if s.AvailableKnown || (!s.Limited && s.Total > 0) {
		values = append(values, m.tr("Available %s", bytes(float64(s.Available))))
	}
	if s.CachedKnown {
		values = append(values, m.tr("Cached %s", bytes(float64(s.Cached))))
	}
	if s.SwapKnown {
		values = append(values, m.tr("Swap %s / %s", bytes(float64(s.SwapUsed)), bytes(float64(s.SwapTotal))))
	}
	bar := ""
	if known && finite(s.Percent) && w >= 24 && h >= 14 {
		candidate := m.tr("Memory composition") + "\n" + m.memoryBar(w)
		summary := m.tr("%s used · %s total", percent(s.Percent), bytes(float64(s.Total)))
		if 1+len(metricLines([]string{summary}, w))+lineCount(candidate)+len(metricLines(values, w)) <= h {
			bar = candidate
		}
	}
	if bar == "" {
		values = append(values, m.memoryLegend()...)
	}
	details := metricLines(values, w)
	lines := []string{m.title(m.tr("Memory"), "")}
	if known && finite(s.Percent) {
		panel := metricPanel{labels: []string{p.memory.Bold(true).Render(m.tr("%s used · %s total", percent(s.Percent), bytes(float64(s.Total))))}, numericLabels: []string{m.tr("%s used · %s total", m.heatPercent(s.Percent, p.memory), bytes(float64(s.Total)))}, keys: []string{"memory"}, limit: 100, style: p.memory}
		lines = append(lines, m.panelLayout([]metricPanel{panel}, w, h-1-len(details)-lineCount(bar), 1))
	} else {
		lines = append(lines, m.tr("Total %s", bytes(float64(s.Total))))
	}
	if bar != "" {
		lines = append(lines, bar)
	}
	lines = append(lines, details...)
	return strings.Join(lines, "\n")
}
func (m *Model) memoryBar(w int) string {
	s := m.snapshot.Memory
	p := m.palette()
	if s.Total == 0 {
		return ""
	}
	inner := max(1, w-2)
	modified := min(s.Modified, s.Used)
	used := s.Used - modified
	values := []uint64{used, modified, s.Reclaimable, s.Unavailable, s.Free}
	styles := []lipgloss.Style{p.memory, p.disk, p.network, p.danger, p.muted}
	styles[4] = m.blockEmptyStyle()
	sum := 0
	segments := make([]float64, len(values))
	for i, v := range values {
		segments[i] = float64(v)
	}
	counts := roundedBarWidths(segments, float64(s.Total), inner)
	var bar strings.Builder
	for i := range values {
		count := min(inner-sum, counts[i])
		if i == len(values)-1 {
			count = inner - sum
		}
		if count > 0 {
			bar.WriteString(styles[i].Render(strings.Repeat("█", count)))
			sum += count
		}
	}
	border := p.border.Render
	frame := border("┌"+strings.Repeat("─", inner)+"┐") + "\n" + border("│") + bar.String() + border("│") + "\n" + border("└"+strings.Repeat("─", inner)+"┘")
	return frame + "\n" + strings.Join(metricLines(m.memoryLegend(), w), "\n")
}
func (m *Model) memoryLegend() []string {
	s := m.snapshot.Memory
	p := m.palette()
	used := s.Used - min(s.Modified, s.Used)
	modified := min(s.Modified, s.Used)
	legend := []string{p.memory.Render(m.tr("In use %s", bytes(float64(used))))}
	if s.ModifiedKnown {
		legend = append(legend, p.disk.Render(m.tr("Modified %s", bytes(float64(modified)))))
	}
	if !s.Limited || s.Reclaimable > 0 {
		legend = append(legend, p.network.Render(m.tr("Reclaimable %s", bytes(float64(s.Reclaimable)))))
	}
	if s.Unavailable > 0 {
		legend = append(legend, p.danger.Render(m.tr("Unavailable %s", bytes(float64(s.Unavailable)))))
	}
	legend = append(legend, p.muted.Render(m.tr("Free %s", bytes(float64(s.Free)))))
	return legend
}

func (m *Model) diskView(i, w, h int) string {
	d := m.snapshot.Disks[i]
	p := m.palette()
	label := m.tr("Combined read/write utilization")
	if !finite(d.Busy) {
		label = m.tr("Combined read/write service time")
	}
	panels := []metricPanel{
		{labels: []string{p.disk.Render(label + " " + percent(d.CombinedBusy))}, numericLabels: []string{p.disk.Render(label+" ") + m.heatPercent(d.CombinedBusy, p.disk)}, keys: []string{"disk:" + d.ID + ":combined"}, limit: 100, style: p.disk},
		{labels: []string{p.disk.Render(m.tr("Read %s", speed(d.ReadRate))), p.network.Render(m.tr("Write %s", speed(d.WriteRate)))}, keys: []string{"disk:" + d.ID + ":read", "disk:" + d.ID + ":write"}, style: p.disk, priority: 1},
	}
	values := []string{m.diskTimes(i), m.tr("Total read %s · Total written %s", bytes(float64(d.ReadBytes)), bytes(float64(d.WriteBytes)))}
	for _, v := range d.Mounts {
		values = append(values, m.tr("%s (%s) · %s / %s used · %s free", clean(v.Path), clean(v.FSType), bytes(float64(v.Used)), bytes(float64(v.Total)), bytes(float64(v.Free))))
	}
	if i == 0 {
		for _, v := range m.snapshot.Volumes {
			values = append(values, m.tr("Volume %s (%s) · %s / %s", clean(v.Path), clean(v.FSType), bytes(float64(v.Used)), bytes(float64(v.Total))))
		}
	}
	details := metricLines(values, w)
	lines := []string{m.title(m.tr("Disk%d", i), d.Name), m.panelLayout(panels, w, h-1-len(details), 1)}
	lines = append(lines, details...)
	return strings.Join(lines, "\n")
}
func (m *Model) networkView(w, h int) string {
	if len(m.snapshot.Networks) == 0 {
		return m.tr("Network") + "\n" + m.tr("No active network interfaces.")
	}
	i := min(m.network, len(m.snapshot.Networks)-1)
	n := m.snapshot.Networks[i]
	p := m.palette()
	state := m.tr("down")
	if n.Up {
		state = m.tr("up")
	}
	limit := chart.Maximum(m.visiblePoints("net:"+n.Name+":rx"), m.visiblePoints("net:"+n.Name+":tx"))
	panels := []metricPanel{
		{labels: []string{p.network.Render(m.tr("Receive %s", speed(n.ReceiveRate)))}, keys: []string{"net:" + n.Name + ":rx"}, limit: limit, style: p.network},
		{labels: []string{p.cpu.Render(m.tr("Send %s", speed(n.SendRate)))}, keys: []string{"net:" + n.Name + ":tx"}, limit: limit, style: p.cpu, priority: 1},
	}
	details := metricLines([]string{m.tr("Received %s · Sent %s · Errors %d · Drops %d", bytes(float64(n.Received)), bytes(float64(n.Sent)), n.Errors, n.Drops), m.tr("Addresses %s", clean(strings.Join(n.Addresses, " · ")))}, w)
	lines := []string{m.title(m.tr("Network"), m.tr("%s · %s · interface %d/%d", n.Name, state, i+1, len(m.snapshot.Networks))), m.panelLayout(panels, w, h-1-len(details), 1)}
	lines = append(lines, details...)
	return strings.Join(lines, "\n")
}
func (m *Model) engineGraphs(kind, id, overallName string, overall float64, engines []platform.Engine, w, h int, style lipgloss.Style, transport ...platform.GPU) string {
	type trace struct {
		key, name, id string
		value         float64
	}
	type plot struct {
		group  string
		traces []trace
		limit  float64
	}
	var plots []plot
	if len(engines) == 0 && finite(overall) {
		plots = append(plots, plot{group: "overall", limit: 100, traces: []trace{{key: kind + ":" + id, name: m.tr(overallName), value: overall}}})
	}
	for _, engine := range engines {
		group := engine.ID
		switch group {
		case "encoder", "decoder":
			group = "codec"
		case "memory", "hbm", "bandwidth":
			group = "memory"
		case "pcie-rx", "pcie-tx":
			group = "pcie"
		}
		at := -1
		for i := range plots {
			if plots[i].group == group {
				at = i
				break
			}
		}
		if at < 0 {
			plots = append(plots, plot{group: group, limit: 100})
			at = len(plots) - 1
		}
		plots[at].traces = append(plots[at].traces, trace{key: kind + ":" + id + ":engine:" + engine.ID, name: m.tr(engine.Name), id: engine.ID, value: engine.Utilization})
	}
	if len(transport) > 0 && (finite(transport[0].PCIeReceive) || finite(transport[0].PCIeSend)) {
		raw := plot{group: "pcie", limit: 0, traces: []trace{{key: kind + ":" + id + ":pcie-rx-bytes", id: "pcie-rx", value: transport[0].PCIeReceive}, {key: kind + ":" + id + ":pcie-tx-bytes", id: "pcie-tx", value: transport[0].PCIeSend}}}
		hasPCIe := false
		for i, p := range plots {
			if p.group == "pcie" {
				hasPCIe = true
				if !finite(transport[0].PCIeCapacity) || transport[0].PCIeCapacity <= 0 {
					plots[i] = raw
				}
			}
		}
		if !hasPCIe {
			plots = append(plots, raw)
		}
	}
	if len(plots) == 0 {
		return ""
	}
	var panels []metricPanel
	for _, p := range plots {
		panel := metricPanel{limit: p.limit, style: style, priority: 3}
		switch p.group {
		case "overall", "compute", "aicore", "renderer":
			panel.priority = 0
		case "memory":
			panel.priority = 1
		case "pcie":
			panel.priority = 2
		}
		for k, t := range p.traces {
			keyStyle := style
			if k == 1 {
				keyStyle = m.palette().network
			}
			text := t.name + " " + percent(t.value)
			numericText := keyStyle.Render("■ "+t.name+" ") + m.heatPercent(t.value, keyStyle)
			if p.group == "pcie" && len(transport) > 0 {
				rate, name := transport[0].PCIeReceive, m.tr("PCIe RX")
				if t.id == "pcie-tx" {
					rate, name = transport[0].PCIeSend, m.tr("TX")
				}
				text = name + " " + speed(rate)
				numericText = keyStyle.Render("■ " + text)
				if p.limit > 0 && finite(t.value) {
					text += " " + percent(t.value)
					numericText += " " + m.heatPercent(t.value, keyStyle)
				}
			}
			panel.labels = append(panel.labels, keyStyle.Render("■ "+text))
			panel.numericLabels = append(panel.numericLabels, numericText)
			panel.keys = append(panel.keys, t.key)
		}
		panels = append(panels, panel)
	}
	return m.panelLayout(panels, w, h, 2)
}

func metricLines(items []string, w int) []string {
	w = max(1, w)
	var lines []string
	current := ""
	for _, item := range items {
		if item == "" {
			continue
		}
		budget := metricWidth(item)
		if budget > w || strings.Contains(item, "\n") {
			if current != "" {
				lines = append(lines, strings.TrimRight(current, " "))
				current = ""
			}
			for _, part := range strings.Split(item, "\n") {
				wrapped := wrapItem(part, w)
				reserved := (metricWidth(part) + w - 1) / w
				for len(wrapped) < reserved {
					wrapped = append(wrapped, "")
				}
				lines = append(lines, wrapped...)
			}
			continue
		}
		if current != "" && ansi.StringWidth(current)+3+budget > w {
			lines = append(lines, strings.TrimRight(current, " "))
			current = ""
		}
		if current != "" {
			current += " · "
		}
		current += fit(item, budget)
	}
	if current != "" {
		lines = append(lines, strings.TrimRight(current, " "))
	}
	return lines
}

func (m *Model) gpuView(i, w, h int) string {
	g := m.snapshot.GPUs[i]
	p := m.palette()
	lines := []string{m.title(fmt.Sprintf("GPU%d", i), g.Name)}
	if finite(g.Utilization) || len(g.Engines) > 0 {
		lines = append(lines, p.gpu.Bold(true).Render(m.tr("Overall %s", percent(g.Utilization))))
	}
	hasMemory := finite(g.MemoryUsed) && finite(g.MemoryTotal) && g.MemoryTotal > 0
	var details []string
	if len(g.Engines) == 0 {
		if finite(g.Renderer) {
			details = append(details, m.tr("Renderer %s", percent(g.Renderer)))
		}
		if finite(g.Tiler) {
			details = append(details, m.tr("Tiler %s", percent(g.Tiler)))
		}
	}
	if g.Cores > 0 {
		details = append(details, m.tr("Cores %d", g.Cores))
	}
	if finite(g.GraphicsMHz) && g.GraphicsMHz > 0 {
		details = append(details, m.tr("Graphics clock %s MHz", number(g.GraphicsMHz)))
	}
	if finite(g.SMMHz) && g.SMMHz > 0 {
		details = append(details, m.tr("SM clock %s MHz", number(g.SMMHz)))
	}
	if finite(g.MemoryMHz) && g.MemoryMHz > 0 {
		details = append(details, m.tr("Memory clock %s MHz", number(g.MemoryMHz)))
	}
	if finite(g.VideoMHz) && g.VideoMHz > 0 {
		details = append(details, m.tr("Video clock %s MHz", number(g.VideoMHz)))
	}
	if finite(g.Temperature) {
		details = append(details, m.tr("Temperature %s °C", number(g.Temperature)))
	}
	if finite(g.Power) {
		details = append(details, m.tr("Power %s W", number(g.Power)))
	}
	detailLines := metricLines(details, w)
	var memoryLines []string
	if hasMemory {
		label := m.tr("GPU memory %s / %s", bytes(g.MemoryUsed), bytes(g.MemoryTotal))
		if g.UnifiedMemory {
			label = m.tr("Shared GPU memory %s / %s system RAM", bytes(g.MemoryUsed), bytes(g.MemoryTotal))
		}
		memoryLines = metricLines([]string{p.gpu.Render(label), percent(g.MemoryUsed / g.MemoryTotal * 100)}, w)
	} else if finite(g.MemoryUsed) {
		memoryLines = metricLines([]string{m.tr("GPU memory used %s", bytes(g.MemoryUsed))}, w)
	}
	numeric := m.engineGraphs("gpu", g.ID, "Overall", g.Utilization, g.Engines, w, 0, p.gpu, g)
	showBar := hasMemory && w >= 24 && h >= 20 && len(lines)+len(detailLines)+len(memoryLines)+lineCount(numeric)+3 <= h
	extra := len(detailLines) + len(memoryLines)
	if showBar {
		extra += 3
	}
	overall := "Overall"
	if g.Backend == "nvidia-smi" {
		overall = "GPU / compute"
	}
	graphs := m.engineGraphs("gpu", g.ID, overall, g.Utilization, g.Engines, w, h-len(lines)-extra, p.gpu, g)
	if !strings.Contains(graphs, "┌") {
		if finite(g.Utilization) || len(g.Engines) > 0 {
			lines[1] = p.gpu.Bold(true).Render(m.tr("Overall %s", m.heatPercent(g.Utilization, p.gpu)))
		}
		if hasMemory {
			label := m.tr("GPU memory %s / %s", bytes(g.MemoryUsed), bytes(g.MemoryTotal))
			if g.UnifiedMemory {
				label = m.tr("Shared GPU memory %s / %s system RAM", bytes(g.MemoryUsed), bytes(g.MemoryTotal))
			}
			memoryLines = metricLines([]string{p.gpu.Render(label), m.heatPercent(g.MemoryUsed/g.MemoryTotal*100, p.gpu)}, w)
		}
		lines = append(lines, memoryLines...)
		if graphs != "" {
			lines = append(lines, graphs)
		}
	} else {
		lines = append(lines, graphs)
		lines = append(lines, memoryLines...)
	}
	if showBar {
		lines = append(lines, m.usageBar(g.MemoryUsed, g.MemoryTotal, w, p.gpu))
	}
	lines = append(lines, detailLines...)
	if len(lines) == 1 {
		lines = append(lines, m.tr("No readable live counters"))
	}
	return strings.Join(lines, "\n")
}
func (m *Model) usageBar(used, total float64, w int, style lipgloss.Style) string {
	inner := max(1, w-2)
	count := 0
	if total > 0 && finite(used) {
		count = int(math.Round(math.Max(0, math.Min(1, used/total)) * float64(inner)))
	}
	border := m.palette().border.Render
	filled := "█"
	empty := m.blockEmptyStyle().Render(strings.Repeat("█", inner-count))
	return border("┌"+strings.Repeat("─", inner)+"┐") + "\n" + border("│") + style.Render(strings.Repeat(filled, count)) + empty + border("│") + "\n" + border("└"+strings.Repeat("─", inner)+"┘")
}
func (m *Model) npuView(i, w, h int) string {
	n := m.snapshot.NPUs[i]
	p := m.palette()
	lines := []string{m.title(fmt.Sprintf("NPU%d", i), n.Name)}
	if finite(n.Utilization) || len(n.Engines) > 0 {
		lines = append(lines, p.cpu.Bold(true).Render(m.tr("Overall %s", percent(n.Utilization))))
	}
	hasMemory := finite(n.MemoryUsed) && finite(n.MemoryTotal) && n.MemoryTotal > 0
	var details []string
	if finite(n.Temperature) {
		details = append(details, m.tr("Temperature %s °C", number(n.Temperature)))
	}
	if finite(n.Power) {
		details = append(details, m.tr("Power %s W", number(n.Power)))
	}
	if n.Cores > 0 {
		details = append(details, m.tr("Cores %d", n.Cores))
	}
	detailLines := metricLines(details, w)
	var memoryLines []string
	if hasMemory {
		memoryLines = metricLines([]string{p.cpu.Render(m.tr("%s memory %s / %s", n.MemoryKind, bytes(n.MemoryUsed), bytes(n.MemoryTotal))), percent(n.MemoryUsed / n.MemoryTotal * 100)}, w)
	}
	numeric := m.engineGraphs("npu", n.ID, "Overall", n.Utilization, n.Engines, w, 0, p.cpu)
	showBar := hasMemory && w >= 24 && h >= 20 && len(lines)+len(detailLines)+len(memoryLines)+lineCount(numeric)+3 <= h
	extra := len(detailLines) + len(memoryLines)
	if showBar {
		extra += 3
	}
	overall := "Overall"
	if n.Backend == "npu-smi" {
		overall = "AI Core"
	}
	graphs := m.engineGraphs("npu", n.ID, overall, n.Utilization, n.Engines, w, h-len(lines)-extra, p.cpu)
	if !strings.Contains(graphs, "┌") {
		if finite(n.Utilization) || len(n.Engines) > 0 {
			lines[1] = p.cpu.Bold(true).Render(m.tr("Overall %s", m.heatPercent(n.Utilization, p.cpu)))
		}
		if hasMemory {
			memoryLines = metricLines([]string{p.cpu.Render(m.tr("%s memory %s / %s", n.MemoryKind, bytes(n.MemoryUsed), bytes(n.MemoryTotal))), m.heatPercent(n.MemoryUsed/n.MemoryTotal*100, p.cpu)}, w)
		}
		lines = append(lines, memoryLines...)
		if graphs != "" {
			lines = append(lines, graphs)
		}
	} else {
		lines = append(lines, graphs)
		lines = append(lines, memoryLines...)
	}
	if showBar {
		lines = append(lines, m.usageBar(n.MemoryUsed, n.MemoryTotal, w, p.cpu))
	}
	lines = append(lines, detailLines...)
	if len(lines) == 1 {
		lines = append(lines, m.tr("No readable live counters"))
	}
	return strings.Join(lines, "\n")
}
func (m *Model) processView(w, h int) string {
	p := m.palette()
	mode := m.tr("list")
	if m.tree {
		mode = m.tr("tree (siblings sorted)")
	}
	arrow := m.processSortArrow()
	order := m.tr(m.sortBy) + " " + arrow
	if m.sortBy == "cpu" {
		order += " · RSS " + arrow
	} else if m.sortBy == "memory" {
		order += " · CPU " + arrow
	}
	lines := []string{m.title(m.tr("Processes"), m.tr("%d shown / %d total · %s · sort: %s", len(m.rows), m.snapshot.ProcessCount, mode, order))}
	if m.searching {
		lines = append(lines, m.input.View())
	} else if m.query != "" {
		lines = append(lines, p.accent.Render(m.tr("Filter: %s · Esc clears", clean(m.query))))
	}
	if m.snapshot.ProcessesAt.IsZero() {
		return strings.Join(append(lines, m.tr("Collecting processes…")), "\n")
	}
	if time.Since(m.snapshot.ProcessesAt) > 5*time.Second && !m.paused {
		lines[0] += p.danger.Render(m.tr(" · stale %s", m.snapshot.ProcessesAt.Format("15:04:05")))
	}
	format := func(pid, name, cpu, rss, threads string) string {
		if w < 20 {
			pw := min(6, max(1, w/3))
			cw := max(0, w-pw-1)
			return fit(pid, pw) + " " + fit(cpu, cw)
		}
		if w < 40 {
			return fit(pid, 6) + fit(name, w-14) + fit(cpu, 8)
		}
		if w < 70 {
			return fit(pid, 7) + fit(name, w-29) + fit(cpu, 9) + fit(rss, 13)
		}
		return fit(pid, 8) + fit(name, w-38) + fit(cpu, 10) + fit(rss, 13) + fit(threads, 7)
	}
	header := func(field, label string) string {
		if m.sortBy == field {
			return p.accent.Bold(true).Render(label + " " + arrow)
		}
		return p.muted.Bold(true).Render(label)
	}
	lines = append(lines, format(header("pid", "PID"), header("name", m.tr("NAME")), header("cpu", "CPU %"), header("memory", "RSS"), p.muted.Render(m.tr("THREADS"))))
	if h <= 2 {
		lines = nil
	}
	count := max(1, h-len(lines))
	pool := m.snapshot.Memory.HostTotal
	if pool == 0 {
		pool = m.snapshot.Memory.Total
	}
	for i := m.offset; i < min(len(m.rows), m.offset+count); i++ {
		r := m.rows[i]
		name := strings.Repeat("  ", r.Depth) + clean(r.Name)
		if m.tree && r.Depth > 0 {
			name = strings.Repeat("  ", r.Depth-1) + "└ " + clean(r.Name)
		}
		cpu, rss := number(r.CPU), knownBytes(r.RSS, r.MemoryKnown)
		if i != m.cursor {
			cpu = m.heatStyle(r.CPU, p.cpu).Render(cpu)
			if r.MemoryKnown && pool > 0 {
				rss = m.heatStyle(float64(r.RSS)/float64(pool)*100, p.memory).Render(rss)
			}
		}
		line := format(fmt.Sprint(r.PID), name, cpu, rss, knownInt(r.Threads, r.ThreadsKnown))
		if i == m.cursor {
			line = p.accent.Reverse(true).Render(fit(line, w))
		}
		lines = append(lines, line)
	}
	if len(m.rows) == 0 {
		lines = append(lines, m.tr("No matching processes."))
	}
	return strings.Join(lines, "\n")
}
func (m *Model) settingsView() string {
	p := m.palette()
	language := map[string]string{"auto": m.tr("Follow system"), "en": "English", "zh-CN": "简体中文"}[m.settings.Language]
	theme := m.tr(m.settings.Theme)
	if m.settings.Theme == "auto" {
		mode := m.tr("dark")
		if m.light() {
			mode = m.tr("light")
		}
		theme = m.tr("Follow terminal (%s)", mode)
	}
	grid := m.tr("off")
	if m.settings.Grid {
		grid = m.tr("on")
	}
	values := []string{language, theme, fmt.Sprintf("%d ms", m.settings.IntervalMS), m.tr("%d seconds", m.settings.RangeSeconds), grid}
	labels := []string{m.tr("Language"), m.tr("Color theme"), m.tr("Sampling interval"), m.tr("Chart time range"), m.tr("Grid lines")}
	lines := []string{m.title(m.tr("Settings"), "")}
	labelWidth := min(22, max(10, (m.width-8)/2))
	for i, label := range labels {
		line := "  " + fit(label, labelWidth) + " ‹ " + values[i] + " ›"
		if m.settingsEditing && i == m.setting {
			line = p.accent.Reverse(true).Render(fit(line, m.width-2))
		}
		lines = append(lines, line)
	}
	lines = append(lines, "  "+fit(m.tr("Version"), labelWidth)+" metra "+m.version)
	return strings.Join(lines, "\n")
}

func (m *Model) modalView(w int) string {
	p := m.palette()
	t := m.target
	switch m.modal {
	case "confirm":
		return p.danger.Bold(true).Render(m.tr("End process?")) + m.tr("\n\n%s · PID %d\n\nSend SIGTERM to this process?\nUnsaved work may be lost.\n\n[y] Send SIGTERM    [n / Enter / Esc] Cancel", clean(t.Name), t.PID)
	case "loading":
		return m.tr("Loading process details…\n\nEsc to close")
	case "terminating":
		return m.tr("Sending SIGTERM…")
	case "detail":
		d := m.detail
		body := m.title(m.tr("Process details"), fmt.Sprintf("PID %d · PPID %d", d.PID, d.PPID)) + m.tr("\n\nName         %s\nUser         %s\nStatus       %s\nCPU          %s (100%% = one core)\nResident     %s\nVirtual      %s\nThreads      %s\nExecutable   %s\nCommand      %s", clean(d.Name), clean(d.User), clean(d.Status), percent(d.CPU), knownBytes(d.RSS, d.MemoryKnown), knownBytes(d.VMS, d.MemoryKnown), knownInt(d.Threads, d.ThreadsKnown), clean(d.Executable), clean(d.Command))
		if d.Err != "" {
			body += "\n" + p.muted.Render(clean(d.Err))
		}
		return ansi.Hardwrap(body, w, true) + "\n\n" + m.tr("Enter / Esc to close")
	}
	return ""
}
func (m *Model) helpView() string {
	return m.title(m.tr("Keyboard shortcuts"), "") + "\n\n" + m.tr("← / →, Tab / Shift+Tab   Switch pages\n↑ / ↓, j / k             Select process / interface, scroll hardware pages\nSpace                     Pause or resume sampling\n/                         Search process names or PID\ns / r                     Cycle process sort / reverse sort\nt                         Toggle process tree\nEnter / d                 Process details\nx / Delete                End process, then y to confirm (SIGTERM)\nc                         Toggle per-core CPU charts\n,                         Open Settings; arrows edit, Enter saves, Esc cancels\n? / F1                    Show help; Esc closes\nq / Ctrl+C                Exit metra")
}
func (m *Model) visiblePoints(key string) []chart.Point {
	points := m.history.Points(key)
	start := m.snapshot.At.Add(-time.Duration(m.settings.RangeSeconds) * time.Second)
	for i, p := range points {
		if !p.At.Before(start) {
			return points[i:]
		}
	}
	return nil
}

func (m *Model) pageHint() string {
	if m.help {
		return m.tr("↑/↓ scroll · Esc close · q close")
	}
	if m.modal != "" {
		if m.modal == "confirm" {
			return m.tr("y confirm · Enter / n / Esc cancel")
		}
		return m.tr("↑/↓ scroll · Enter / Esc close")
	}
	if m.current().Kind == "settings" {
		return m.settingsHint()
	}
	common := m.tr("←/→ tabs · space pause · ? help · q quit")
	extra := ""
	switch m.current().Kind {
	case "processes":
		extra = m.tr("↑/↓ select · / search · s sort · r reverse · t tree · Enter details")
	case "cpu":
		if len(m.snapshot.CPU.Cores) >= largeCoreThreshold {
			extra = m.tr("↑/↓ scroll")
		} else {
			extra = m.tr("c cores")
		}
	case "network":
		extra = m.tr("↑/↓ interface")
	}
	if extra != "" && ansi.StringWidth(" metra "+common+" · "+extra) <= m.width {
		return common + " · " + extra
	}
	return common
}

func (m *Model) diskTimes(i int) string {
	d := m.snapshot.Disks[i]
	var values []string
	if finite(d.ReadBusy) {
		values = append(values, m.tr("Read time %s", percent(d.ReadBusy)))
	}
	if finite(d.WriteBusy) {
		values = append(values, m.tr("Write time %s", percent(d.WriteBusy)))
	}
	if finite(d.Busy) {
		values = append(values, m.tr("Active %s", percent(d.Busy)))
	}
	return strings.Join(values, " · ")
}

func (m *Model) liveValue(v float64) string {
	if finite(v) {
		return percent(v)
	}
	return m.tr("Detected")
}
