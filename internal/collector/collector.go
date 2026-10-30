package collector

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	netstat "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type processCounter struct {
	total   float64
	created int64
	at      time.Time
}

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
	diskAt, netAt     time.Time
	lastGPUs          []platform.GPU
	lastNPUs          []platform.NPU
	gpu               platform.GPUBackend
	cpu               []cpu.TimesStat
	disks             map[string]disk.IOCountersStat
	nets              map[string]netstat.IOCountersStat
	proc              map[int32]processCounter
	metadata          Snapshot
	initialized       bool
	mounts            map[string][]Mount
	mountsAt          time.Time
	processes         []Process
	processesAt       time.Time
}

func New() *Collector {
	return &Collector{gpu: platform.NewGPU(), npu: platform.NewNPU(), proc: make(map[int32]processCounter)}
}

func (c *Collector) Close() {
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
	elapsed := now.Sub(c.diskAt).Seconds()
	counters, err := disk.IOCountersWithContext(ctx)
	warn("disk I/O", err)
	if err == nil {
		names := make([]string, 0, len(counters))
		for name := range counters {
			if platform.PhysicalDisk(name) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		if c.mounts == nil || now.Sub(c.mountsAt) > 15*time.Second {
			c.readMounts(ctx, names)
			c.mountsAt = now
		}
		for _, name := range names {
			v := counters[name]
			d := Disk{CombinedBusy: missing(), ID: name, Name: name, ReadRate: missing(), WriteRate: missing(), Busy: missing(), ReadBusy: missing(), WriteBusy: missing(), ReadBytes: v.ReadBytes, WriteBytes: v.WriteBytes, ReadOps: v.ReadCount, WriteOps: v.WriteCount, Mounts: c.mounts[name]}
			if old, ok := c.disks[name]; ok && elapsed > 0 {
				d.ReadRate = rate(v.ReadBytes, old.ReadBytes, elapsed)
				d.WriteRate = rate(v.WriteBytes, old.WriteBytes, elapsed)
				// Read/write service time can overlap (queued I/O); these are
				// elapsed-time ratios, not percentages of a guessed peak bandwidth.
				if runtime.GOOS == "linux" || v.ReadTime+v.WriteTime > 0 || old.ReadTime+old.WriteTime > 0 {
					d.ReadBusy = math.Min(100, rate(v.ReadTime, old.ReadTime, elapsed)/10)
					d.WriteBusy = math.Min(100, rate(v.WriteTime, old.WriteTime, elapsed)/10)
				}
				if runtime.GOOS == "linux" {
					d.Busy = math.Min(100, rate(v.IoTime, old.IoTime, elapsed)/10)
				}
			}
			d.CombinedBusy = d.Busy
			if math.IsNaN(d.CombinedBusy) && !math.IsNaN(d.ReadBusy) && !math.IsNaN(d.WriteBusy) {
				d.CombinedBusy = math.Min(100, d.ReadBusy+d.WriteBusy)
			}
			s.Disks = append(s.Disks, d)
		}
		c.disks = counters
		c.diskAt = now
	}
	interfaces, e := netstat.InterfacesWithContext(ctx)
	warn("network interfaces", e)
	ifaces := make(map[string]netstat.InterfaceStat)
	if c.routesAt.IsZero() || now.Sub(c.routesAt) >= 15*time.Second {
		c.defaultInterfaces = platform.DefaultInterfaces()
		c.routesAt = now
	}
	visibility := make(map[string]platform.NetworkInfo)
	for _, v := range interfaces {
		ifaces[v.Name] = v
	}
	elapsed = now.Sub(c.netAt).Seconds()
	nc, err := netstat.IOCountersWithContext(ctx, true)
	warn("network", err)
	if err == nil {
		next := make(map[string]netstat.IOCountersStat)
		for _, v := range nc {
			info := ifaces[v.Name]
			loopback := false
			up := false
			for _, f := range info.Flags {
				if f == "loopback" {
					loopback = true
				}
				if f == "up" {
					up = true
				}
			}
			_, known := ifaces[v.Name]
			meta := platform.NetworkInfo{Name: v.Name, Known: known, Up: up, Loopback: loopback, DefaultRoute: c.defaultInterfaces[v.Name]}
			n := Network{Name: v.Name, Up: up, Received: v.BytesRecv, Sent: v.BytesSent, Errors: v.Errin + v.Errout, Drops: v.Dropin + v.Dropout, ReceiveRate: missing(), SendRate: missing()}
			for _, a := range info.Addrs {
				n.Addresses = append(n.Addresses, a.Addr)
			}
			meta.Addresses = n.Addresses
			if !platform.UsefulNetwork(meta) {
				continue
			}
			next[v.Name] = v
			visibility[n.Name] = meta
			if old, ok := c.nets[v.Name]; ok && elapsed > 0 {
				n.ReceiveRate = rate(v.BytesRecv, old.BytesRecv, elapsed)
				n.SendRate = rate(v.BytesSent, old.BytesSent, elapsed)
			}
			s.Networks = append(s.Networks, n)
		}
		s.Networks = visibleNetworks(s.Networks, visibility)
		c.nets = next
		c.netAt = now
	}
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
	} else {
		c.lastGPUs = append([]platform.GPU(nil), s.GPUs...)
	}
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
	} else {
		c.lastNPUs = append([]platform.NPU(nil), s.NPUs...)
	}
	pids, err := process.PidsWithContext(ctx)
	warn("process list", err)
	if err == nil {
		s.ProcessCount = len(pids)
	}
	if withProcesses && (c.processesAt.IsZero() || now.Sub(c.processesAt) >= time.Second) {
		ps, err := c.readProcesses(ctx, pids, now)
		warn("process metrics", err)
		if err == nil {
			c.processes = ps
			c.processesAt = now
		}
	}
	c.applyResources(&s, now)
	frequency := platform.ReadCPUFrequency(ctx, s.CPU.IDs)
	s.CPU.MHz, s.CPU.MinMHz, s.CPU.MaxMHz = frequency.MHz, frequency.MinMHz, frequency.MaxMHz
	s.CPU.FrequencyReported = frequency.Reported
	s.Volumes = c.mounts["unmapped"]
	s.Processes = c.processes
	s.ProcessesAt = c.processesAt
	return s
}
func cpuUsage(a, b cpu.TimesStat) float64 {
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
func (c *Collector) readMounts(ctx context.Context, names []string) {
	c.mounts = make(map[string][]Mount)
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, p := range usefulPartitions(parts) {
		if ctx.Err() != nil {
			return
		}
		if seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		name := ""
		for _, n := range names {
			if platform.MountBelongs(p.Device, n) {
				name = n
				break
			}
		}
		// APFS synthesized volume devices cannot reliably be mapped to physical I/O
		// devices with gopsutil. Report them as unmapped volumes in disk details.
		if name == "" {
			name = "unmapped"
		}
		u, e := disk.UsageWithContext(ctx, p.Mountpoint)
		if e == nil {
			c.mounts[name] = append(c.mounts[name], Mount{p.Mountpoint, p.Fstype, u.Total, u.Used, u.Free, u.UsedPercent})
		}
	}
}
func (c *Collector) readProcesses(ctx context.Context, pids []int32, at time.Time) ([]Process, error) {
	rows := make([]Process, 0, len(pids))
	next := make(map[int32]processCounter, len(pids))
	for _, pid := range pids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, e := process.NewProcessWithContext(ctx, pid)
		if e != nil {
			continue
		}
		name, e := p.NameWithContext(ctx)
		if e != nil {
			name = "N/A"
		}
		created, _ := p.CreateTimeWithContext(ctx)
		ppid, _ := p.PpidWithContext(ctx)
		mi, _ := p.MemoryInfoWithContext(ctx)
		threads, threadsErr := p.NumThreadsWithContext(ctx)
		r := Process{PID: pid, PPID: ppid, Name: name, Created: created, CPU: missing(), Threads: threads, ThreadsKnown: threadsErr == nil && threads > 0}
		if mi != nil && (runtime.GOOS != "darwin" || threads > 0) {
			r.MemoryKnown = true
			r.RSS = mi.RSS
			r.VMS = mi.VMS
		}
		t, e := p.TimesWithContext(ctx)
		if e == nil && (runtime.GOOS != "darwin" || threads > 0) {
			total := t.User + t.System
			if prev, ok := c.proc[pid]; ok && created > 0 && created == prev.created && at.After(prev.at) && total >= prev.total {
				r.CPU = (total - prev.total) / at.Sub(prev.at).Seconds() * 100
			}
			next[pid] = processCounter{total, created, at}
		}
		rows = append(rows, r)
	}
	c.proc = next
	return rows, nil
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
