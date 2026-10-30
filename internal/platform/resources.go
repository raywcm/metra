package platform

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Limits describes the allocation visible to this process, never a reservation
// inferred from its UID or from GOMAXPROCS. CPUCapacity can be fractional.
type Limits struct {
	MemoryReclaimable, MemoryHardRoom                                  uint64
	Partial                                                            bool
	AllowedKnown                                                       bool
	CPUUsageID                                                         string
	MemoryDirtyKnown                                                   bool
	MemoryLimitKnown                                                   bool
	AllowedCPUs                                                        []int
	CPUCapacity, CPUQuota                                              float64
	CPURestricted, CPUUsageKnown                                       bool
	CPUUsageNS                                                         uint64
	MemoryLimit, MemoryCurrent, MemoryRoom, MemoryCached, MemoryDirty  uint64
	MemoryLimited, MemoryUsageKnown, MemoryRoomKnown, MemoryStatsKnown bool
	SchedulerCPU, SchedulerMemory                                      bool
	AddressLimit, AddressRoom, DataLimit, DataRoom                     uint64
	AddressKnown, DataKnown                                            bool
	Scope                                                              string
}
type resourceMount struct {
	root, point, kind string
	controllers       map[string]bool
}
type resourceFS struct {
	root   string
	denied *bool
}

