//go:build linux || darwin

package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/raywcm/metra/internal/buildinfo"
	"github.com/raywcm/metra/internal/collector"
	"github.com/raywcm/metra/internal/config"
	"github.com/raywcm/metra/internal/i18n"
	"github.com/raywcm/metra/internal/platform"
	"github.com/raywcm/metra/internal/tui"
	"io"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version string

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "metra:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if version == "" {
		version = buildinfo.Version()
	}
	flags := flag.NewFlagSet("metra", flag.ContinueOnError)
	showVersion := flags.Bool("version", false, "print version")
	snapshot := flags.Bool("snapshot", false, "print a system summary without opening the TUI")
	configFile := flags.String("config", "", "settings file path")
	language := flags.String("language", "", "UI language: auto, en, zh-CN")
	theme := flags.String("theme", "", "color theme: auto, dark, light, forest")
	chartStyle := flags.String("chart-style", "blocks", "compatibility option; only blocks is supported")
	var interval time.Duration
	flags.Func("interval", "sampling interval (500ms–10s)", func(value string) error {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		if duration < config.MinIntervalMS*time.Millisecond || duration > config.MaxIntervalMS*time.Millisecond {
			return fmt.Errorf("interval must be %d–%d ms", config.MinIntervalMS, config.MaxIntervalMS)
		}
		interval = duration
		return nil
	})
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "metra — a terminal system monitor for Linux and macOS\n\nUsage: metra [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if *showVersion {
		fmt.Println("metra", version)
		return nil
	}
	path := *configFile
	if path == "" {
		var err error
		path, err = config.Path()
		if err != nil {
			return err
		}
	}
	settings, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "metra: settings warning:", err, "(using defaults)")
	}
	if *language != "" {
		settings.Language = *language
	}
	if *theme != "" {
		settings.Theme = *theme
	}
	if *chartStyle != "blocks" {
		return fmt.Errorf("unsupported chart style %q: only blocks is supported", *chartStyle)
	}
	if interval != 0 {
		settings.IntervalMS = int(interval.Milliseconds())
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	source := collector.New()
	defer source.Close()
	if *snapshot {
		first, c := context.WithTimeout(ctx, 5*time.Second)
		source.Sample(first, false)
		c()
		timer := time.NewTimer(settings.Interval())
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		second, c := context.WithTimeout(ctx, 5*time.Second)
		defer c()
		s := source.Sample(second, false)
		writeSnapshot(os.Stdout, s, settings.Language, version)
		for _, w := range s.Warnings {
			fmt.Fprintln(os.Stderr, i18n.Text(settings.Language, "warning: %s", w))
		}
		return nil
	}
	_, err = tea.NewProgram(tui.New(ctx, source, settings, path, version), tea.WithContext(ctx), tea.WithFPS(30)).Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func writeSnapshot(w io.Writer, s collector.Snapshot, language, version string) {
	tr := func(key string, args ...any) string { return i18n.Text(language, key, args...) }
	line := func(key string, args ...any) { fmt.Fprintln(w, tr(key, args...)) }
	engines := func(values []platform.Engine) {
		for _, engine := range values {
			if !math.IsNaN(engine.Utilization) {
				line("  %s: %.1f%%", tr(engine.Name), engine.Utilization)
			}
		}
	}

	fmt.Fprintf(w, "metra %s · %s · %s/%s\n", version, s.Host, s.OS, s.Arch)
	line("CPU: %s", percent(s.CPU.Percent))
	if s.CPU.MHz > 0 && !math.IsNaN(s.CPU.MHz) {
		line("CPU clock: %.1f MHz", s.CPU.MHz)
	}
	if s.CPU.TemperatureKnown {
		line("CPU temperature: %.1f C", s.CPU.Temperature)
	}
	total := "—"
	if s.Memory.Total > 0 || s.Limits.MemoryLimitKnown {
		total = memory(float64(s.Memory.Total))
	}
	usage := tr("Memory: %s", total)
	if s.Memory.UsageKnown {
		usage += " · " + tr("used %s (%s)", memory(float64(s.Memory.Used)), percent(s.Memory.Percent))
	}
	if s.Memory.AvailableKnown {
		usage += " · " + tr("available %s", memory(float64(s.Memory.Available)))
	}
	fmt.Fprintln(w, usage)
	line("Disks: %d  Interfaces: %d  GPUs: %d  NPUs: %d  Processes: %d", len(s.Disks), len(s.Networks), len(s.GPUs), len(s.NPUs), s.ProcessCount)
	for _, d := range s.Disks {
		fmt.Fprint(w, tr("Disk %s: read %s (%s) · write %s (%s)\n", d.Name, percent(d.ReadBusy), rate(d.ReadRate), percent(d.WriteBusy), rate(d.WriteRate)))
	}
	for _, g := range s.GPUs {
		line("GPU %s: %s · memory %s · %s", g.Name, percent(g.Utilization), memory(g.MemoryUsed), g.Backend)
		engines(g.Engines)
		if !math.IsNaN(g.PCIeReceive) || !math.IsNaN(g.PCIeSend) {
			line("  PCIe RX: %s · TX: %s", rate(g.PCIeReceive), rate(g.PCIeSend))
		}
		for _, clock := range []struct {
			label string
			mhz   float64
		}{{"Graphics", g.GraphicsMHz}, {"SM", g.SMMHz}, {"Memory", g.MemoryMHz}, {"Video", g.VideoMHz}} {
			if clock.mhz > 0 && !math.IsNaN(clock.mhz) {
				line("  %s clock: %.1f MHz", tr(clock.label), clock.mhz)
			}
		}
	}
	for _, n := range s.NPUs {
		line("NPU %s: %s · %s", n.Name, percent(n.Utilization), n.Backend)
		if n.MemoryTotal > 0 && !math.IsNaN(n.MemoryUsed) {
			line("  %s memory: %s / %s", n.MemoryKind, memory(n.MemoryUsed), memory(n.MemoryTotal))
		}
		if !math.IsNaN(n.Temperature) {
			line("  Temperature: %.1f C", n.Temperature)
		}
		if !math.IsNaN(n.Power) {
			line("  Power: %.1f W", n.Power)
		}
		engines(n.Engines)
	}
}

func percent(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", v)
}

func rate(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.0f B/s", v)
}

func memory(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	return fmt.Sprintf("%.1f MiB", v/(1024*1024))
}
