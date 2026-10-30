package platform

import (
	"context"
	"github.com/shirou/gopsutil/v4/process"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestNVIDIAParsing(t *testing.T) {
	g, e := parseNVIDIA("GPU-1, NVIDIA RTX, 42, 1024, 8192, 55, [N/A]\n")
	if e != nil || len(g) != 1 || g[0].Utilization != 42 || g[0].MemoryUsed != 1024*1024*1024 || !math.IsNaN(g[0].Power) {
		t.Fatal(g, e)
	}
	if _, e = parseNVIDIA("invalid,row\n"); e == nil {
		t.Fatal("malformed GPU data accepted")
	}
}
func TestTerminateRequiresIdentity(t *testing.T) {
	ctx := context.Background()
	if Terminate(ctx, 1, 123) == nil || Terminate(ctx, int32(os.Getpid()), 123) == nil {
		t.Fatal("protected PID accepted")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	p, e := process.NewProcess(int32(cmd.Process.Pid))
	if e != nil {
		t.Fatal(e)
	}
	created, e := p.CreateTime()
	if e != nil {
		t.Fatal(e)
	}
	if Terminate(ctx, p.Pid, created-1) == nil {
		t.Fatal("stale PID accepted")
	}
	if Terminate(ctx, p.Pid, 0) == nil {
		t.Fatal("unknown identity accepted")
	}
	if err := Terminate(ctx, p.Pid, created); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("child did not exit")
	}
}
func TestMountMatching(t *testing.T) {
	if !MountBelongs("/dev/disk0s2", "disk0") && !MountBelongs("/dev/nvme0n1p1", "nvme0n1") {
		t.Fatal("no native partition match")
	}
	if MountBelongs("/dev/disk10s2", "disk1") || MountBelongs("/dev/sdaa1", "sda") {
		t.Fatal("prefix collision")
	}
}
