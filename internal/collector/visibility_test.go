package collector

import (
	"errors"
	"math"
	"testing"
	"time"

	netstat "github.com/shirou/gopsutil/v4/net"
)

func TestFilterAndDeduplicateMounts(t *testing.T) {
	parts := []mountTarget{{device: "/dev/sda1", path: "/mnt/bind", fsType: "ext4"}, {device: "/dev/loop0", path: "/snap/core/1", fsType: "squashfs"}, {device: "/dev/sda1", path: "/", fsType: "ext4"}, {device: "/dev/sdb1", path: "/data", fsType: "xfs"}, {device: "/dev/sda2", path: "/boot/efi", fsType: "vfat"}}
	got := usefulPartitions(parts)
	if len(got) != 2 || got[0].path != "/" || got[1].path != "/data" {
		t.Fatal(got)
	}
	if parts[0].path != "/mnt/bind" {
		t.Fatal("input mount list mutated")
	}
}

func TestVisibleNetworkOrderAndTunnelAccounting(t *testing.T) {
	c := &Collector{defaultInterfaces: map[string]bool{"eth1": true}}
	var ifaces []netstat.InterfaceStat
	for _, name := range []string{"wg0", "docker0", "eth1", "eth0", "veth1"} {
		ifaces = append(ifaces, netstat.InterfaceStat{Name: name, Flags: []string{"up"}, Addrs: []netstat.InterfaceAddr{{Addr: "10.0.0.2/24"}}})
	}
	got := c.sampleNetworks(time.Now(), ifaces, nil, nil, nil)
	if len(got) != 3 || got[0].Name != "eth1" || got[1].Name != "eth0" || !got[2].ExcludeFromTotals {
		t.Fatal(got)
	}
	if only := c.sampleNetworks(time.Now(), ifaces[:1], nil, nil, nil); len(only) != 1 || only[0].ExcludeFromTotals {
		t.Fatal("tunnel-only environment lost its totals", only)
	}
}

func TestNetworkInventoryAndPerInterfaceSampling(t *testing.T) {
	now := time.Now()
	c := &Collector{defaultInterfaces: map[string]bool{"eth0": true}}
	ifaces := []netstat.InterfaceStat{{Index: 3, Name: "eth0", Flags: []string{"up"}}, {Index: 4, Name: "wg0", Flags: []string{"up"}, Addrs: []netstat.InterfaceAddr{{Addr: "10.0.0.5/24"}}}}
	raw := []netstat.IOCountersStat{{Name: "eth0", BytesRecv: 100}, {Name: "wg0", BytesRecv: 200}}
	failure := errors.New("network data unavailable")
	sample := func(second int, infos []netstat.InterfaceStat, counts []netstat.IOCountersStat, infoErr, countErr error) []Network {
		return c.sampleNetworks(now.Add(time.Duration(second)*time.Second), infos, counts, infoErr, countErr)
	}
	first := sample(0, ifaces, raw, nil, nil)
	if len(first) != 2 || !first[1].ExcludeFromTotals || !math.IsNaN(first[0].ReceiveRate) {
		t.Fatal("initial inventory", first)
	}
	raw[0].BytesRecv = 130
	partial := sample(1, ifaces, raw[:1], nil, failure)
	if len(partial) != 2 || partial[0].ReceiveRate != 30 || !math.IsNaN(partial[1].ReceiveRate) {
		t.Fatal("partial failure discarded valid counters or an active interface", partial)
	}
	if partial[0].CountersUnavailable || !partial[1].CountersUnavailable || partial[1].MetadataUnavailable {
		t.Fatal("partial failure mislabelled valid interface data", partial)
	}
	unknown := sample(2, nil, nil, failure, failure)
	if len(unknown) != 2 || unknown[0].Name != "eth0" || !math.IsNaN(unknown[0].ReceiveRate) {
		t.Fatal("temporary read failure discarded known interfaces or repeated their rates", unknown)
	}
	if !unknown[0].CountersUnavailable || !unknown[0].MetadataUnavailable {
		t.Fatal("old data was presented as current", unknown)
	}
	raw[0].BytesRecv, raw[1].BytesRecv = 170, 500
	recovered := sample(3, ifaces, raw, nil, nil)
	if recovered[0].ReceiveRate != 20 || recovered[1].ReceiveRate != 100 {
		t.Fatal("rates did not use each interface's last valid timestamp", recovered)
	}
	ifaces[0].Index++
	raw[0].BytesRecv = 100000
	replaced := sample(4, ifaces, raw, nil, nil)
	if !math.IsNaN(replaced[0].ReceiveRate) {
		t.Fatal("recreated interface inherited old traffic counters", replaced)
	}
	if gone := sample(5, nil, nil, nil, nil); len(gone) != 0 {
		t.Fatal("successful empty inventory retained disconnected interfaces", gone)
	}
	if returned := sample(6, ifaces, raw, nil, nil); !math.IsNaN(returned[0].ReceiveRate) {
		t.Fatal("disconnected interface retained its traffic baseline", returned)
	}
	countersOnly := sample(7, nil, raw, failure, nil)
	if len(countersOnly) != 2 || !countersOnly[0].MetadataUnavailable || countersOnly[0].CountersUnavailable || countersOnly[0].ReceiveRate != 0 {
		t.Fatal("metadata failure hid valid traffic or implied a known link state", countersOnly)
	}

}
