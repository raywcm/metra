//go:build darwin

package platform

import (
	"context"
	"path/filepath"
	"regexp"

	"github.com/shirou/gopsutil/v4/disk"
)

var physicalDisk = regexp.MustCompile(`^disk[0-9]+$`)

func ReadDisks(ctx context.Context, previous []string) (DiskInventory, error) {
	counters, err := disk.IOCountersWithContext(ctx)
	inv := DiskInventory{Devices: make(map[string]DiskReading), Incomplete: err != nil}
	if err != nil {
		for _, name := range previous {
			inv.Devices[name] = DiskReading{}
		}
	}
	for name, value := range counters {
		if physicalDisk.MatchString(name) {
			inv.Devices[name] = DiskReading{Counters: &value}
		}
	}
	return inv, err
}

func MountBelongs(device, disk string) bool {
	name := filepath.Base(device)
	return name == disk || regexp.MustCompile(`^`+regexp.QuoteMeta(disk)+`s[0-9]+(s[0-9]+)?$`).MatchString(name)
}
