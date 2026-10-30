//go:build linux

package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type linuxNPU struct{ ascend NPUBackend }

func NewNPU() NPUBackend { return linuxNPU{ascend: newAscendNPU()} }
func (b linuxNPU) Sample(ctx context.Context) ([]NPU, error) {
	paths, _ := filepath.Glob("/sys/class/accel/accel[0-9]*")
	var out []NPU
	for _, p := range paths {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if strings.Contains(filepath.Base(p), "-") {
			continue
		}
		real, _ := filepath.EvalSymlinks(filepath.Join(p, "device"))
		driver, _ := filepath.EvalSymlinks(filepath.Join(p, "device/driver"))
		name := filepath.Base(driver)
		if name == "." || name == "" {
			name = "NPU"
		}
		n := EmptyNPU(real, name+" · "+filepath.Base(p), "accel sysfs")
		for _, file := range []string{"device/npu_busy_percent", "device/busy_percent"} {
			if _, err := os.Stat(filepath.Join(p, file)); err == nil {
				n.Utilization = readNumber(filepath.Join(p, file))
				break
			}
		}
		out = append(out, n)
	}
	if b.ascend != nil {
		devices, err := b.ascend.Sample(ctx)
		out = append(out, devices...)
		return out, err
	}
	// Expose real mapped Ascend devices even if the vendor tool is absent.
	devices, _ := filepath.Glob("/dev/davinci[0-9]*")
	for _, device := range devices {
		out = append(out, EmptyNPU("ascend:"+filepath.Base(device), "Ascend · "+filepath.Base(device), "device node"))
	}
	if len(devices) > 0 {
		return out, errors.New("Ascend devices found, but npu-smi is unavailable")
	}
	return out, nil
}
