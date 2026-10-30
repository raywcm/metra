package collector

import (
	"github.com/raywcm/metra/internal/platform"
	"math"
	"time"
)

func (c *Collector) applyResources(s *Snapshot, now time.Time) {
	hostCPUs := s.CPU.HostLogical
	if hostCPUs == 0 {
		hostCPUs = s.CPU.Logical
	}
	hostMemory := s.Memory.HostTotal
	if hostMemory == 0 {
		hostMemory = s.Memory.Total
	}
	limits := platform.ResourceLimits(hostCPUs, hostMemory)
	now = time.Now()
	applyAllocation(s, limits)
	if limits.Partial {
		s.Warnings = append(s.Warnings, "resource limits: some files are not readable")
	}
	if s.CPU.Scoped {
		s.CPU.Percent = allocationCPUPercent(c.previousLimits, limits, now.Sub(c.limitsAt))
	}
	c.previousLimits = limits
	if !s.CPU.Scoped {
		c.previousLimits.CPUUsageKnown = false
	}
	c.limitsAt = now
	// Apple GPU memory is a shared system pool, not dedicated VRAM. Its physical
	// capacity is distinct from the current cgroup/job's CPU memory allocation.
	for i := range s.GPUs {
		if s.GPUs[i].UnifiedMemory && hostMemory > 0 {
			s.GPUs[i].MemoryTotal = float64(hostMemory)
		}
	}
}
func applyAllocation(s *Snapshot, limits platform.Limits) {
	s.Limits = limits
	s.CPU.Capacity = limits.CPUCapacity
	s.CPU.HostLogical = max(s.CPU.HostLogical, len(s.CPU.Cores))
	s.CPU.Scoped = limits.CPUUsageKnown && ((!math.IsNaN(limits.CPUQuota) && limits.CPUQuota <= float64(len(limits.AllowedCPUs))) || limits.SchedulerCPU)
	allowed := map[int]bool{}
	for _, id := range limits.AllowedCPUs {
		allowed[id] = true
	}
	var values []float64
	var ids []int
	sum := 0.0
	count := 0
	for i, v := range s.CPU.Cores {
		id := i
		if i < len(s.CPU.IDs) {
			id = s.CPU.IDs[i]
		}
		if limits.AllowedKnown && !allowed[id] {
			continue
		}
		ids = append(ids, id)
		values = append(values, v)
		if !math.IsNaN(v) {
			sum += v
			count++
		}
	}
	s.CPU.Cores = values
	s.CPU.IDs = ids
	s.CPU.Logical = len(values)
	if count > 0 {
		s.CPU.Percent = sum / float64(count)
	}
	if !limits.CPUUsageKnown && (!math.IsNaN(limits.CPUQuota) || limits.SchedulerCPU) {
		s.CPU.Percent = missing()
	}
	if s.CPU.Capacity <= 0 && !limits.AllowedKnown {
		s.CPU.Capacity = float64(len(values))
	}
	m := &s.Memory
	if m.HostTotal == 0 {
		m.HostTotal = m.Total
		m.HostAvailable = m.Available
	}
	if limits.MemoryLimited {
		m.Limited = true
		m.Total = limits.MemoryLimit
		m.UsageKnown = limits.MemoryUsageKnown
		m.AvailableKnown = limits.MemoryUsageKnown
		m.ModifiedKnown = limits.MemoryDirtyKnown && limits.MemoryUsageKnown
		m.CachedKnown = limits.MemoryStatsKnown && limits.MemoryUsageKnown
		m.Cached = limits.MemoryCached
		m.Percent = math.NaN()
		m.Used = 0
		m.Available = 0
		m.Reclaimable = 0
		m.Modified = 0
		m.Free = 0
		m.SwapKnown = false
		if limits.MemoryUsageKnown {
			m.Charged = limits.MemoryCurrent
			m.Used = limits.MemoryCurrent - min(limits.MemoryCurrent, limits.MemoryReclaimable)
			m.Available = m.Total - min(m.Used, m.Total)
			if limits.MemoryRoomKnown {
				m.Available = min(m.Available, limits.MemoryRoom)
			}
			m.Available = min(m.Available, m.HostAvailable)
			m.Free = m.Available
			if limits.MemoryRoomKnown {
				m.Free = min(m.Free, limits.MemoryHardRoom)
			}
			m.Reclaimable = m.Available - m.Free
			m.Unavailable = m.Total - min(m.Used, m.Total) - m.Available
			m.Modified = min(limits.MemoryDirty, m.Used)
			if m.Total > 0 {
				m.Percent = float64(m.Used) / float64(m.Total) * 100
			}
		}
	}
	if m.AvailableKnown {
		m.Allocatable = m.Available
		m.AllocatableKnown = true
		if limits.AddressKnown {
			m.Allocatable = min(m.Allocatable, limits.AddressRoom)
		}
		if limits.DataKnown {
			m.Allocatable = min(m.Allocatable, limits.DataRoom)
		}
	}
}

func allocationCPUPercent(before, after platform.Limits, elapsed time.Duration) float64 {
	if !before.CPUUsageKnown || !after.CPUUsageKnown || elapsed <= 0 || after.CPUCapacity <= 0 || after.CPUCapacity != before.CPUCapacity || before.CPUUsageID != after.CPUUsageID || after.CPUUsageNS < before.CPUUsageNS {
		return math.NaN()
	}
	return math.Min(100, float64(after.CPUUsageNS-before.CPUUsageNS)/float64(elapsed)/after.CPUCapacity*100)
}
