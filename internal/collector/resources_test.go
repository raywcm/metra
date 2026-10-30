package collector

import (
	"github.com/raywcm/metra/internal/platform"
	"math"
	"testing"
	"time"
)

func TestAllocationIsNotWholeHost(t *testing.T) {
	s := Snapshot{CPU: CPU{Logical: 192, HostLogical: 192, Cores: make([]float64, 192), IDs: make([]int, 192)}, Memory: Memory{Total: 512 << 30, HostTotal: 512 << 30, HostAvailable: 128 << 30, Available: 128 << 30, UsageKnown: true, AvailableKnown: true}}
	for i := range s.CPU.Cores {
		s.CPU.IDs[i] = i
		s.CPU.Cores[i] = float64(i)
	}
	r := platform.Limits{AllowedKnown: true, AllowedCPUs: []int{4, 5, 6}, CPUCapacity: 2.5, CPUQuota: 2.5, CPUUsageKnown: true, MemoryLimit: 4 << 30, MemoryCurrent: 1 << 30, MemoryRoom: 512 << 20, MemoryLimited: true, MemoryUsageKnown: true, MemoryRoomKnown: true, Scope: "allocation"}
	applyAllocation(&s, r)
	if s.CPU.Logical != 3 || s.CPU.Capacity != 2.5 || s.CPU.IDs[0] != 4 || s.Memory.Total != 4<<30 || s.Memory.Used != 1<<30 || s.Memory.Available != 512<<20 || s.Memory.Unavailable != (4<<30)-(1<<30)-(512<<20) {
		t.Fatalf("%+v %+v", s.CPU, s.Memory)
	}
	if s.Memory.Percent != 25 {
		t.Fatal(s.Memory.Percent)
	}
}
func TestSchedulerOnlyMemoryDoesNotFakeUsage(t *testing.T) {
	s := Snapshot{Memory: Memory{Total: 512 << 30, HostTotal: 512 << 30, HostAvailable: 300 << 30, Available: 300 << 30}}
	applyAllocation(&s, platform.Limits{MemoryLimited: true, MemoryLimit: 8 << 30, CPUQuota: math.NaN()})
	if s.Memory.UsageKnown || s.Memory.AvailableKnown || !math.IsNaN(s.Memory.Percent) || s.Memory.Total != 8<<30 {
		t.Fatal(s.Memory)
	}
}
func TestAddressSpaceCapsAllocationHeadroom(t *testing.T) {
	s := Snapshot{Memory: Memory{Total: 16 << 30, Available: 8 << 30, HostTotal: 16 << 30, HostAvailable: 8 << 30, AvailableKnown: true}}
	applyAllocation(&s, platform.Limits{CPUQuota: math.NaN(), AddressKnown: true, AddressRoom: 1 << 30, DataKnown: true, DataRoom: 2 << 30})
	if !s.Memory.AllocatableKnown || s.Memory.Allocatable != 1<<30 || s.Memory.Total != 16<<30 {
		t.Fatal(s.Memory)
	}
}

func TestFractionalCPUQuotaNormalizationAndTransitions(t *testing.T) {
	before := platform.Limits{CPUUsageKnown: true, CPUUsageID: "job", CPUUsageNS: 1e9, CPUCapacity: 2.5}
	after := before
	after.CPUUsageNS += 1e9
	if got := allocationCPUPercent(before, after, time.Second); got != 40 {
		t.Fatal(got)
	}
	if got := allocationCPUPercent(before, after, 2*time.Second); got != 20 {
		t.Fatal(got)
	}
	after.CPUUsageNS += 10e9
	if got := allocationCPUPercent(before, after, time.Second); got != 100 {
		t.Fatal(got)
	}
	for _, kind := range []string{"scope", "capacity", "reset", "missing"} {
		next := before
		next.CPUUsageNS += 1e9
		switch kind {
		case "scope":
			next.CPUUsageID = "other"
		case "capacity":
			next.CPUCapacity = 1
		case "reset":
			next.CPUUsageNS = 0
		case "missing":
			next.CPUUsageKnown = false
		}
		if !math.IsNaN(allocationCPUPercent(before, next, time.Second)) {
			t.Fatal("invalid baseline accepted", kind)
		}
	}
}

func TestScopedMemoryUsesWorkingSetAndReclaimableHeadroom(t *testing.T) {
	s := Snapshot{Memory: Memory{Total: 16 << 30, HostTotal: 16 << 30, HostAvailable: 8 << 30, AvailableKnown: true, UsageKnown: true}}
	limits := platform.Limits{CPUQuota: math.NaN(), MemoryLimited: true, MemoryLimit: 4 << 30, MemoryCurrent: 3 << 30, MemoryReclaimable: 2 << 30, MemoryUsageKnown: true, MemoryRoomKnown: true, MemoryRoom: 3 << 30, MemoryHardRoom: 1 << 30}
	applyAllocation(&s, limits)
	if s.Memory.Used != 1<<30 || s.Memory.Available != 3<<30 || s.Memory.Reclaimable != 2<<30 || s.Memory.Free != 1<<30 || s.Memory.Charged != 3<<30 {
		t.Fatal(s.Memory)
	}
}
