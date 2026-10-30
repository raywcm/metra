package platform

import (
	"math"
	"os"
	"strings"
)

// Display-only virtual adapters and BMC framebuffers are not compute GPUs.
// A passed-through physical GPU or a vendor vGPU retains its real vendor ID.
func monitoringGPU(vendor, driver string) bool {
	switch strings.ToLower(vendor) {
	case "0x1af4", "0x1234", "0x80ee", "0x1414", "0x1a03", "0x102b":
		return false
	}
	switch strings.ToLower(driver) {
	case "virtio_gpu", "qxl", "bochs", "bochs-drm", "vboxvideo", "hyperv_drm", "ast", "mgag200", "simpledrm":
		return false
	}
	return true
}

func readDRMClock(path string) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return math.NaN()
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return metricNumber(strings.TrimSuffix(strings.ToLower(fields[1]), "mhz"))
		}
	}
	return math.NaN()
}
