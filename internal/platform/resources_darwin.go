//go:build darwin

package platform

import "math"

func ResourceLimits(hostCPUs int, hostMemory uint64) Limits {
	r := Limits{CPUCapacity: float64(hostCPUs), CPUQuota: math.NaN(), Scope: "host", AllowedKnown: true}
	for i := 0; i < hostCPUs; i++ {
		r.AllowedCPUs = append(r.AllowedCPUs, i)
	}
	return r
}
