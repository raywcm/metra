package collector

import (
	"context"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	netstat "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Collector is owned by one sampling command at a time. It uses counter deltas,
// never blocking cpu.Percent intervals or a goroutine per process.
type Collector struct {
	routesAt          time.Time
	defaultInterfaces map[string]bool
	temperatureAt     time.Time
	temperatures      []platform.Temperature
	limitsAt          time.Time
	previousLimits    platform.Limits
	npu               platform.NPUBackend
	lastGPUs          []platform.GPU
	lastNPUs          []platform.NPU
	gpu               platform.GPUBackend
	cpu               []cpu.TimesStat
	disks             map[string]diskBaseline
	nets              map[string]networkCounter
	proc              map[int32]processCounter
	sweep             *processSweep
	metadata          Snapshot
	initialized       bool
	volumes           volumeCache
	volumeCtx         context.Context
	stopVolumes       context.CancelFunc
	processes         []Process
	processesAt       time.Time
	processListTotal  int
}

func New() *Collector {
	return newCollector(platform.NewGPU(), platform.NewNPU())
}

func newCollector(gpu platform.GPUBackend, npu platform.NPUBackend) *Collector {
	ctx, cancel := context.WithCancel(context.Background())
	return &Collector{gpu: gpu, npu: npu, volumeCtx: ctx, stopVolumes: cancel}
}

func (c *Collector) Close() {
	c.stopVolumes()
	if closer, ok := c.gpu.(interface{ Close() }); ok {
		closer.Close()
	}
}
func missing() float64 { return math.NaN() }
func (c *Collector) Sample(ctx context.Context, withProcesses bool) Snapshot {
	now := time.Now()
	s := c.metadata
	s.At = now
	s.Warnings = nil
	s.CPU.Percent = missing()
	s.CPU.Cores = nil
	s.CPU.Load = [3]float64{missing(), missing(), missing()}
	s.Memory.Percent = missing()
	s.Disks = nil
	s.Networks = nil
	s.GPUs = nil
	s.NPUs = nil
	warn := func(label string, err error) {
		if err != nil {
			s.Warnings = append(s.Warnings, label+": "+err.Error())
		}
	}
	if !c.initialized {
		info, err := host.InfoWithContext(ctx)
		warn("host", err)
		if err == nil {
			s.Host = info.Hostname
			s.OS = info.Platform + " " + info.PlatformVersion
			s.Kernel = info.KernelVersion
		}
		s.Arch = runtime.GOARCH
		s.CPU.Logical = runtime.NumCPU()
		s.CPU.Physical, _ = cpu.CountsWithContext(ctx, false)
		infos, err := cpu.InfoWithContext(ctx)
		warn("CPU info", err)
		if len(infos) > 0 {
			s.CPU.Name = infos[0].ModelName
			s.CPU.MHz = infos[0].Mhz
		}
		c.metadata = s
		c.initialized = true
	}
	s.Uptime, _ = host.UptimeWithContext(ctx)
	times, err := cpu.TimesWithContext(ctx, true)
	warn("CPU", err)
	if err == nil {
		s.CPU.Cores = make([]float64, len(times))
		s.CPU.IDs = make([]int, len(times))
		s.CPU.HostLogical = len(times)
		sum, count := 0.0, 0
		previous := map[string]cpu.TimesStat{}
		for _, old := range c.cpu {
			previous[old.CPU] = old
		}
		for i, t := range times {
			v := missing()
			if old, ok := previous[t.CPU]; ok {
				v = cpuUsage(old, t)
			}
			s.CPU.Cores[i] = v
			id, e := strconv.Atoi(strings.TrimPrefix(t.CPU, "cpu"))
			if e != nil {
				id = i
			}
			s.CPU.IDs[i] = id
			if !math.IsNaN(v) {
				sum += v
				count++
			}
		}
		if count > 0 {
			s.CPU.Percent = sum / float64(count)
		}
		c.cpu = times
	}
	if l, e := load.AvgWithContext(ctx); e == nil {
		s.CPU.Load = [3]float64{l.Load1, l.Load5, l.Load15}
	}
	if c.temperatureAt.IsZero() || now.Sub(c.temperatureAt) >= 2*time.Second {
		c.temperatures = platform.CPUTemperatures(ctx)
		c.temperatureAt = now
	}
	s.CPU.Temperatures = append([]platform.Temperature(nil), c.temperatures...)
	s.CPU.Temperature, s.CPU.TemperatureKnown = missing(), false
	for _, sensor := range s.CPU.Temperatures {
		if !s.CPU.TemperatureKnown || sensor.Celsius > s.CPU.Temperature {
			s.CPU.Temperature, s.CPU.TemperatureKnown = sensor.Celsius, true
		}
	}
	vm, err := mem.VirtualMemoryWithContext(ctx)
	warn("memory", err)
	if err == nil {
		s.Memory = Memory{Total: vm.Total, Used: vm.Used, Available: vm.Available, Cached: vm.Cached, Percent: vm.UsedPercent, UsageKnown: true, AvailableKnown: true, CachedKnown: runtime.GOOS == "linux"}
	}
	if vm != nil && err == nil && vm.Total > 0 {
		available := min(vm.Available, vm.Total)
		s.Memory.Used = vm.Total - available
		s.Memory.Percent = float64(s.Memory.Used) / float64(vm.Total) * 100
		s.Memory.Modified = min(vm.Dirty, s.Memory.Used)
		s.Memory.ModifiedKnown = runtime.GOOS == "linux"
		s.Memory.HostTotal = vm.Total
		s.Memory.HostAvailable = vm.Available
		s.Memory.Free = min(vm.Free, available)
		s.Memory.Reclaimable = available - s.Memory.Free
	}
	if swap, e := mem.SwapMemoryWithContext(ctx); e == nil {
		s.Memory.SwapKnown = true
		s.Memory.SwapTotal = swap.Total
		s.Memory.SwapUsed = swap.Used
	}
	inventory, diskErr := platform.ReadDisks(ctx, slices.Collect(maps.Keys(c.disks)))
	warn("disk I/O", diskErr)
	s.DiskInventoryUnavailable = inventory.Incomplete
	s.Disks = c.sampleDisks(time.Now(), inventory)
	diskIDs := make(map[string]string, len(s.Disks))
	for _, d := range s.Disks {
		diskIDs[d.Name] = d.ID
	}
	mounts, mountErr := c.volumes.sample(c.volumeCtx, diskIDs, readMounts, mountUsage)
	warn("mounts", mountErr)
	for i, d := range s.Disks {
		s.Disks[i].Mounts = mounts[d.Name]
		delete(mounts, d.Name)
	}
	interfaces, interfaceErr := netstat.InterfacesWithContext(ctx)
	warn("network interfaces", interfaceErr)
	if c.routesAt.IsZero() || now.Sub(c.routesAt) >= 15*time.Second {
		c.defaultInterfaces = platform.DefaultInterfaces(ctx)
		c.routesAt = now
	}
	networkCounters, networkErr := netstat.IOCountersWithContext(ctx, true)
	warn("network", networkErr)
	s.Networks = c.sampleNetworks(time.Now(), interfaces, networkCounters, interfaceErr, networkErr)
	s.NetworkInventoryUnavailable = interfaceErr != nil && networkErr != nil
	// Backends bound external commands themselves. Native reads across many
	// devices share the overall sampling deadline, rather than losing tail GPUs.
	s.GPUs, err = c.gpu.Sample(ctx)
	warn("GPU", err)
	for i := range s.GPUs {
		for _, old := range c.lastGPUs {
			if old.ID == s.GPUs[i].ID {
				s.GPUs[i].Engines = platform.KeepKnownEngines(s.GPUs[i].Engines, old.Engines, s.GPUs[i].AbsentEngines...)
				break
			}
		}
		s.GPUs[i].Utilization = platform.PeakUtilization(s.GPUs[i].Utilization, s.GPUs[i].Engines)
	}
	if err != nil {
		seen := map[string]bool{}
		for _, g := range s.GPUs {
			seen[g.ID] = true
		}
		for _, g := range c.lastGPUs {
			if !seen[g.ID] {
				s.GPUs = append(s.GPUs, platform.UnavailableGPU(g))
			}
		}
	}
	c.lastGPUs = append([]platform.GPU(nil), s.GPUs...)
	s.NPUs, err = c.npu.Sample(ctx)
	warn("NPU", err)
	for i := range s.NPUs {
		for _, old := range c.lastNPUs {
			if old.ID == s.NPUs[i].ID {
				s.NPUs[i].Engines = platform.KeepKnownEngines(s.NPUs[i].Engines, old.Engines)
				break
			}
		}
		s.NPUs[i].Utilization = platform.PeakUtilization(s.NPUs[i].Utilization, s.NPUs[i].Engines)
	}
	if err != nil {
		seen := map[string]bool{}
		for _, n := range s.NPUs {
			seen[n.ID] = true
		}
		for _, n := range c.lastNPUs {
			if !seen[n.ID] {
				s.NPUs = append(s.NPUs, platform.UnavailableNPU(n))
			}
		}
	}
	c.lastNPUs = append([]platform.NPU(nil), s.NPUs...)
	c.applyResources(&s, now)
	frequency := platform.ReadCPUFrequency(ctx, s.CPU.IDs)
	s.CPU.MHz, s.CPU.MinMHz, s.CPU.MaxMHz = frequency.MHz, frequency.MinMHz, frequency.MaxMHz
	s.CPU.FrequencyReported = frequency.Reported
	pids, err := process.PidsWithContext(ctx)
	warn("process list", err)
	s.ProcessCountUnavailable = err != nil
	s.ProcessCount = len(pids)
	if !withProcesses {
		c.sweep = nil
	} else if err == nil && (c.processesAt.IsZero() || now.Sub(c.processesAt) >= time.Second) {
		ps, err := c.readProcesses(ctx, pids, readProcess)
		warn("process metrics", err)
		if err == nil {
			c.processes = ps
			c.processesAt = now
			c.processListTotal = len(pids)
		}
	}
	// Keep capacity visible even when only some devices have I/O counters.
	for _, volumes := range mounts {
		s.Volumes = append(s.Volumes, volumes...)
	}
	sort.Slice(s.Volumes, func(i, j int) bool { return s.Volumes[i].Path < s.Volumes[j].Path })
	s.Processes = c.processes
	s.ProcessesAt = c.processesAt
	s.ProcessListTotal = c.processListTotal
	return s
}
func cpuUsage(a, b cpu.TimesStat) float64 {
	if b.User < a.User || b.System < a.System || b.Nice < a.Nice || b.Idle < a.Idle ||
		b.Iowait < a.Iowait || b.Irq < a.Irq || b.Softirq < a.Softirq || b.Steal < a.Steal {
		return missing()
	}
	total := func(t cpu.TimesStat) float64 {
		return t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
	}
	dt := total(b) - total(a)
	idle := (b.Idle + b.Iowait) - (a.Idle + a.Iowait)
	if dt <= 0 || idle < 0 {
		return missing()
	}
	return math.Max(0, math.Min(100, 100*(dt-idle)/dt))
}
func rate(current, previous uint64, seconds float64) float64 {
	if seconds <= 0 || current < previous {
		return missing()
	}
	return float64(current-previous) / seconds
}
func (c *Collector) Detail(ctx context.Context, row Process) (Detail, error) {
	d := Detail{Process: row}
	p, err := process.NewProcessWithContext(ctx, row.PID)
	if err != nil {
		return d, err
	}
	created, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return d, err
	}
	if row.Created <= 0 || created != row.Created {
		return d, fmt.Errorf("process identity changed or unavailable")
	}
	var issues []string
	get := func(label string, v string, e error) string {
		if e != nil {
			issues = append(issues, label+": "+e.Error())
			return "N/A"
		}
		return v
	}
	v, e := p.UsernameWithContext(ctx)
	d.User = get("user", v, e)
	v, e = p.CmdlineWithContext(ctx)
	d.Command = get("command", v, e)
	v, e = p.ExeWithContext(ctx)
	d.Executable = get("executable", filepath.Clean(v), e)
	status, e := p.StatusWithContext(ctx)
	d.Status = get("status", strings.Join(status, ", "), e)
	d.Err = strings.Join(issues, "; ")
	return d, nil
}
func (c *Collector) Terminate(ctx context.Context, p Process) error {
	return platform.Terminate(ctx, p.PID, p.Created)
}
