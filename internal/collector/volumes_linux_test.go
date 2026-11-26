//go:build linux

package collector

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkVolumeEnumeratedWithoutPhysicalDisk(t *testing.T) {
	root := t.TempDir()
	export := filepath.Join(root, "export")
	if err := os.Mkdir(export, 0700); err != nil {
		t.Fatal(err)
	}
	// Point the Linux mount parser at a fixture. statfs runs on a real
	// temporary directory, so this test needs no NFS server or mount rights.
	mountInfo := filepath.Join(root, "mountinfo")
	path := strings.ReplaceAll(export, " ", `\040`)
	if err := os.WriteFile(mountInfo, []byte(fmt.Sprintf("42 1 0:9 / %s rw - nfs4 server:/exports/sda1 rw\n", path)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_PROC_MOUNTINFO", mountInfo)
	found := false
	ctx := context.Background()
	targets, err := readMounts(ctx, []string{"sda"})
	for _, target := range targets {
		if target.path == export {
			found = true
			m := mountUsage(ctx, target.path, target.fsType)
			if !target.remote || target.disk != "" || m.FSType != "nfs4" || m.Total == 0 {
				t.Errorf("invalid network volume: target %+v, mount %+v", target, m)
			}
		}
	}
	if err != nil || !found {
		t.Fatal("network volume missing", err)
	}
}

func TestListedVolumeWithUnreadableCapacityRemainsVisible(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "unreachable")
	mountInfo := filepath.Join(root, "mountinfo")
	if err := os.WriteFile(mountInfo, []byte(fmt.Sprintf("42 1 0:9 / %s rw - nfs4 server:/export rw\n", path)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_PROC_MOUNTINFO", mountInfo)
	var mounts map[string]Mount
	probe := func() {
		t.Helper()
		mounts = make(map[string]Mount)
		ctx := context.Background()
		targets, err := readMounts(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			mounts[target.path] = mountUsage(ctx, target.path, target.fsType)
		}
		if _, ok := mounts["/"]; !ok {
			t.Fatal("container root missing")
		}
	}
	probe()
	if m, ok := mounts[path]; !ok || !m.CapacityUnavailable || m.FSType != "nfs4" {
		t.Fatal("mount vanished when statfs failed", mounts)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	probe()
	if m, ok := mounts[path]; !ok || m.CapacityUnavailable || m.Total == 0 {
		t.Fatal("capacity did not recover", mounts)
	}
}

func TestLinuxVolumeStackUsesVisibleParent(t *testing.T) {
	// Intentionally out of order. A pseudo mount at /data hides the lower
	// tree, but the NFS mount attached to its upper layer is still reachable.
	const table = `15 13 0:5 / /data/visible\040share rw shared:4 - nfs4 host:/new rw
12 10 0:2 / /data rw - ext4 /dev/sda1 rw
18 12 0:8 / /data/lower rw - nfs4 host:/lower rw
10 9 8:2 / / rw - ext4 /dev/sdb1 rw
14 10 0:4 / /data/hidden rw - nfs4 host:/hidden rw
13 12 0:3 / /data rw - tmpfs tmpfs rw
16 10 0:6 / /database rw - fuse.rclone shared-name rw
17 10 0:7 / /other rw - fuse.rclone shared-name rw
19 10 0:7 /sub /other-bind rw - fuse.rclone shared-name rw
`
	root := t.TempDir()
	file := filepath.Join(root, "custom-table")
	if err := os.WriteFile(file, []byte(table), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_PROC_MOUNTINFO", file)
	targets, err := readMounts(t.Context(), []string{"sda", "sdb"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uint64{}
	for _, m := range targets {
		got[m.path] = m.mountID
	}
	want := map[string]uint64{"/": 10, "/data/visible share": 15, "/database": 16, "/other": 17}
	if !maps.Equal(got, want) {
		t.Fatalf("visible mount IDs = %v, want %v", got, want)
	}
}

func TestLinuxVolumeListMatchesCapacityNamespace(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"1", "self"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	host := filepath.Join(root, "1/mountinfo")
	if err := os.WriteFile(host, []byte("11 1 0:1 / /host-only rw - nfs4 host:/data rw\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_PROC", root)
	t.Setenv("HOST_PROC_MOUNTINFO", "")
	for _, tc := range []struct{ file, data string }{
		{"mounts", "group:/local /mine nfs4 rw 0 0\n"},
		{"mountinfo", "21 7 0:2 / /mine rw - nfs4 group:/local rw\n"},
	} {
		if err := os.WriteFile(filepath.Join(root, "self", tc.file), []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		mounts, err := mountPartitions(t.Context())
		if err != nil || len(mounts) != 1 || mounts[0].path != "/mine" {
			t.Fatalf("%s: read mounts from a different namespace: %+v, %v", tc.file, mounts, err)
		}
	}
	// A failed inventory must preserve the previous cache until a valid read.
	if err := os.WriteFile(filepath.Join(root, "self/mountinfo"), []byte("broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mountPartitions(t.Context()); err == nil {
		t.Fatal("malformed inventory accepted")
	}
}
