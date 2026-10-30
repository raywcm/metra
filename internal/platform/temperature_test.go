package platform

import (
	"math"
	"testing"

	"github.com/shirou/gopsutil/v4/sensors"
)

func TestCPUOnlyTemperatureSensors(t *testing.T) {
	readings := []sensors.TemperatureStat{{SensorKey: "coretemp_package_id_0", Temperature: 53}, {SensorKey: "k10temp_tctl", Temperature: 61}, {SensorKey: "cpu_thermal", Temperature: 48}, {SensorKey: "pACC MTR Temp Sensor0", Temperature: 55}, {SensorKey: "nvme", Temperature: 120}, {SensorKey: "amdgpu_edge", Temperature: 110}, {SensorKey: "PMU tdie1", Temperature: 98}, {SensorKey: "battery", Temperature: 85}, {SensorKey: "coretemp_bad", Temperature: math.NaN()}, {SensorKey: "cpu_zero", Temperature: 0}, {SensorKey: "coretemp_package_id_0", Temperature: 53}}
	got := cpuTemperatures(readings)
	if len(got) != 4 {
		t.Fatal("unrelated or invalid sensors became CPU temperatures", got)
	}
}
