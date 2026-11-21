//go:build linux

package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/common"
	"github.com/shirou/gopsutil/v4/disk"
)

func ReadDisks(ctx context.Context, previous []string) (DiskInventory, error) {
	env, _ := ctx.Value(common.EnvKey).(common.EnvMap)
	sys := cmp.Or(env[common.HostSysEnvKey], os.Getenv("HOST_SYS"), "/sys")
	proc := cmp.Or(env[common.HostProcEnvKey], os.Getenv("HOST_PROC"), "/proc")
	root := filepath.Join(sys, "block")
	entries, listErr := os.ReadDir(root)
	inv := DiskInventory{Devices: make(map[string]DiskReading), Incomplete: listErr != nil}
	if listErr != nil {
		for _, name := range previous {
			inv.Devices[name] = DiskReading{Sequence: diskSequence(filepath.Join(root, name))}
		}
	}
	for _, entry := range entries {
		if physicalDiskAt(root, entry.Name()) {
			inv.Devices[entry.Name()] = DiskReading{Sequence: diskSequence(filepath.Join(root, entry.Name()))}
		}
	}
	// The bulk file avoids opening a stat file per disk on large systems.
	// A bad or missing row falls back independently to that device's file.
	bulk := make(map[string]*disk.IOCountersStat, len(inv.Devices))
	data, _ := os.ReadFile(filepath.Join(proc, "diskstats"))
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[2]
		if _, found := inv.Devices[name]; !found {
			if listErr == nil || filepath.Base(name) != name || name == "." || name == ".." || !physicalDiskAt(root, name) {
				continue
			}
			// Its identity was unavailable before the bulk read. Read stat
			// after identifying the disk so the two refer to the same attachment.
			inv.Devices[name] = DiskReading{Sequence: diskSequence(filepath.Join(root, name))}
			continue
		}
		bulk[name] = parseBlockStat(fields[3:])
	}
	firstErr := listErr
	for _, name := range slices.Sorted(maps.Keys(inv.Devices)) {
		reading := inv.Devices[name]
		if err := ctx.Err(); err != nil {
			return inv, errors.Join(firstErr, err)
		}
		dir := filepath.Join(root, name)
		reading.Counters = bulk[name]
		if reading.Counters == nil {
			stat, err := os.ReadFile(filepath.Join(dir, "stat"))
			if err == nil {
				reading.Counters = parseBlockStat(strings.Fields(string(stat)))
				if reading.Counters == nil {
					err = fmt.Errorf("%s: invalid block I/O statistics", name)
				}
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		// A hotplug between inventory and counter reads invalidates the
		// measurement, even when the new device has larger counters.
		if seq := diskSequence(dir); seq != reading.Sequence {
			reading.Counters = nil
			if seq != 0 {
				reading.Sequence = seq
			}
		}
		inv.Devices[name] = reading
	}
	return inv, firstErr
}

func diskSequence(dir string) uint64 {
	data, _ := os.ReadFile(filepath.Join(dir, "diskseq"))
	seq, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return seq
}

func parseBlockStat(fields []string) *disk.IOCountersStat {
	if len(fields) < 11 {
		return nil
	}
	var v [11]uint64
	for i := range v {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return nil
		}
		v[i] = n
	}
	// Linux reports sectors of 512 bytes regardless of the hardware block size.
	if v[2] > math.MaxUint64/512 || v[6] > math.MaxUint64/512 {
		return nil
	}
	return &disk.IOCountersStat{
		ReadCount: v[0], ReadBytes: v[2] * 512, ReadTime: v[3],
		WriteCount: v[4], WriteBytes: v[6] * 512, WriteTime: v[7], IoTime: v[9],
	}
}

func physicalDiskAt(root, name string) bool {
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
		return false
	}
	_, err := os.Stat(filepath.Join(root, name))
	if err != nil {
		return false
	}
	if data, e := os.ReadFile(filepath.Join(root, name, "size")); e == nil {
		if sectors, e := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); e == nil && sectors == 0 {
			return false
		}
	}
	return true
}
func MountBelongs(device, disk string) bool {
	if resolved, err := filepath.EvalSymlinks(device); err == nil {
		device = resolved
	}
	name := filepath.Base(device)
	if name == disk {
		return true
	}
	suffix := strings.TrimPrefix(name, disk)
	if suffix == name || suffix == "" {
		return false
	}
	// The kernel inserts 'p' before the partition number when the disk
	// name ends in a digit (e.g. nvme0n1p2 versus the disk nvme0n12).
	if disk == "" {
		return false
	}
	if last := disk[len(disk)-1]; last >= '0' && last <= '9' {
		var ok bool
		suffix, ok = strings.CutPrefix(suffix, "p")
		if !ok {
			return false
		}
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return suffix != ""
}
