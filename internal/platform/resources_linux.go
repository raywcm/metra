//go:build linux

package platform

import (
	"golang.org/x/sys/unix"
	"os"
	"runtime"
	"strings"
)

func ResourceLimits(hostCPUs int, hostMemory uint64) Limits {
	// Linux affinity is a thread property; inspect it on one stable OS thread.
	runtime.LockOSThread()
	var mask unix.CPUSet
	err := unix.SchedGetaffinity(0, &mask)
	runtime.UnlockOSThread()
	var ids []int
	if err == nil {
		for i := 0; i < len(mask)*64; i++ {
			if mask.IsSet(i) {
				ids = append(ids, i)
			}
		}
	}
	if len(ids) == 0 {
		for _, line := range strings.Split(resourceFS{root: "/"}.read("/proc/self/status"), "\n") {
			if strings.HasPrefix(line, "Cpus_allowed_list:") {
				ids, _ = parseCPUList(strings.TrimPrefix(line, "Cpus_allowed_list:"))
				break
			}
		}
	}
	limits := detectLimits("/", ids, os.Getenv, hostCPUs, hostMemory)
	status := countersFromStatus(resourceFS{root: "/"}.read("/proc/self/status"))
	var as, data unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_AS, &as) == nil && as.Cur != unix.RLIM_INFINITY && as.Cur > 0 {
		limits.AddressKnown = true
		limits.AddressLimit = as.Cur
		if as.Cur > status["VmSize"] {
			limits.AddressRoom = as.Cur - status["VmSize"]
		}
	}
	if unix.Getrlimit(unix.RLIMIT_DATA, &data) == nil && data.Cur != unix.RLIM_INFINITY && data.Cur > 0 {
		limits.DataKnown = true
		limits.DataLimit = data.Cur
		if data.Cur > status["VmData"] {
			limits.DataRoom = data.Cur - status["VmData"]
		}
	}
	return limits
}
