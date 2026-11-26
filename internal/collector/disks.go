package collector

import (
	"maps"
	"math"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/disk"
)

type diskBaseline struct {
	at       time.Time
	sequence uint64
	counters disk.IOCountersStat
}

func (c *Collector) sampleDisks(now time.Time, inventory platform.DiskInventory) []Disk {
	next := make(map[string]diskBaseline, len(inventory.Devices))
	disks := make([]Disk, 0, len(inventory.Devices))
	for _, name := range slices.Sorted(maps.Keys(inventory.Devices)) {
		reading := inventory.Devices[name]
		before := c.disks[name]
		if reading.Sequence != 0 && before.sequence != reading.Sequence {
			before = diskBaseline{sequence: reading.Sequence}
		}
		d := Disk{
			ID: name, Name: name,
			ServiceTimeOnly: runtime.GOOS == "darwin", CountersUnavailable: reading.Counters == nil,
			ReadRate: missing(), WriteRate: missing(),
			Busy: missing(), CombinedBusy: missing(), ReadBusy: missing(), WriteBusy: missing(),
		}
		if before.sequence != 0 {
			d.ID = name + "@" + strconv.FormatUint(before.sequence, 10)
		}
		if v := reading.Counters; v != nil {
			d.ReadBytes, d.WriteBytes, d.ReadOps, d.WriteOps = v.ReadBytes, v.WriteBytes, v.ReadCount, v.WriteCount
			if !before.at.IsZero() {
				elapsed := now.Sub(before.at).Seconds()
				old := before.counters
				d.ReadRate = rate(v.ReadBytes, old.ReadBytes, elapsed)
				d.WriteRate = rate(v.WriteBytes, old.WriteBytes, elapsed)
				if !d.ServiceTimeOnly || v.ReadTime != 0 || v.WriteTime != 0 || old.ReadTime != 0 || old.WriteTime != 0 {
					d.ReadBusy = math.Min(100, rate(v.ReadTime, old.ReadTime, elapsed)/10)
					d.WriteBusy = math.Min(100, rate(v.WriteTime, old.WriteTime, elapsed)/10)
				}
				if d.ServiceTimeOnly {
					d.CombinedBusy = math.Min(100, d.ReadBusy+d.WriteBusy)
				} else {
					d.Busy = math.Min(100, rate(v.IoTime, old.IoTime, elapsed)/10)
					d.CombinedBusy = d.Busy
				}
			}
			before.at, before.counters = now, *v
		}
		next[name] = before
		disks = append(disks, d)
	}
	c.disks = next
	return disks
}
