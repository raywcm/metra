package platform

import "github.com/shirou/gopsutil/v4/disk"

type DiskReading struct {
	Sequence uint64 // Linux identifies each attachment, even when its name is reused.
	Counters *disk.IOCountersStat
}

type DiskInventory struct {
	Devices    map[string]DiskReading
	Incomplete bool
}
