package platform

import (
	"net"
	"path/filepath"
	"strings"
)

func UsefulMount(device, path, filesystem string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	if path == "/" {
		return true
	}
	fs := strings.ToLower(filesystem)
	switch fs {
	case "proc", "procfs", "sysfs", "tmpfs", "devtmpfs", "devfs", "devpts", "cgroup", "cgroup2", "pstore", "securityfs", "debugfs", "tracefs", "configfs", "efivarfs", "hugetlbfs", "mqueue", "fusectl", "autofs", "rpc_pipefs", "binfmt_misc", "nsfs", "squashfs", "overlay", "aufs":
		return false
	}
	for _, prefix := range []string{"/snap", "/var/lib/snapd/snap", "/proc", "/sys", "/dev", "/boot", "/efi"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return false
		}
	}
	if path == "/etc/hosts" || path == "/etc/hostname" || path == "/etc/resolv.conf" {
		return false
	}
	if (path == "/run" || strings.HasPrefix(path, "/run/")) && !strings.HasPrefix(path, "/run/media/") {
		return false
	}
	if strings.HasPrefix(path, "/System/Volumes/") && path != "/System/Volumes/Data" {
		return false
	}
	return true
}

type NetworkInfo struct {
	Name                              string
	Up, Loopback, Known, DefaultRoute bool
	Addresses                         []string
}

func internalNetwork(name string) bool {
	for _, prefix := range []string{"veth", "docker", "br-", "virbr", "vnet", "cni", "flannel", "cali", "cilium", "kube", "lxc", "awdl", "llw", "p2p"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return name == "tunl0" || name == "dummy0" || name == "ifb0"
}

func TunnelNetwork(name string) bool {
	for _, prefix := range []string{"tun", "tap", "utun", "wg", "tailscale", "ipsec", "ppp", "zt", "gif", "stf"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func UsefulNetwork(n NetworkInfo) bool {
	if n.Name == "" || n.Loopback || n.Name == "lo" || n.Name == "lo0" {
		return false
	}
	if n.Known && !n.Up {
		return false
	}
	if n.DefaultRoute {
		return true
	}
	if internalNetwork(n.Name) {
		return false
	}
	if !n.Known {
		return true
	}
	for _, address := range n.Addresses {
		address, _, _ = strings.Cut(address, "/")
		address, _, _ = strings.Cut(address, "%")
		if ip := net.ParseIP(address); ip != nil && ip.IsGlobalUnicast() {
			return true
		}
	}
	return false
}
