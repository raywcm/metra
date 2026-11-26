package collector

import (
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/disk"
)

func TestDiskCounterRecoveryAndReplacement(t *testing.T) {
	var c Collector
	start := time.Now()
	inv := platform.DiskInventory{Devices: map[string]platform.DiskReading{}}
	sample := func(seconds int) []Disk { return c.sampleDisks(start.Add(time.Duration(seconds)*time.Second), inv) }
	set := func(name string, seq, bytes, busy uint64) {
		inv.Devices[name] = platform.DiskReading{Sequence: seq, Counters: &disk.IOCountersStat{ReadBytes: bytes, IoTime: busy, ReadTime: busy + 1}}
	}
	set("sda", 1, 100, 20)
	set("sdb", 2, 200, 30)
	first := sample(0)
	if len(first) != 2 || !math.IsNaN(first[0].ReadRate) {
		t.Fatal(first)
	}
	set("sda", 1, 120, 25)
	inv.Devices["sdb"] = platform.DiskReading{}
	partial := sample(1)
	if partial[0].ReadRate != 20 || !partial[1].CountersUnavailable || !math.IsNaN(partial[1].ReadRate) || partial[1].ID != first[1].ID {
		t.Fatal("partial read discarded a good disk or repeated old counters", partial)
	}
	set("sda", 1, 140, 3)
	inv.Devices["sda"].Counters.ReadTime = 50 // Only the overall busy counter reset.
	set("sdb", 2, 260, 32)
	recovered := sample(3)
	if recovered[0].ReadRate != 10 || recovered[1].ReadRate != 20 {
		t.Fatal("recovery used the timestamp of another disk", recovered)
	}
	if runtime.GOOS == "linux" && !math.IsNaN(recovered[0].CombinedBusy) {
		t.Fatal("reset busy counter yielded a fabricated utilization", recovered[0])
	}
	set("sda", 3, 1000000, 1000)
	replaced := sample(4)
	if replaced[0].ID == first[0].ID || !math.IsNaN(replaced[0].ReadRate) {
		t.Fatal("new attachment reused the former disk's history or counters", replaced)
	}
	delete(inv.Devices, "sdb")
	if got := sample(5); len(got) != 1 || len(c.disks) != 1 {
		t.Fatalf("departed disk remained in inventory: %+v", got)
	}
}
