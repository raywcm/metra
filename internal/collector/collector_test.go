package collector

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/cpu"
)

func TestCounterRatesAndResets(t *testing.T) {
	for _, c := range []struct {
		now, old      uint64
		seconds, want float64
	}{{120, 100, 2, 10}, {1, 2, 1, math.NaN()}, {5, 0, 0, math.NaN()}} {
		got := rate(c.now, c.old, c.seconds)
		if !(math.IsNaN(c.want) && math.IsNaN(got)) && got != c.want {
			t.Fatal(c, got)
		}
	}
	a := cpu.TimesStat{User: 20, Idle: 80}
	b := cpu.TimesStat{User: 30, Idle: 90}
	if cpuUsage(a, b) != 50 {
		t.Fatal(cpuUsage(a, b))
	}
	if !math.IsNaN(cpuUsage(a, a)) {
		t.Fatal("counter reset treated as zero")
	}
}
func TestLocalSamplingAndProcessIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("live OS sampling")
	}
	c := New()
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first := c.Sample(ctx, false)
	time.Sleep(80 * time.Millisecond)
	s := c.Sample(ctx, true)
	if s.Memory.Total == 0 || len(s.CPU.Cores) == 0 || s.ProcessCount == 0 {
		t.Fatalf("missing host metrics: %+v", s.Warnings)
	}
	if s.ProcessListTotal != s.ProcessCount || s.ProcessCountUnavailable {
		t.Fatal("process table count did not come from its own PID inventory", s.ProcessListTotal, s.ProcessCount)
	}
	if !s.At.After(first.At) {
		t.Fatal("timestamps")
	}
	if s.CPU.Percent < 0 || s.CPU.Percent > 100 {
		t.Fatal(s.CPU.Percent)
	}
	var self Process
	for _, p := range s.Processes {
		if p.PID == int32(os.Getpid()) {
			self = p
			break
		}
	}
	if self.PID == 0 {
		t.Fatalf("self not listed: %v", s.Warnings)
	}
	d, err := c.Detail(ctx, self)
	if err != nil || d.PID != self.PID {
		t.Fatal(d, err)
	}
	if err = c.Terminate(ctx, self); err == nil {
		t.Fatal("allowed terminating metra itself")
	}
	self.Created--
	if _, err = c.Detail(ctx, self); err == nil {
		t.Fatal("accepted reused PID")
	}
}

func TestPartialCPUCounterResetIsMissing(t *testing.T) {
	before := cpu.TimesStat{User: 12, System: 20, Idle: 100}
	after := cpu.TimesStat{User: 8, System: 25, Idle: 102}
	if got := cpuUsage(before, after); !math.IsNaN(got) {
		t.Fatal("reported usage across reset", got)
	}
}

