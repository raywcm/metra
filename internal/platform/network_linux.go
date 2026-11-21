//go:build linux

package platform

import (
	"context"
	"maps"
	"os"
	"strconv"
	"strings"
)

type routePreference struct {
	metric     uint64
	interfaces map[string]bool
}

func (r *routePreference) consider(name, rawFlags, rawMetric string, base int) {
	flags, err := strconv.ParseUint(rawFlags, 16, 32)
	if err != nil || flags&1 == 0 || flags&0x200 != 0 {
		return
	}
	metric, err := strconv.ParseUint(rawMetric, base, 32)
	if err != nil || (len(r.interfaces) > 0 && metric > r.metric) {
		return
	}
	if metric < r.metric {
		clear(r.interfaces)
	}
	r.metric = metric
	r.interfaces[name] = true
}

func defaultInterfaces(ipv4, ipv6 string) map[string]bool {
	v4 := routePreference{interfaces: make(map[string]bool)}
	v6 := routePreference{interfaces: make(map[string]bool)}
	for _, line := range strings.Split(ipv4, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		v4.consider(f[0], f[3], f[6], 10)
	}
	for _, line := range strings.Split(ipv6, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[0] != strings.Repeat("0", 32) || f[1] != "00" || f[3] != "00" {
			continue
		}
		v6.consider(f[9], f[8], f[5], 16)
	}
	maps.Copy(v4.interfaces, v6.interfaces)
	return v4.interfaces
}

func DefaultInterfaces(_ context.Context) map[string]bool {
	v4, _ := os.ReadFile("/proc/net/route")
	v6, _ := os.ReadFile("/proc/net/ipv6_route")
	return defaultInterfaces(string(v4), string(v6))
}
