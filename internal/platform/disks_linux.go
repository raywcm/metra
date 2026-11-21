//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func PhysicalDisk(name string) bool {
	return physicalDiskAt("/sys/block", name)
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
