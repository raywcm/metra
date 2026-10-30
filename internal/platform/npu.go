package platform

import (
	"context"
	"math"
)

// NPU counters are independent of GPU counters. Never infer NPU utilization
// from power state, IOServiceBusyTime, or CPU/GPU activity.
type NPU struct {
	Engines                              []Engine
	MemoryKind                           string
	MemoryUsed, MemoryTotal, Temperature float64
	ID, Name, Backend                    string
	Utilization, Power                   float64
	Cores                                int
}
type NPUBackend interface {
	Sample(context.Context) ([]NPU, error)
}

func EmptyNPU(id, name, backend string) NPU {
	return NPU{ID: id, Name: name, Backend: backend, Utilization: math.NaN(), Power: math.NaN(), MemoryUsed: math.NaN(), MemoryTotal: math.NaN(), Temperature: math.NaN()}
}
