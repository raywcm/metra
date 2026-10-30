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
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version string

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "metra:", err)
		os.Exit(1)
	}
}
func run() error {
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
	interval := flags.Duration("interval", 0, "sampling interval (500ms–10s)")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "metra — a terminal system monitor for Linux and macOS\n\nUsage: metra [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
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
	if err := settings.Validate(); err != nil {
		return err
	}
	if *interval != 0 {
		settings.IntervalMS = int(interval.Milliseconds())
		if err := settings.Validate(); err != nil {
			return err
		}
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
		fmt.Printf("metra %s · %s · %s/%s\nCPU: %s\n", version, s.Host, s.OS, s.Arch, percent(s.CPU.Percent))
		if s.CPU.MHz > 0 && !math.IsNaN(s.CPU.MHz) {
			fmt.Printf("CPU clock: %.1f MHz\n", s.CPU.MHz)
		}
		if s.CPU.TemperatureKnown {
			fmt.Printf("CPU temperature: %.1f C\n", s.CPU.Temperature)
		}
		fmt.Printf("Memory: %s", memory(float64(s.Memory.Total)))
		if s.Memory.UsageKnown {
			fmt.Printf(" · used %s (%s)", memory(float64(s.Memory.Used)), percent(s.Memory.Percent))
		}
		if s.Memory.AvailableKnown {
			fmt.Printf(" · available %s", memory(float64(s.Memory.Available)))
		}
		fmt.Println()
		fmt.Printf("Disks: %d  Interfaces: %d  GPUs: %d  NPUs: %d  Processes: %d\n", len(s.Disks), len(s.Networks), len(s.GPUs), len(s.NPUs), s.ProcessCount)
		for _, d := range s.Disks {
			fmt.Print(i18n.Text(settings.Language, "Disk %s: read %s (%s) · write %s (%s)\n", d.Name, percent(d.ReadBusy), rate(d.ReadRate), percent(d.WriteBusy), rate(d.WriteRate)))
		}
		for _, g := range s.GPUs {
			fmt.Printf("GPU %s: %s · memory %s · %s\n", g.Name, percent(g.Utilization), memory(g.MemoryUsed), g.Backend)
			printEngines(g.Engines)
			if !math.IsNaN(g.PCIeReceive) || !math.IsNaN(g.PCIeSend) {
				fmt.Printf("  PCIe RX: %s · TX: %s\n", rate(g.PCIeReceive), rate(g.PCIeSend))
			}
			for _, clock := range []struct {
				name  string
				value float64
			}{{"Graphics", g.GraphicsMHz}, {"SM", g.SMMHz}, {"Memory", g.MemoryMHz}, {"Video", g.VideoMHz}} {
				if clock.value > 0 && !math.IsNaN(clock.value) {
					fmt.Printf("  %s clock: %.1f MHz\n", clock.name, clock.value)
				}
			}
		}
		for _, n := range s.NPUs {
			fmt.Printf("NPU %s: %s · %s\n", n.Name, percent(n.Utilization), n.Backend)
			if n.MemoryTotal > 0 && !math.IsNaN(n.MemoryUsed) {
				fmt.Printf("  %s memory: %s / %s\n", n.MemoryKind, memory(n.MemoryUsed), memory(n.MemoryTotal))
			}
			if !math.IsNaN(n.Temperature) {
				fmt.Printf("  Temperature: %.1f C\n", n.Temperature)
			}
			if !math.IsNaN(n.Power) {
				fmt.Printf("  Power: %.1f W\n", n.Power)
			}
			printEngines(n.Engines)
		}
		for _, w := range s.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		return nil
	}
	_, err = tea.NewProgram(tui.New(ctx, source, settings, path, version), tea.WithContext(ctx), tea.WithFPS(30)).Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func printEngines(engines []platform.Engine) {
	for _, engine := range engines {
		if !math.IsNaN(engine.Utilization) {
			fmt.Printf("  %s: %.1f%%\n", engine.Name, engine.Utilization)
		}
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
