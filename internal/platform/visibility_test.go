package platform

import "testing"

func TestUsefulMounts(t *testing.T) {
	for _, tc := range []struct {
		path, fs string
		want     bool
	}{
		{"/snap/firefox/7633", "squashfs", false}, {"/boot/efi", "vfat", false}, {"/proc", "proc", false}, {"/run/user/1000", "tmpfs", false}, {"/var/lib/docker/overlay2/a/merged", "overlay", false}, {"/etc/hosts", "ext4", false},
		{"/", "ext4", true}, {"/", "overlay", true}, {"/data0", "xfs", true}, {"/home", "btrfs", true}, {"/mnt/archive", "nfs4", true}, {"/run/media/user/backup", "exfat", true}, {"/System/Volumes/Data", "apfs", true}, {"/System/Volumes/Preboot", "apfs", false},
	} {
		if UsefulMount("device", tc.path, tc.fs) != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestUsefulNetworkInterfaces(t *testing.T) {
	for _, tc := range []struct {
		name, address   string
		up, route, want bool
	}{
		{"lo", "127.0.0.1/8", true, false, false}, {"docker0", "172.17.0.1/16", true, false, false}, {"veth123", "10.0.0.2/24", true, false, false}, {"br-123abc", "172.18.0.1/16", true, false, false}, {"cali123", "10.1.0.1/24", true, false, false},
		{"eth0", "192.0.2.10/24", true, true, true}, {"eth1", "192.168.1.2/24", true, false, true}, {"eth2", "192.168.1.3/24", false, false, false}, {"bond0", "10.1.1.2/24", true, false, true}, {"enp5s0.100", "10.2.2.2/24", true, false, true},
		{"utun7", "fe80::1%utun7/64", true, false, false}, {"wg0", "10.8.0.2/24", true, false, true}, {"tailscale0", "100.100.0.1/32", true, false, true}, {"awdl0", "fe80::a/64", true, false, false}, {"veth0", "10.0.0.1/24", true, true, true}, {"eth0", "", true, true, true}, {"en0", "", true, false, false},
	} {
		n := NetworkInfo{Name: tc.name, Addresses: []string{tc.address}, Known: true, Up: tc.up, DefaultRoute: tc.route}
		if UsefulNetwork(n) != tc.want {
			t.Fatal(tc)
		}
	}
	if !UsefulNetwork(NetworkInfo{Name: "eth0"}) {
		t.Fatal("metadata failure discarded otherwise readable counters")
	}
}
