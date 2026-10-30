package tui

import (
	"context"
	"fmt"
	"github.com/raywcm/metra/internal/buildinfo"
	"github.com/raywcm/metra/internal/i18n"
	"image/color"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/raywcm/metra/internal/chart"
	"github.com/raywcm/metra/internal/collector"
	"github.com/raywcm/metra/internal/config"
)

type Tab struct {
	ID, Label, Kind string
	Index           int
}
type sampleMsg struct {
	withProcesses bool
	snapshot      collector.Snapshot
	elapsed       time.Duration
}
type tickMsg struct{ generation uint64 }
type themeTickMsg struct{}
type themeQueryMsg struct{}
type detailMsg struct {
	detail collector.Detail
	err    error
	target collector.Process
}
type actionMsg struct{ err error }
type saveMsg struct {
	settings config.Settings
	err      error
}

const settingsCount = 5
const largeCoreThreshold = 64

type Model struct {
	processPinned, networkPinned                             bool
	settingsEditing                                          bool
	savedSettings                                            config.Settings
	version                                                  string
	lastTab                                                  int
	dark                                                     bool
	terminalBackground                                       color.Color
	historyGap                                               bool
	generation                                               uint64
	ctx                                                      context.Context
	source                                                   collector.Source
	settings                                                 config.Settings
	configPath                                               string
	snapshot                                                 collector.Snapshot
	history                                                  chart.Store
	tabs                                                     []Tab
	tab                                                      int
	width, height                                            int
	overlayScroll                                            int
	ready, inFlight, paused, tree, reverse, cores, help      bool
	rows                                                     []processRow
	cursor, offset, scroll, overviewScroll, network, setting int
	sortBy                                                   string
	input                                                    textinput.Model
	searching                                                bool
	query                                                    string
	modal                                                    string
	target                                                   collector.Process
	detail                                                   collector.Detail
	status                                                   string
	saving                                                   bool
}

