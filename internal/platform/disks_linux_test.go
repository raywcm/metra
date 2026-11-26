//go:build linux

package platform

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/common"
	"golang.org/x/sys/unix"
)

func TestPhysicalDisksExcludeEmptyDevices(t *testing.T) {
	root := t.TempDir()
	for name, sectors := range map[string]string{"nvme0n1": "10000", "dm-0": "5000", "sr0": "0", "nbd0": "0", "loop0": "1000", "zram0": "500"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "size"), []byte(sectors), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"nvme0n1", "dm-0"} {
		if !physicalDiskAt(root, name) {
			t.Fatal("real data disk filtered", name)
		}
	}
	for _, name := range []string{"sr0", "nbd0", "loop0", "zram0", "absent"} {
		if physicalDiskAt(root, name) {
			t.Fatal("empty or auxiliary device retained", name)
		}
	}
	alias := filepath.Join(root, "root-volume")
	if err := os.Symlink(filepath.Join(root, "dm-0"), alias); err != nil {
		t.Fatal(err)
	}
	if !MountBelongs(alias, "dm-0") {
		t.Fatal("mapped LVM alias lost its physical association")
	}
}

func TestMountBelongsToNumberedLinuxDisk(t *testing.T) {
	for _, tc := range []struct {
		device, disk string
		want         bool
	}{
		{"nvme0n10", "nvme0n1", false},
		{"dm-10", "dm-1", false},
		{"nvme0n1p10", "nvme0n1", true},
		{"mmcblk0p1", "mmcblk0", true},
		{"sda10", "sda", true},
	} {
		if got := MountBelongs("/dev/"+tc.device, tc.disk); got != tc.want {
			t.Errorf("%s assigned to %s = %v", tc.device, tc.disk, got)
		}
	}
}

func TestDiskInventoryIndependentOfCounterFiles(t *testing.T) {
	base := t.TempDir()
	sys, proc := filepath.Join(base, "sys"), filepath.Join(base, "proc")
	write := func(path, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(sys, "block")
	for _, name := range []string{"sda", "sdb", "sdc", "loop0"} {
		write(filepath.Join(root, name, "size"), "4096\n")
	}
	write(filepath.Join(root, "sda", "diskseq"), "9\n")
	write(filepath.Join(root, "sdb", "stat"), "0 0 0 0 0 0 0 0 0 0 0\n")
	procFile := filepath.Join(proc, "diskstats")
	write(procFile, "8 0 sda 2 0 4 5 3 0 6 7 0 4 1\n8 16 sdb bad\n8 32 sdc 1 0 2 4 1 0 0 1 0 2 3\n8 1 sda1 1 0 1 1 1 0 1 1 0 1 1\n")
	ctx := context.WithValue(context.Background(), common.EnvKey, common.EnvMap{common.HostSysEnvKey: sys, common.HostProcEnvKey: proc})
	read := func() (DiskInventory, error) { return ReadDisks(ctx, []string{"sda", "sdb", "sdc"}) }
	first, err := read()
	if err != nil || first.Incomplete || len(first.Devices) != 3 || first.Devices["sda"].Sequence != 9 {
		t.Fatal("unexpected inventory", first, err)
	}
	if a := first.Devices["sda"].Counters; a == nil || a.ReadBytes != 2048 || a.WriteBytes != 3072 || a.IoTime != 4 {
		t.Fatal("bulk counter units", a)
	}
	if b := first.Devices["sdb"].Counters; b == nil || b.ReadBytes != 0 {
		t.Fatal("idle disk not recovered from sysfs", b)
	}
	if c := first.Devices["sdc"].Counters; c == nil || c.ReadBytes != 1024 {
		t.Fatal("valid row after malformed row lost", c)
	}
	if err := os.Remove(procFile); err != nil {
		t.Fatal(err)
	}
	partial, err := read()
	if err == nil || partial.Incomplete || len(partial.Devices) != 3 || partial.Devices["sda"].Counters != nil || partial.Devices["sdb"].Counters == nil {
		t.Fatal("unreadable counters invalidated the inventory or lost the idle disk", partial, err)
	}
	if err := os.Rename(root, root+"-hidden"); err != nil {
		t.Fatal(err)
	}
	write(procFile, "8 0 sda 2 0 4 5 3 0 6 7 0 4 1\n")
	unknown, err := read()
	if err == nil || !unknown.Incomplete || len(unknown.Devices) != 3 || unknown.Devices["sda"].Counters == nil {
		t.Fatal("unavailable inventory erased existing counter rows", unknown, err)
	}
}

func TestDiskReplacedDuringBulkRead(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "block", "vda")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	seq := filepath.Join(dir, "diskseq")
	if err := os.WriteFile(seq, []byte("10"), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "diskstats")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), common.EnvKey, common.EnvMap{common.HostSysEnvKey: root, common.HostProcEnvKey: root})
	updated := make(chan error, 1)
	go func() {
		// Opening the writer synchronizes with the collector's bulk read.
		f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err == nil {
			defer f.Close()
			err = os.WriteFile(seq, []byte("11"), 0600)
		}
		if err == nil {
			_, err = f.WriteString("8 0 vda 1 0 999 0 0 0 0 0 0 0 0\n")
		}
		updated <- err
	}()
	inv, err := ReadDisks(ctx, nil)
	if writeErr := <-updated; writeErr != nil {
		t.Fatal(writeErr)
	}
	v := inv.Devices["vda"]
	if err != nil || v.Sequence != 11 || v.Counters != nil {
		t.Fatal("old counters attributed to the new attachment", v, err)
	}
}
