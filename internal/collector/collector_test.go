package collector

import (
	"context"
	"errors"
	"github.com/raywcm/metra/internal/platform"
	"github.com/shirou/gopsutil/v4/cpu"
	"math"
	"os"
	"testing"
	"time"
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
	c := &Collector{gpu: &gpuSamples{}, npu: platform.NewNPU()}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c.Sample(ctx, false)
	s := c.Sample(ctx, false)
	if len(s.GPUs) != 1 || s.GPUs[0].ID != "test" || !math.IsNaN(s.GPUs[0].Utilization) || len(s.GPUs[0].Engines) != 1 {
		t.Fatal("device identity lost or stale reading reused", s.GPUs)
	}
}
