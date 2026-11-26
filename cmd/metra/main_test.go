//go:build linux || darwin

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raywcm/metra/internal/collector"
	"github.com/raywcm/metra/internal/platform"
)

func TestSnapshotLanguage(t *testing.T) {
	s := collector.Snapshot{
		CPU:    collector.CPU{MHz: 2000},
		Memory: collector.Memory{Total: 8 << 20, Used: 2 << 20, Available: 6 << 20, Percent: 25, UsageKnown: true, AvailableKnown: true},
		Disks:  []collector.Disk{{Name: "disk0"}},
		GPUs:   []platform.GPU{{Name: "Test", GraphicsMHz: 800, Engines: []platform.Engine{{Name: "Renderer", Utilization: 40}}}},
		NPUs:   []platform.NPU{{Name: "Test", MemoryKind: "HBM", MemoryTotal: 4 << 20, Power: 20}},
	}
	for lang, labels := range map[string][]string{
		"en":    {"CPU clock: 2000.0 MHz", "Memory: 8.0 MiB · used 2.0 MiB (25.0%) · available 6.0 MiB", "Disks: 1  Interfaces: 0  GPUs: 1  NPUs: 1  Processes: 0", "Disk disk0: read", "Renderer: 40.0%", "Graphics clock: 800.0 MHz", "HBM memory:", "Power: 20.0 W"},
		"zh-CN": {"CPU 频率：2000.0 MHz", "内存：8.0 MiB · 已用 2.0 MiB（25.0%） · 可用 6.0 MiB", "磁盘：1  网络接口：0  GPU：1  NPU：1  进程：0", "磁盘 disk0：读取", "渲染器：40.0%", "图形频率：800.0 MHz", "HBM 内存：", "功耗：20.0 W"},
	} {
		var out bytes.Buffer
		writeSnapshot(&out, s, lang, "test")
		for _, label := range labels {
			if !strings.Contains(out.String(), label) {
				t.Errorf("%s snapshot missing %q in %s", lang, label, &out)
			}
		}
	}
}

func TestSnapshotUnavailableInventories(t *testing.T) {
	for lang, want := range map[string]string{"en": "Disks: —  Interfaces: —  GPUs: 0  NPUs: 0  Processes: —", "zh-CN": "磁盘：—  网络接口：—  GPU：0  NPU：0  进程：—"} {
		var out bytes.Buffer
		writeSnapshot(&out, collector.Snapshot{DiskInventoryUnavailable: true, NetworkInventoryUnavailable: true, ProcessCountUnavailable: true}, lang, "test")
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, &out)
		}
	}
}

func TestSnapshotDistinguishesUnknownMemoryFromZeroLimit(t *testing.T) {
	var s collector.Snapshot
	for _, tc := range []struct {
		known bool
		want  string
	}{{false, "Memory: —\n"}, {true, "Memory: 0.0 MiB\n"}} {
		var out bytes.Buffer
		s.Limits.MemoryLimitKnown = tc.known
		writeSnapshot(&out, s, "en", "test")
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("expected %q in %s", tc.want, &out)
		}
	}
}

func TestInvalidExplicitInterval(t *testing.T) {
	for _, interval := range []string{"0", "499ms", "10.0001s"} {
		args := []string{"--snapshot", "--config", filepath.Join(t.TempDir(), "settings.json"), "--interval", interval}
		if err := run(args); err == nil {
			t.Errorf("accepted interval outside supported range: %s", interval)
		}
	}
}