func New(ctx context.Context, source collector.Source, s config.Settings, path string, versions ...string) *Model {
	input := textinput.New()
	input.Prompt = "Search › "
	input.Placeholder = "process name or PID"
	input.CharLimit = 128
	m := &Model{dark: fallbackDark(), ctx: ctx, source: source, settings: s, configPath: path, width: 100, height: 30, history: chart.Store{}, input: input, sortBy: "cpu"}
	m.savedSettings = s
	m.version = buildinfo.Version()
	if len(versions) > 0 {
		m.version = versions[0]
	}
	m.rebuildTabs()
	m.localizeInput()
	return m
}
func (m *Model) Init() tea.Cmd {
	m.inFlight = true
	return tea.Batch(m.sample(), tea.RequestBackgroundColor, themeTick())
}
func (m *Model) sample() tea.Cmd {
	processes := m.current().Kind == "processes"
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		s := m.source.Sample(ctx, processes)
		return sampleMsg{snapshot: s, elapsed: time.Since(start), withProcesses: processes}
	}
}
func (m *Model) tick(after time.Duration) tea.Cmd {
	generation := m.generation
	return tea.Tick(max(25*time.Millisecond, after), func(time.Time) tea.Msg { return tickMsg{generation} })
}
func (m *Model) current() Tab { return m.tabs[min(m.tab, len(m.tabs)-1)] }
func (m *Model) rebuildTabs() {
	id := "overview"
	if len(m.tabs) > 0 {
		id = m.current().ID
	}
	tabs := []Tab{{"overview", "Performance", "overview", 0}, {"processes", "Processes", "processes", 0}, {"cpu", "CPU", "cpu", 0}, {"memory", "Memory", "memory", 0}}
	for i, d := range m.snapshot.Disks {
		tabs = append(tabs, Tab{"disk:" + d.ID, fmt.Sprintf("Disk%d", i), "disk", i})
	}
	tabs = append(tabs, Tab{"network", "Network", "network", 0})
	for i, g := range m.snapshot.GPUs {
		tabs = append(tabs, Tab{"gpu:" + g.ID, fmt.Sprintf("GPU%d", i), "gpu", i})
	}
	for i, n := range m.snapshot.NPUs {
		tabs = append(tabs, Tab{"npu:" + n.ID, fmt.Sprintf("NPU%d", i), "npu", i})
	}
	tabs = append(tabs, Tab{"settings", "Settings", "settings", 0})
	m.tabs = tabs
	m.tab = 0
	for i, t := range tabs {
		if t.ID == id {
			m.tab = i
			break
		}
	}
}
func (m *Model) remember() {
	s := m.snapshot
	keys := map[string]bool{}
	add := func(k string, v float64) {
		if m.historyGap {
			m.history.Add(k, s.At.Add(-time.Nanosecond), math.NaN())
		}
		m.history.Add(k, s.At, v)
		keys[k] = true
	}
	add("cpu", s.CPU.Percent)
	add("memory", s.Memory.Percent)
	if len(s.CPU.Cores) < largeCoreThreshold {
		for i, v := range s.CPU.Cores {
			id := i
			if i < len(s.CPU.IDs) {
				id = s.CPU.IDs[i]
			}
			add(fmt.Sprintf("core:%d", id), v)
		}
	}
	for _, d := range s.Disks {
		add("disk:"+d.ID+":read", d.ReadRate)
		add("disk:"+d.ID+":write", d.WriteRate)
		add("disk:"+d.ID+":read_busy", d.ReadBusy)
		add("disk:"+d.ID+":write_busy", d.WriteBusy)
		add("disk:"+d.ID+":busy", d.Busy)
		add("disk:"+d.ID+":combined", d.CombinedBusy)
	}
	for _, n := range s.Networks {
		add("net:"+n.Name+":rx", n.ReceiveRate)
		add("net:"+n.Name+":tx", n.SendRate)
	}
	add("net:total:rx", networkRate(s.Networks, true))
	add("net:total:tx", networkRate(s.Networks, false))
	for _, g := range s.GPUs {
		add("gpu:"+g.ID, g.Utilization)
		add("gpu:"+g.ID+":pcie-rx-bytes", g.PCIeReceive)
		add("gpu:"+g.ID+":pcie-tx-bytes", g.PCIeSend)
		for _, engine := range g.Engines {
			add("gpu:"+g.ID+":engine:"+engine.ID, engine.Utilization)
		}
	}
	for _, n := range s.NPUs {
		add("npu:"+n.ID, n.Utilization)
		for _, engine := range n.Engines {
			add("npu:"+n.ID+":engine:"+engine.ID, engine.Utilization)
		}
	}
	m.history.Retain(keys)
	m.historyGap = false
}
func (m *Model) refreshRows() {
	var selected collector.Process
	selectedValid := false
	pinned := m.processPinned || m.cursor > 0
	row := m.cursor - m.offset
	if m.cursor >= 0 && m.cursor < len(m.rows) && pinned {
		selected = m.rows[m.cursor].Process
		selectedValid = true
	}
	m.rows = processRows(m.snapshot.Processes, m.query, m.sortBy, m.reverse, m.tree)
	m.cursor, m.offset = 0, 0
	found := false
	for i, p := range m.rows {
		if selectedValid && p.PID == selected.PID && p.Created == selected.Created {
			m.cursor = i
			m.offset = max(0, i-row)
			found = true
			break
		}
	}
	if !found {
		m.processPinned = false
	}
	m.clampCursor()
}
func (m *Model) clampCursor() {
	n := m.processRowsVisible()
	m.cursor = max(0, min(m.cursor, len(m.rows)-1))
	m.offset = max(0, min(m.offset, max(0, len(m.rows)-n)))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+n {
		m.offset = m.cursor - n + 1
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case themeTickMsg:
		if m.settings.Theme == "auto" {
			return m, tea.Batch(themeTick(), tea.RequestBackgroundColor)
		}
		return m, themeTick()
	case themeQueryMsg:
		if m.settings.Theme == "auto" {
			return m, tea.RequestBackgroundColor
		}
	case tea.BackgroundColorMsg:
		if msg.Color != nil {
			m.dark = msg.IsDark()
			m.terminalBackground = msg.Color
			m.styleInput()
		}
	case tea.FocusMsg:
		if m.settings.Theme == "auto" {
			return m, tea.RequestBackgroundColor
		}
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		m.input.SetWidth(max(5, m.width-18))
		m.clampCursor()
	case sampleMsg:
		m.inFlight = false
		selectedNetwork := ""
		if m.networkPinned && m.network >= 0 && m.network < len(m.snapshot.Networks) {
			selectedNetwork = m.snapshot.Networks[m.network].Name
		}
		m.snapshot = msg.snapshot
		m.network = 0
		if selectedNetwork != "" {
			found := false
			for i, n := range m.snapshot.Networks {
				if n.Name == selectedNetwork {
					m.network = i
					found = true
					break
				}
			}
			if !found {
				m.networkPinned = false
			}
		}
		m.ready = true
		m.rebuildTabs()
		m.remember()
		m.refreshRows()
		if !m.paused && m.current().Kind == "processes" && !msg.withProcesses {
			m.inFlight = true
			return m, m.sample()
		}
		return m, m.tick(m.settings.Interval() - msg.elapsed)
	case tickMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		if !m.paused && !m.inFlight {
			m.inFlight = true
			return m, m.sample()
		}
		return m, m.tick(m.settings.Interval())
	case detailMsg:
		if m.modal == "loading" && m.target.PID == msg.target.PID && m.target.Created == msg.target.Created {
			if msg.err != nil {
				m.modal = ""
				m.status = m.tr("Details: %s", msg.err.Error())
			} else {
				m.detail = msg.detail
				m.modal = "detail"
			}
		}
	case actionMsg:
		m.modal = ""
		if msg.err != nil {
			m.status = m.tr("SIGTERM failed: %s", msg.err.Error())
		} else {
			m.status = m.tr("SIGTERM sent; process may take time to exit")
		}
	case saveMsg:
		m.saving = false
		if msg.err != nil {
			m.status = m.tr("Save failed: %s", msg.err.Error())
		} else {
			m.settings = msg.settings
			m.savedSettings = msg.settings
			m.settingsEditing = false
			m.status = m.tr("Settings saved")
		}
	case tea.KeyPressMsg:
		previousTab := m.current().ID
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.searching {
			switch key {
			case "enter", "esc":
				m.searching = false
				m.input.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.query = m.input.Value()
			m.processPinned = false
			m.cursor, m.offset = 0, 0
			m.refreshRows()
			return m, cmd
		}
		if m.modal != "" {
			if m.modal == "terminating" {
				return m, nil
			}
			if key == "down" || key == "j" {
				m.overlayScroll++
				return m, nil
			}
			if key == "up" || key == "k" {
				m.overlayScroll = max(0, m.overlayScroll-1)
				return m, nil
			}
			if key == "esc" || key == "n" || (key == "enter" && m.modal == "confirm") {
				m.modal = ""
				return m, nil
			}
			if m.modal == "confirm" && key == "y" {
				m.modal = "terminating"
				target := m.target
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(m.ctx, 2*time.Second)
					defer cancel()
					return actionMsg{m.source.Terminate(ctx, target)}
				}
			}
			if m.modal == "detail" && key == "enter" {
				m.modal = ""
			}
			return m, nil
		}
		if m.help {
			if key == "down" || key == "j" {
				m.overlayScroll++
				return m, nil
			}
			if key == "up" || key == "k" {
				m.overlayScroll = max(0, m.overlayScroll-1)
				return m, nil
			}
			if key == "?" || key == "esc" || key == "q" {
				m.help = false
			}
			return m, nil
		}
		if m.current().Kind == "settings" {
			return m, m.settingsKey(key)
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "?", "f1":
			m.help = true
			m.overlayScroll = 0
		case "left", "shift+tab":
			m.selectTab(m.tab - 1)
		case "right", "tab":
			m.selectTab(m.tab + 1)
		case ",":
			m.selectTab(len(m.tabs) - 1)
			m.settingsEditing = true
			m.status = ""
		case "space":
			m.paused = !m.paused
			if !m.paused {
				m.historyGap = true
			}
			if !m.paused && !m.inFlight {
				m.generation++
				m.inFlight = true
				return m, m.sample()
			}
		case "c":
			if m.current().Kind == "cpu" {
				m.cores = !m.cores
				m.scroll = 0
			}
		case "esc":
			m.query = ""
			m.processPinned = false
			m.cursor, m.offset = 0, 0
			m.input.SetValue("")
			m.refreshRows()
		default:
			return m, m.pageKey(key)
		}
		if m.current().Kind == "processes" && m.current().ID != previousTab && !m.inFlight && !m.paused {
			m.generation++
			m.inFlight = true
			return m, m.sample()
		}
	}
	if m.searching {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m *Model) pageKey(key string) tea.Cmd {
	switch m.current().Kind {
	case "processes":
		switch key {
		case "up", "k":
			m.processPinned = true
			m.cursor--
		case "down", "j":
			m.processPinned = true
			m.cursor++
		case "pgup":
			m.processPinned = true
			m.cursor -= m.processRowsVisible()
		case "pgdown":
			m.processPinned = true
			m.cursor += m.processRowsVisible()
		case "home", "g":
			m.processPinned = false
			m.offset = 0
			m.cursor = 0
		case "end", "G":
			m.processPinned = true
			m.cursor = len(m.rows) - 1
		case "/":
			m.processPinned = false
			m.cursor, m.offset = 0, 0
			m.searching = true
			return m.input.Focus()
		case "t":
			m.tree = !m.tree
			m.refreshRows()
		case "s":
			m.processPinned = false
			m.cursor, m.offset = 0, 0
			orders := []string{"cpu", "memory", "pid", "name"}
			for i, o := range orders {
				if o == m.sortBy {
					m.sortBy = orders[(i+1)%len(orders)]
					break
				}
			}
			m.refreshRows()
		case "r":
			m.processPinned = false
			m.cursor, m.offset = 0, 0
			m.reverse = !m.reverse
			m.refreshRows()
		case "enter", "d":
			if len(m.rows) > 0 {
				m.target = m.rows[m.cursor].Process
				m.modal = "loading"
				m.overlayScroll = 0
				target := m.target
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(m.ctx, 2*time.Second)
					defer cancel()
					d, e := m.source.Detail(ctx, target)
					return detailMsg{d, e, target}
				}
			}
		case "delete", "backspace", "x":
			if len(m.rows) > 0 {
				m.target = m.rows[m.cursor].Process
				m.modal = "confirm"
				m.overlayScroll = 0
			}
		}
		m.clampCursor()
	case "settings":
		return m.settingsKey(key)
	case "network":
		switch key {
		case "up", "k":
			m.networkPinned = true
			m.network = max(0, m.network-1)
		case "down", "j":
			m.networkPinned = true
			m.network = min(max(0, len(m.snapshot.Networks)-1), m.network+1)
		case "home":
			m.network, m.networkPinned = 0, false
		}
	default:
		switch key {
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll++
		case "pgdown":
			m.scroll += max(1, m.height-10)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height-10))
		case "home":
			m.scroll = 0
		case "end":
			m.scroll = m.hardwareScrollMax()
		}
		m.scroll = min(m.scroll, m.hardwareScrollMax())
	}
	return nil
}
func (m *Model) changeSetting(direction int) {
	cycle := func(values []int, current int) int {
		idx := 0
		for i, v := range values {
			if v >= current {
				idx = i
				break
			}
		}
		return values[(idx+direction+len(values))%len(values)]
	}
	switch m.setting {
	case 2:
		m.settings.IntervalMS = cycle([]int{500, 1000, 2000, 5000, 10000}, m.settings.IntervalMS)
	case 3:
		m.settings.RangeSeconds = cycle([]int{30, 60, 120, 300, 900}, m.settings.RangeSeconds)
	case 1:
		themes := []string{"auto", "dark", "light", "forest"}
		if m.settings.Theme == "midnight" {
			m.settings.Theme = "dark"
		}
		for i, t := range themes {
			if t == m.settings.Theme {
				m.settings.Theme = themes[(i+direction+len(themes))%len(themes)]
				break
			}
		}
	case 4:
		m.settings.Grid = !m.settings.Grid
	case 0:
		languages := []string{"auto", "en", "zh-CN"}
		for i, lang := range languages {
			if m.settings.Language == lang {
				m.settings.Language = languages[(i+direction+len(languages))%len(languages)]
				break
			}
		}
		m.localizeInput()
	}
	m.styleInput()
	m.status = ""
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func networkRate(networks []collector.Network, receive bool) float64 {
	sum := 0.0
	valid := false
	for _, n := range networks {
		if n.ExcludeFromTotals {
			continue
		}
		v := n.SendRate
		if receive {
			v = n.ReceiveRate
		}
		if finite(v) {
			sum += v
			valid = true
		}
	}
	if !valid {
		return math.NaN()
	}
	return sum
}

func (m *Model) tr(key string, args ...any) string {
	return i18n.Text(m.settings.Language, key, args...)
}
func (m *Model) localizeInput() {
	m.styleInput()
	m.input.Prompt = m.tr("Search › ")
	m.input.Placeholder = m.tr("process name or PID")
}
func fallbackDark() bool {
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	if n, e := strconv.Atoi(parts[len(parts)-1]); e == nil {
		return n != 7 && n != 15
	}
	return true
}
func (m *Model) light() bool {
	return m.settings.Theme == "light" || (m.settings.Theme == "auto" && !m.dark)
}

func themeTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return themeTickMsg{} })
}

