//go:build linux

package platform

import (
	"os"
	"strconv"
	"strings"
)

func defaultInterfaces(ipv4, ipv6 string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(ipv4, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err == nil && flags&1 != 0 && flags&0x200 == 0 {
			out[f[0]] = true
		}
	}
	for _, line := range strings.Split(ipv6, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[0] != strings.Repeat("0", 32) || f[1] != "00" {
			continue
		}
		flags, err := strconv.ParseUint(f[8], 16, 32)
		if err == nil && flags&1 != 0 && flags&0x200 == 0 {
			out[f[9]] = true
		}
	}
	return out
}

func DefaultInterfaces() map[string]bool {
	v4, _ := os.ReadFile("/proc/net/route")
	v6, _ := os.ReadFile("/proc/net/ipv6_route")
	return defaultInterfaces(string(v4), string(v6))
}
