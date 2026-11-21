package platform

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const GiB = uint64(1024 * 1024 * 1024)

func put(t *testing.T, root, path, text string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0644); e != nil {
		t.Fatal(e)
	}
}
func noEnv(string) string { return "" }
func TestCPUListAndQuota(t *testing.T) {
	ids, e := parseCPUList("0-3,7,10-11")
	if e != nil || !reflect.DeepEqual(ids, []int{0, 1, 2, 3, 7, 10, 11}) {
		t.Fatal(ids, e)
	}
	for _, bad := range []string{"4-2", "x", "-1", "1-2-3"} {
		if _, e := parseCPUList(bad); e == nil {
			t.Fatal(bad)
		}
	}
	if cpuQuota("250000 100000") != 2.5 || !math.IsNaN(cpuQuota("max 100000")) || !math.IsNaN(cpuQuota("-1 100000")) {
		t.Fatal("quota semantics")
	}
}
func TestCgroupV2AncestorsAndSharedHeadroom(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/self/cgroup", "0::/jobs/child\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
	put(t, root, "sys/fs/cgroup/jobs/child/cpu.max", "max 100000")
	put(t, root, "sys/fs/cgroup/jobs/cpu.max", "250000 100000")
	put(t, root, "sys/fs/cgroup/jobs/child/cpuset.cpus.effective", "4-7")
	put(t, root, "sys/fs/cgroup/jobs/child/cpu.stat", "usage_usec 123456\n")
	put(t, root, "sys/fs/cgroup/jobs/child/memory.max", "1073741824")
	put(t, root, "sys/fs/cgroup/jobs/child/memory.current", "107374182")
	put(t, root, "sys/fs/cgroup/jobs/memory.max", "2147483648")
	put(t, root, "sys/fs/cgroup/jobs/memory.current", "2040109465")
	put(t, root, "sys/fs/cgroup/jobs/child/memory.stat", "file 100\nfile_dirty 10\n")
	r := detectLimits(root, []int{0, 1, 4, 5, 6}, noEnv, 192, 512*GiB)
	if !reflect.DeepEqual(r.AllowedCPUs, []int{4, 5, 6}) || r.CPUCapacity != 2.5 || r.CPUGroupCPUs != 4 || r.CPUUsageNS != 123456000 {
		t.Fatalf("CPU: %+v", r)
	}
	if r.MemoryLimit != GiB || !r.MemoryLimited || r.MemoryRoom != 107374183 || !r.MemoryUsageKnown || r.MemoryDirty != 10 {
		t.Fatalf("memory: %+v", r)
	}
}
func TestCgroupV1AndUnlimitedSentinel(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/self/cgroup", "2:cpu,cpuacct:/job\n3:memory:/job\n4:cpuset:/job\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 / /cg/cpu rw - cgroup cgroup rw,cpu,cpuacct\n2 0 0:2 / /cg/mem rw - cgroup cgroup rw,memory\n3 0 0:3 / /cg/set rw - cgroup cgroup rw,cpuset\n")
	put(t, root, "cg/cpu/job/cpu.cfs_quota_us", "150000")
	put(t, root, "cg/cpu/job/cpu.cfs_period_us", "100000")
	put(t, root, "cg/cpu/job/cpuacct.usage", "400000000")
	put(t, root, "cg/set/job/cpuset.cpus", "2-5")
	put(t, root, "cg/mem/job/memory.limit_in_bytes", "4294967296")
	put(t, root, "cg/mem/job/memory.usage_in_bytes", "1073741824")
	put(t, root, "cg/mem/memory.limit_in_bytes", "9223372036854771712")
	r := detectLimits(root, []int{0, 1, 2, 3, 4, 5, 6, 7}, noEnv, 192, 512*GiB)
	if r.CPUCapacity != 1.5 || len(r.AllowedCPUs) != 4 || r.CPUGroupCPUs != 192 || r.MemoryLimit != 4*GiB || r.MemoryCurrent != GiB {
		t.Fatalf("%+v", r)
	}
	// The same cpuset bounds the accounting group only when the
	// controllers share a hierarchy.
	put(t, root, "proc/self/cgroup", "2:cpu,cpuacct,cpuset:/job\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 / /cg/cpu rw - cgroup cgroup rw,cpu,cpuacct,cpuset\n")
	put(t, root, "cg/cpu/job/cpuset.cpus", "2-5")
	r = detectLimits(root, []int{2, 3}, noEnv, 192, 512*GiB)
	if !r.CPUUsageKnown || r.CPUGroupCPUs != 4 || len(r.AllowedCPUs) != 2 {
		t.Fatalf("incorrect CPU accounting scope: %+v", r)
	}
}
func TestNamespacedRootAndZeroMemoryLimit(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/self/cgroup", "0::/\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 /docker/container /sys/fs/cgroup rw - cgroup2 cgroup rw\n")
	put(t, root, "sys/fs/cgroup/cpu.max", "50000 100000")
	put(t, root, "sys/fs/cgroup/memory.max", "0")
	put(t, root, "sys/fs/cgroup/memory.current", "20")
	r := detectLimits(root, []int{3, 5}, noEnv, 192, 512*GiB)
	if r.CPUCapacity != .5 || !r.MemoryLimited || !r.MemoryLimitKnown || r.MemoryLimit != 0 || r.MemoryRoom != 0 {
		t.Fatalf("%+v", r)
	}
}
func TestSlurmLimitsAreNotHostUsage(t *testing.T) {
	root := t.TempDir()
	values := map[string]string{"SLURM_JOB_ID": "10", "SLURM_CPUS_PER_TASK": "4", "SLURM_MEM_PER_CPU": "2048"}
	env := func(k string) string { return values[k] }
	r := detectLimits(root, nil, env, 192, 512*GiB)
	if r.CPUCapacity != 4 || r.MemoryLimit != 8*GiB || !r.SchedulerMemory || r.MemoryUsageKnown || r.CPUUsageKnown {
		t.Fatalf("%+v", r)
	}
	delete(values, "SLURM_JOB_ID")
	r = detectLimits(root, nil, env, 192, 512*GiB)
	if r.CPUCapacity != 192 || r.MemoryLimited {
		t.Fatal("stale scheduler variables applied without job identity")
	}
}
func TestUnrestrictedHostAndMountRootMapping(t *testing.T) {
	r := detectLimits(t.TempDir(), []int{0, 1, 2, 3}, noEnv, 4, 16*GiB)
	if r.Scope != "host" || r.CPUCapacity != 4 || r.MemoryLimited {
		t.Fatal(r)
	}
	m := resourceMount{root: "/job", point: "/mount"}
	if p, ok := groupPath(m, "/job/task"); !ok || p != "/mount/task" {
		t.Fatal(p, ok)
	}
	if _, ok := groupPath(m, "/other"); ok {
		t.Fatal("unrelated hierarchy mapped")
	}
	status := countersFromStatus("VmSize: 1024 kB\nVmData: 128 kB\n")
	if status["VmSize"] != 1024*1024 {
		t.Fatal(status)
	}
}