func (m *Model) styleInput() {
	if m.light() {
		m.input.SetStyles(textinput.DefaultLightStyles())
	} else {
		m.input.SetStyles(textinput.DefaultDarkStyles())
	}
}

func (m *Model) selectTab(tab int) {
	if m.current().Kind == "settings" {
		m.settings = m.savedSettings
		m.settingsEditing = false
		m.localizeInput()
		m.status = ""
	} else {
		m.lastTab = m.tab
	}
	m.tab = (tab + len(m.tabs)) % len(m.tabs)
	m.scroll = 0
}
func (m *Model) settingsKey(key string) tea.Cmd {
	if m.saving {
		return nil
	}
	switch key {
	case "tab":
		m.selectTab(m.tab + 1)
		return nil
	case "shift+tab":
		m.selectTab(m.tab - 1)
		return nil
	case "?", "f1":
		m.help = true
		m.overlayScroll = 0
		return nil
	case ",":
		m.settingsEditing = true
		m.status = ""
		return nil
	}
	if !m.settingsEditing {
		switch key {
		case "left", "h":
			m.selectTab(m.tab - 1)
		case "right", "l":
			m.selectTab(m.tab + 1)
		case "enter":
			m.settingsEditing = true
			m.status = ""
		case "down", "j":
			m.settingsEditing = true
			m.setting = 0
			m.status = ""
		case "up", "k":
			m.settingsEditing = true
			m.setting = settingsCount - 1
			m.status = ""
		case "esc":
			m.selectTab(m.lastTab)
		case "q":
			return tea.Quit
		}
		return nil
	}
	switch key {
	case "esc", "q":
		m.settings = m.savedSettings
		m.settingsEditing = false
		m.status = ""
		m.localizeInput()
	case "up", "k":
		m.setting = (m.setting + settingsCount - 1) % settingsCount
	case "down", "j":
		m.setting = (m.setting + 1) % settingsCount
	case "left", "h", "-":
		m.changeSetting(-1)
	case "right", "l", "space", "+", "=":
		m.changeSetting(1)
	case "enter", "s":
		m.saving = true
		s, path := m.settings, m.configPath
		return func() tea.Msg { return saveMsg{settings: s, err: config.Save(path, s)} }
	}
	if m.setting == 1 && m.settings.Theme == "auto" {
		return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return themeQueryMsg{} })
	}
	return nil
}
func (m *Model) settingsHint() string {
	if m.settingsEditing {
		return m.tr("↑/↓ select · ←/→ change · Enter save · Esc cancel")
	}
	return m.tr("Enter edit · ←/→ tabs · Esc back · q quit")
}

func (m *Model) largeCoreScrollMax() int {
	width, height, _ := bodySize(m.width, m.height)
	return max(0, lineCount(m.cpuView(width, height))-height)
}