func TestInterruptedProcessSweepResumesWithoutPublishingPartialRows(t *testing.T) {
	start := time.Now()
	c := &Collector{proc: map[int32]processCounter{
		2: {total: 1, created: 2, at: start},
		3: {total: 0, created: 99, at: start}, // PID 3 was reused.
	}}
	reads := map[int32]int{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read := func(_ context.Context, pid int32) (Process, processCounter, bool) {
		reads[pid]++
		if pid == 2 {
			cancel()
		}
		return Process{PID: pid, Created: int64(pid), CPU: missing()},
			processCounter{total: 2, created: int64(pid), at: start.Add(2 * time.Second)}, true
	}
	rows, err := c.readProcesses(ctx, []int32{1, 2, 3}, read)
	if !errors.Is(err, context.Canceled) || rows != nil || c.proc[2].total != 1 {
		t.Fatalf("unfinished sweep published rows or CPU baselines: %v, %v", rows, err)
	}
	// PID 1 exited and PID 4 arrived while the sweep was in progress.
	rows, err = c.readProcesses(context.Background(), []int32{2, 3, 4}, read)
	if err != nil || len(rows) != 2 || rows[0].PID != 2 || rows[1].PID != 3 || reads[2] != 1 || reads[4] != 0 {
		t.Fatalf("sweep restarted or included exited processes: %v, reads %v, %v", rows, reads, err)
	}
	if rows[0].CPU != 50 || !math.IsNaN(rows[1].CPU) {
		t.Fatalf("wrong process CPU deltas: %v", rows)
	}
	if _, stale := c.proc[1]; stale {
		t.Fatal("exited process kept its CPU baseline")
	}
	rows, err = c.readProcesses(context.Background(), []int32{2, 3, 4}, read)
	if err != nil || len(rows) != 3 || rows[2].PID != 4 {
		t.Fatal("new processes missing from next sweep", rows, err)
	}
}

type gpuSamples struct{ calls int }

func (g *gpuSamples) Sample(context.Context) ([]platform.GPU, error) {
	g.calls++
	if g.calls == 1 {
		device := platform.EmptyGPU("test", "GPU", "fixture")
		device.Engines = []platform.Engine{{ID: "compute", Utilization: 20}}
		return []platform.GPU{device}, errors.New("partial sample")
	}
	return nil, errors.New("temporary failure")
}

func TestDeviceDiscoveredOnPartialSampleSurvivesFailure(t *testing.T) {
	c := newCollector(&gpuSamples{}, platform.NewNPU())
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.Sample(ctx, false)
	s := c.Sample(ctx, false)
	if len(s.GPUs) != 1 || s.GPUs[0].ID != "test" || !math.IsNaN(s.GPUs[0].Utilization) || len(s.GPUs[0].Engines) != 1 {
		t.Fatal("device identity lost or stale reading reused", s.GPUs)
	}
}

func TestInterruptedLastPIDIsRetriedBeforePublishing(t *testing.T) {
	c := &Collector{}
	ctx, cancel := context.WithCancel(context.Background())
	pids := []int32{10, 20}
	calls := map[int32]int{}
	read := func(_ context.Context, pid int32) (Process, processCounter, bool) {
		calls[pid]++
		if pid == 20 && calls[pid] == 1 {
			cancel()
			return Process{}, processCounter{}, false
		}
		return Process{PID: pid}, processCounter{}, true
	}
	defer cancel()
	rows, err := c.readProcesses(ctx, pids, read)
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("interrupted final PID was published as an exit: rows=%v err=%v", rows, err)
	}
	rows, err = c.readProcesses(context.Background(), pids, read)
	if err != nil || len(rows) != 2 || rows[1].PID != 20 || calls[10] != 1 || calls[20] != 2 {
		t.Fatal("did not resume the unfinished PID", rows, calls, err)
	}
}

func TestProcessCPURecoversAfterMissingCounter(t *testing.T) {
	c := &Collector{}
	start := time.Unix(100, 0)
	for i, sample := range []struct {
		created int64
		total   float64
		known   bool
		want    float64
	}{{1, 2, true, math.NaN()}, {1, 0, false, math.NaN()}, {1, 3, true, 50}, {2, 0, false, math.NaN()}, {2, 22, true, math.NaN()}, {2, 23, true, 100}} {
		rows, err := c.readProcesses(context.Background(), []int32{10}, func(_ context.Context, pid int32) (Process, processCounter, bool) {
			var counter processCounter
			if sample.known {
				counter = processCounter{created: sample.created, total: sample.total, at: start.Add(time.Duration(i) * time.Second)}
			}
			return Process{PID: pid, Created: sample.created, CPU: missing()}, counter, true
		})
		got := rows[0].CPU
		if err != nil || !(got == sample.want || math.IsNaN(got) && math.IsNaN(sample.want)) {
			t.Fatalf("sample %d got %g, want %g, error %v", i, got, sample.want, err)
		}
	}
}

func TestRepeatedlyInterruptedPIDDoesNotBlockOtherProcesses(t *testing.T) {
	c := &Collector{}
	pids := []int32{10, 20}
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := c.readProcesses(ctx, pids, func(_ context.Context, pid int32) (Process, processCounter, bool) {
			if pid != 10 {
				t.Fatalf("expected to resume PID 10, got %d", pid)
			}
			cancel()
			return Process{}, processCounter{}, false
		})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	rows, err := c.readProcesses(context.Background(), pids, func(_ context.Context, pid int32) (Process, processCounter, bool) {
		return Process{PID: pid}, processCounter{}, true
	})
	if err != nil || len(rows) != 2 || rows[0].PID != 10 || rows[0].Created != 0 || !math.IsNaN(rows[0].CPU) || rows[1].PID != 20 {
		t.Fatal("an interrupted PID disappeared, exposed stale data, or blocked the sweep", rows, err)
	}
}
