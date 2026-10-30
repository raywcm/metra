package collector

import (
	"testing"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/disk"
)

func TestFilterAndDeduplicateMounts(t *testing.T) {
	parts := []disk.PartitionStat{{Device: "/dev/sda1", Mountpoint: "/mnt/bind", Fstype: "ext4"}, {Device: "/dev/loop0", Mountpoint: "/snap/core/1", Fstype: "squashfs"}, {Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4"}, {Device: "/dev/sdb1", Mountpoint: "/data", Fstype: "xfs", Opts: []string{"ro"}}, {Device: "/dev/sda2", Mountpoint: "/boot/efi", Fstype: "vfat"}}
	got := usefulPartitions(parts)
	if len(got) != 2 || got[0].Mountpoint != "/" || got[1].Mountpoint != "/data" {
		t.Fatal(got)
	}
	if parts[0].Mountpoint != "/mnt/bind" {
		t.Fatal("input mount list mutated")
	}
}

func TestVisibleNetworkOrderAndTunnelAccounting(t *testing.T) {
	networks := []Network{{Name: "wg0"}, {Name: "docker0"}, {Name: "eth1"}, {Name: "eth0"}, {Name: "veth1"}}
	info := map[string]platform.NetworkInfo{}
	for _, n := range networks {
		info[n.Name] = platform.NetworkInfo{Known: true, Up: true, Addresses: []string{"10.0.0.2/24"}, DefaultRoute: n.Name == "eth1"}
	}
	got := visibleNetworks(networks, info)
	if len(got) != 3 || got[0].Name != "eth1" || got[1].Name != "eth0" || !got[2].ExcludeFromTotals {
		t.Fatal(got)
	}
	if only := visibleNetworks(networks[:1], info); len(only) != 1 || only[0].ExcludeFromTotals {
		t.Fatal("tunnel-only environment lost its totals", only)
	}
}
