//go:build linux

package platform

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"testing"
)

func TestLiveLinuxAllocation(t *testing.T) {
	times, err := cpu.Times(true)
	if err != nil {
		t.Fatal(err)
	}
	vm, err := mem.VirtualMemory()
	if err != nil {
		t.Fatal(err)
	}
	r := ResourceLimits(len(times), vm.Total)
	if len(r.AllowedCPUs) == 0 || r.CPUCapacity <= 0 || r.CPUCapacity > float64(len(r.AllowedCPUs)) {
		t.Fatalf("invalid live CPU allocation: %+v", r)
	}
	if r.MemoryLimited && r.MemoryLimit > vm.Total {
		t.Fatalf("memory exceeds host: %+v", r)
	}
}
