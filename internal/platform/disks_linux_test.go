//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"testing"
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