func TestSlurmMemoryUsesAllocatedCPUsNotFractionalTimeQuota(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/self/cgroup", "0::/slurm/job_2\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n")
	put(t, root, "cg/slurm/job_2/cpu.max", "250000 100000")
	put(t, root, "cg/slurm/job_2/memory.current", "100")
	put(t, root, "cg/slurm/job_2/cpu.stat", "usage_usec 1")
	env := func(k string) string {
		return map[string]string{"SLURM_JOB_ID": "2", "SLURM_CPUS_PER_TASK": "8", "SLURM_CPUS_ON_NODE": "16", "SLURM_MEM_PER_CPU": "1024"}[k]
	}
	r := detectLimits(root, nil, env, 192, 512*GiB)
	if r.CPUCapacity != 2.5 || r.MemoryLimit != 16*GiB || !r.MemoryUsageKnown {
		t.Fatal(r)
	}
}

func TestCgroupReclaimableCacheCountsTowardAvailable(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/self/cgroup", "0::/job\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n")
	put(t, root, "cg/job/memory.max", "4294967296")
	put(t, root, "cg/job/memory.current", "3221225472")
	put(t, root, "cg/job/memory.stat", "file 2147483648\ninactive_file 2147483648\n")
	r := detectLimits(root, nil, noEnv, 192, 512*GiB)
	if r.MemoryRoom != 3*GiB || r.MemoryHardRoom != GiB || r.MemoryReclaimable != 2*GiB {
		t.Fatal(r)
	}
}

func TestV1DisabledMemoryHierarchyDoesNotApplyParentLimit(t *testing.T) {
	root := t.TempDir()
	put(t, root, "proc/self/cgroup", "1:memory:/parent/job\n")
	put(t, root, "proc/self/mountinfo", "1 0 0:1 / /cg rw - cgroup cgroup rw,memory\n")
	put(t, root, "cg/parent/job/memory.limit_in_bytes", "4294967296")
	put(t, root, "cg/parent/job/memory.usage_in_bytes", "100")
	put(t, root, "cg/parent/memory.limit_in_bytes", "1073741824")
	put(t, root, "cg/parent/memory.use_hierarchy", "0")
	r := detectLimits(root, nil, noEnv, 192, 512*GiB)
	if r.MemoryLimit != 4*GiB {
		t.Fatal("unrelated non-hierarchical parent limit applied", r)
	}
}