func (f resourceFS) read(path string) string {
	data, e := os.ReadFile(filepath.Join(f.root, strings.TrimPrefix(filepath.Clean(path), "/")))
	if e != nil {
		if os.IsPermission(e) && f.denied != nil {
			*f.denied = true
		}
		return ""
	}
	return strings.TrimSpace(string(data))
}
func parseCPUList(text string) ([]int, error) {
	set := map[int]bool{}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	for _, part := range strings.Split(strings.TrimSpace(text), ",") {
		bounds := strings.Split(part, "-")
		first, e := strconv.Atoi(bounds[0])
		if e != nil || first < 0 || len(bounds) > 2 {
			return nil, strconv.ErrSyntax
		}
		last := first
		if len(bounds) == 2 {
			last, e = strconv.Atoi(bounds[1])
			if e != nil || last < first {
				return nil, strconv.ErrSyntax
			}
		}
		if last-first > 1<<20 {
			return nil, strconv.ErrRange
		}
		for i := first; i <= last; i++ {
			set[i] = true
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}
func intersection(a, b []int) []int {
	set := map[int]bool{}
	for _, n := range b {
		set[n] = true
	}
	out := make([]int, 0, len(a))
	for _, n := range a {
		if set[n] {
			out = append(out, n)
		}
	}
	return out
}
func parseMounts(text string) []resourceMount {
	var out []resourceMount
	decode := func(s string) string {
		r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`)
		return r.Replace(s)
	}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left, right := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(left) < 5 || len(right) < 3 || (right[0] != "cgroup" && right[0] != "cgroup2") {
			continue
		}
		m := resourceMount{root: decode(left[3]), point: decode(left[4]), kind: right[0], controllers: map[string]bool{}}
		for _, c := range strings.Split(right[2], ",") {
			m.controllers[c] = true
		}
		out = append(out, m)
	}
	return out
}
func membership(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[1] == "" {
			out["v2"] = parts[2]
		}
		for _, controller := range strings.Split(parts[1], ",") {
			out[controller] = parts[2]
		}
	}
	return out
}
func groupPath(m resourceMount, group string) (string, bool) {
	root := filepath.Clean(m.root)
	group = filepath.Clean(group)
	rel := ""
	switch {
	case group == "/":
	case root == "/":
		rel = strings.TrimPrefix(group, "/")
	case group == root:
	case strings.HasPrefix(group, root+"/"):
		rel = strings.TrimPrefix(group, root+"/")
	default:
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return filepath.Join(m.point, rel), true
}
func ancestors(path, stop string) []string {
	path, stop = filepath.Clean(path), filepath.Clean(stop)
	var out []string
	for path == stop || strings.HasPrefix(path, stop+"/") {
		out = append(out, path)
		if path == stop {
			break
		}
		path = filepath.Dir(path)
	}
	return out
}
func uintValue(s string) (uint64, bool) {
	v, e := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v, e == nil
}
func counters(s string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			if v, ok := uintValue(f[1]); ok {
				out[f[0]] = v
			}
		}
	}
	return out
}
func cpuQuota(s string) float64 {
	f := strings.Fields(s)
	if len(f) != 2 || f[0] == "max" {
		return math.NaN()
	}
	q, e := strconv.ParseFloat(f[0], 64)
	p, e2 := strconv.ParseFloat(f[1], 64)
	if e != nil || e2 != nil || q <= 0 || p <= 0 {
		return math.NaN()
	}
	return q / p
}
func detectLimits(root string, affinity []int, env func(string) string, hostCPUs int, hostMemory uint64) Limits {
	denied := false
	f := resourceFS{root: root, denied: &denied}
	r := Limits{CPUQuota: math.NaN(), Scope: "host", AllowedKnown: true}
	if len(affinity) == 0 {
		for i := 0; i < hostCPUs; i++ {
			affinity = append(affinity, i)
		}
	}
	r.AllowedCPUs = append([]int(nil), affinity...)
	groups := membership(f.read("/proc/self/cgroup"))
	mounts := parseMounts(f.read("/proc/self/mountinfo"))
	memoryPath := ""
	memoryV2 := false
	cpuPath := ""

	applyCPU := func(v float64) {
		if !math.IsNaN(v) && (math.IsNaN(r.CPUQuota) || v < r.CPUQuota) {
			r.CPUQuota = v
		}
	}
	applyMemory := func(limit uint64, current uint64, currentKnown bool, reclaimable uint64) {
		if limit > hostMemory {
			return
		}

		if !r.MemoryLimitKnown || limit < r.MemoryLimit {
			r.MemoryLimit = limit
			r.MemoryLimitKnown = true
		}
		if currentKnown {
			hard := uint64(0)
			if limit > current {
				hard = limit - current
			}
			working := current - min(current, reclaimable)
			room := uint64(0)
			if limit > working {
				room = limit - working
			}
			if !r.MemoryRoomKnown || hard < r.MemoryHardRoom {
				r.MemoryHardRoom = hard
			}
			if !r.MemoryRoomKnown || room < r.MemoryRoom {
				r.MemoryRoom = room
				r.MemoryRoomKnown = true
			}
		}
	}
	for _, m := range mounts {
		if m.kind == "cgroup2" {
			group, ok := groups["v2"]
			if !ok {
				continue
			}
			path, ok := groupPath(m, group)
			if !ok {
				continue
			}
			cpuPath = path
			memoryPath = path
			memoryV2 = true
			for _, dir := range ancestors(path, m.point) {
				applyCPU(cpuQuota(f.read(filepath.Join(dir, "cpu.max"))))
				if ids, e := parseCPUList(f.read(filepath.Join(dir, "cpuset.cpus.effective"))); e == nil && len(ids) > 0 {
					r.AllowedCPUs = intersection(r.AllowedCPUs, ids)
				}
				limit, valid := uintValue(f.read(filepath.Join(dir, "memory.max")))
				current, known := uintValue(f.read(filepath.Join(dir, "memory.current")))
				if valid {
					applyMemory(limit, current, known, reclaimableAt(f, dir, m.kind == "cgroup2"))
				}
			}
			stat := counters(f.read(filepath.Join(path, "cpu.stat")))
			if usage, ok := stat["usage_usec"]; ok && usage <= math.MaxUint64/1000 {
				r.CPUUsageNS = usage * 1000
				r.CPUUsageKnown = true
			}
		} else {
			for _, controller := range []string{"cpu", "cpuacct", "cpuset", "memory"} {
				if !m.controllers[controller] {
					continue
				}
				group, ok := groups[controller]
				if !ok {
					continue
				}
				path, ok := groupPath(m, group)
				if !ok {
					continue
				}
				switch controller {
				case "cpu":
					cpuPath = path
					for _, dir := range ancestors(path, m.point) {
						q := f.read(filepath.Join(dir, "cpu.cfs_quota_us"))
						p := f.read(filepath.Join(dir, "cpu.cfs_period_us"))
						applyCPU(cpuQuota(q + " " + p))
					}
				case "cpuacct":
					if !m.controllers["cpu"] || groups["cpu"] != group {
						continue
					}
					if usage, ok := uintValue(f.read(filepath.Join(path, "cpuacct.usage"))); ok {
						r.CPUUsageKnown = true
						r.CPUUsageNS = usage
					}
				case "cpuset":
					for _, dir := range ancestors(path, m.point) {
						if ids, e := parseCPUList(f.read(filepath.Join(dir, "cpuset.cpus"))); e == nil && len(ids) > 0 {
							r.AllowedCPUs = intersection(r.AllowedCPUs, ids)
						}
					}
				case "memory":
					memoryPath = path
					for _, dir := range ancestors(path, m.point) {
						if dir != path && f.read(filepath.Join(dir, "memory.use_hierarchy")) == "0" {
							continue
						}
						limit, valid := uintValue(f.read(filepath.Join(dir, "memory.limit_in_bytes")))
						current, known := uintValue(f.read(filepath.Join(dir, "memory.usage_in_bytes")))
						if valid {
							applyMemory(limit, current, known, reclaimableAt(f, dir, m.kind == "cgroup2"))
						}
					}
				}
			}
		}
	}
	if memoryPath != "" {
		file := "memory.usage_in_bytes"
		if memoryV2 {
			file = "memory.current"
		}
		r.MemoryCurrent, r.MemoryUsageKnown = uintValue(f.read(filepath.Join(memoryPath, file)))
		stat := counters(f.read(filepath.Join(memoryPath, "memory.stat")))
		if memoryV2 {
			r.MemoryCached, r.MemoryStatsKnown = stat["file"]
			r.MemoryReclaimable = stat["inactive_file"]
			r.MemoryDirty, r.MemoryDirtyKnown = stat["file_dirty"]
		} else {
			r.MemoryCached, r.MemoryStatsKnown = stat["cache"]
			r.MemoryReclaimable = stat["total_inactive_file"]
			if _, ok := stat["total_inactive_file"]; !ok {
				r.MemoryReclaimable = stat["inactive_file"]
			}
			r.MemoryDirty, r.MemoryDirtyKnown = stat["dirty"]
		}
	}
	r.CPUCapacity = float64(len(r.AllowedCPUs))
	if !math.IsNaN(r.CPUQuota) {
		r.CPUCapacity = math.Min(r.CPUCapacity, r.CPUQuota)
	}
	if env("SLURM_JOB_ID") != "" {
		if cpus, e := strconv.ParseFloat(slurmCPU(env), 64); e == nil && cpus > 0 && cpus < r.CPUCapacity {
			r.CPUCapacity = cpus
			r.SchedulerCPU = true
		}
		memMB, ok := uintValue(env("SLURM_MEM_PER_NODE"))
		if !ok {
			if perCPU, valid := uintValue(env("SLURM_MEM_PER_CPU")); valid {
				cpus, e := strconv.ParseFloat(slurmMemoryCPU(env), 64)
				if e == nil && cpus > 0 && float64(perCPU)*cpus < float64(math.MaxUint64) {
					memMB = uint64(float64(perCPU) * cpus)
					ok = true
				}
			}
		}
		if ok && memMB > 0 && memMB <= math.MaxUint64/(1024*1024) {
			limit := memMB * 1024 * 1024
			if limit < hostMemory && (!r.MemoryLimitKnown || limit < r.MemoryLimit) {
				r.MemoryLimit = limit
				r.SchedulerMemory = true
				r.MemoryLimitKnown = true
				if !strings.Contains(memoryPath, "slurm") && !strings.Contains(memoryPath, "job_"+env("SLURM_JOB_ID")) {
					r.MemoryUsageKnown = false
				}
			}
		}
	}
	if r.SchedulerCPU && !strings.Contains(cpuPath, "slurm") && !strings.Contains(cpuPath, "job_"+env("SLURM_JOB_ID")) {
		r.CPUUsageKnown = false
	}
	r.Partial = denied
	r.CPUUsageID = cpuPath
	r.CPURestricted = len(r.AllowedCPUs) < hostCPUs || r.CPUCapacity < float64(hostCPUs)
	r.MemoryLimited = r.MemoryLimitKnown && r.MemoryLimit <= hostMemory
	if r.CPURestricted || r.MemoryLimited || !math.IsNaN(r.CPUQuota) {
		r.Scope = "allocation"
	}
	return r
}

func countersFromStatus(text string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if n, ok := uintValue(fields[1]); ok && n <= math.MaxUint64/1024 {
				out[strings.TrimSuffix(fields[0], ":")] = n * 1024
			}
		}
	}
	return out
}

func slurmCPU(env func(string) string) string {
	for _, key := range []string{"SLURM_CPUS_PER_TASK", "SLURM_CPUS_ON_NODE"} {
		if value := env(key); value != "" {
			return value
		}
	}
	return ""
}

func slurmMemoryCPU(env func(string) string) string {
	if v := env("SLURM_CPUS_ON_NODE"); v != "" {
		return v
	}
	return env("SLURM_CPUS_PER_TASK")
}

func reclaimableAt(f resourceFS, dir string, v2 bool) uint64 {
	stat := counters(f.read(filepath.Join(dir, "memory.stat")))
	if v2 {
		return stat["inactive_file"]
	}
	if v, ok := stat["total_inactive_file"]; ok {
		return v
	}
	return stat["inactive_file"]
}
