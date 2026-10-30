//go:build linux

package collector

import "github.com/shirou/gopsutil/v4/process"

func init() {
	// Every process stat query otherwise scans /proc/stat for the same boot
	// timestamp. On many-core hosts this makes a process sweep quadratic in
	// cores × processes and can exhaust the sampling deadline. Configure once
	// at package initialization, before any concurrent process queries.
	process.EnableBootTimeCache(true)
}
