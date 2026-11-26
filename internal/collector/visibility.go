package collector

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/raywcm/metra/internal/platform"
)

// One capacity row per filesystem, with / preferred over duplicate bind mounts.
func usefulPartitions(parts []mountTarget) []mountTarget {
	parts = append([]mountTarget(nil), parts...)
	sort.SliceStable(parts, func(i, j int) bool {
		if (parts[i].path == "/") != (parts[j].path == "/") {
			return parts[i].path == "/"
		}
		return parts[i].path < parts[j].path
	})
	seen := map[string]bool{}
	var out []mountTarget
	for _, p := range parts {
		if !platform.UsefulMount(p.device, p.path, p.fsType) {
			continue
		}
		device := p.fsDevice
		if device == "" {
			device = p.device
			if strings.HasPrefix(device, "/dev/") {
				if resolved, err := filepath.EvalSymlinks(device); err == nil {
					device = resolved
				}
			}
		}
		key := device + "\x00" + p.fsType
		if device == "" {
			key = p.path
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func orderNetworks(networks []Network) {
	hasPhysical := false
	for _, n := range networks {
		if !platform.TunnelNetwork(n.Name) {
			hasPhysical = true
			break
		}
	}
	for i := range networks {
		networks[i].ExcludeFromTotals = hasPhysical && platform.TunnelNetwork(networks[i].Name)
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].DefaultRoute != networks[j].DefaultRoute {
			return networks[i].DefaultRoute
		}
		return networks[i].Name < networks[j].Name
	})
}