func TestSlurmUsageRequiresExactJobIdentity(t *testing.T) {
	for _, group := range []string{"/slurm/job_123/step_0", "/jobs/job_1234", "/slurm"} {
		root := t.TempDir()
		put(t, root, "proc/self/cgroup", "0::"+group+"\n")
		put(t, root, "proc/self/mountinfo", "1 0 0:1 / /cg rw - cgroup2 cgroup rw\n")
		put(t, root, "cg"+group+"/cpu.stat", "usage_usec 100\n")
		put(t, root, "cg"+group+"/memory.current", "100")
		env := func(k string) string {
			return map[string]string{"SLURM_JOB_ID": "12", "SLURM_CPUS_PER_TASK": "2", "SLURM_MEM_PER_NODE": "512"}[k]
		}
		r := detectLimits(root, nil, env, 8, 8*GiB)
		if r.MemoryUsageKnown || r.CPUUsageKnown {
			t.Fatal("attributed unrelated cgroup to job 12", group)
		}
	}
}

func TestHybridCgroupCountersFollowTheirController(t *testing.T) {
	v1 := "1 0 0:1 / /legacy rw - cgroup cgroup rw,cpu,cpuacct,memory\n"
	v2 := "2 0 0:2 / /unified rw - cgroup2 cgroup rw\n"
	for _, mounts := range []string{v1 + v2, v2 + v1} {
		root := t.TempDir()
		put(t, root, "proc/self/mountinfo", mounts)
		put(t, root, "proc/self/cgroup", "0::/session\n1:cpu,cpuacct,memory:/job\n")
		put(t, root, "legacy/job/cpuacct.usage", "1200")
		put(t, root, "legacy/job/cpu.cfs_quota_us", "50000")
		put(t, root, "legacy/job/cpu.cfs_period_us", "100000")
		put(t, root, "legacy/job/memory.usage_in_bytes", "2048")
		put(t, root, "legacy/job/memory.limit_in_bytes", "4096")
		put(t, root, "unified/session/cpu.stat", "usage_usec 300")
		r := detectLimits(root, []int{0, 1}, noEnv, 2, GiB)
		if !r.MemoryUsageKnown || r.MemoryCurrent != 2048 || r.CPUUsageNS != 1200 || r.CPUCapacity != .5 {
			t.Fatalf("wrong counters for mount order %q: %+v", mounts, r)
		}
	}
}

func TestV1MemoryCountersFollowUsageScope(t *testing.T) {
	for _, tc := range []struct {
		hierarchy, stats           string
		cached, dirty, reclaimable uint64
	}{
		{"1", "total_cache 300\ntotal_dirty 20\ntotal_inactive_file 80", 300, 20, 80},
		{"0", "total_cache 300\ntotal_dirty 20\ntotal_inactive_file 80", 100, 4, 10},
		{"1", "", 100, 4, 10},
		{"1", "total_cache 0\ntotal_dirty 0\ntotal_inactive_file 0", 0, 0, 0},
	} {
		root := t.TempDir()
		put(t, root, "proc/self/cgroup", "1:memory:/job")
		put(t, root, "proc/self/mountinfo", "1 0 0:1 / /mem rw - cgroup cgroup rw,memory")
		put(t, root, "mem/job/memory.use_hierarchy", tc.hierarchy)
		put(t, root, "mem/job/memory.limit_in_bytes", "2048")
		put(t, root, "mem/job/memory.usage_in_bytes", "1024")
		put(t, root, "mem/job/memory.stat", "cache 100\ndirty 4\ninactive_file 10\n"+tc.stats)
		r := detectLimits(root, []int{0, 1}, noEnv, 2, GiB)
		if !r.MemoryStatsKnown || !r.MemoryDirtyKnown || r.MemoryCached != tc.cached || r.MemoryDirty != tc.dirty ||
			r.MemoryReclaimable != tc.reclaimable || r.MemoryRoom != 1024+tc.reclaimable {
			t.Errorf("hierarchy %s: cache %d, dirty %d, reclaimable %d, headroom %d", tc.hierarchy, r.MemoryCached, r.MemoryDirty, r.MemoryReclaimable, r.MemoryRoom)
		}
	}
}
