package collector

import (
	"path/filepath"
	"sort"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/disk"
)

// One capacity row per filesystem, with / preferred over duplicate bind mounts.
func usefulPartitions(parts []disk.PartitionStat) []disk.PartitionStat {
	parts = append([]disk.PartitionStat(nil), parts...)
	sort.SliceStable(parts, func(i, j int) bool {
		if (parts[i].Mountpoint == "/") != (parts[j].Mountpoint == "/") {
			return parts[i].Mountpoint == "/"
		}
		return parts[i].Mountpoint < parts[j].Mountpoint
	})
	seen := map[string]bool{}
	var out []disk.PartitionStat
	for _, p := range parts {
		if !platform.UsefulMount(p.Device, p.Mountpoint, p.Fstype) {
			continue
		}
		device := p.Device
		if resolved, err := filepath.EvalSymlinks(device); err == nil {
			device = resolved
		}
		key := device + "\x00" + p.Fstype
		if device == "" {
			key = p.Mountpoint
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func visibleNetworks(networks []Network, info map[string]platform.NetworkInfo) []Network {
	var out []Network
	hasPhysical := false
	for _, n := range networks {
		meta := info[n.Name]
		meta.Name = n.Name
		if !platform.UsefulNetwork(meta) {
			continue
		}
		n.DefaultRoute = meta.DefaultRoute
		out = append(out, n)
		if !platform.TunnelNetwork(n.Name) {
			hasPhysical = true
		}
	}
	for i := range out {
		out[i].ExcludeFromTotals = hasPhysical && platform.TunnelNetwork(out[i].Name)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DefaultRoute != out[j].DefaultRoute {
			return out[i].DefaultRoute
		}
		return out[i].Name < out[j].Name
	})
	return out
}
