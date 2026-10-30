package collector

import (
	"context"
	"github.com/raywcm/metra/internal/platform"
	"time"
)

type CPU struct {
	Temperatures      []platform.Temperature
	Temperature       float64
	TemperatureKnown  bool
	HostLogical       int
	Capacity          float64
	IDs               []int
	Scoped            bool
	Name              string
	Logical, Physical int
	Percent           float64
	Cores             []float64
	MHz               float64
	MinMHz, MaxMHz    float64
	FrequencyReported bool
	Load              [3]float64
}
type Memory struct {
	Charged                                             uint64
	HostTotal, HostAvailable                            uint64
	Unavailable                                         uint64
	Limited                                             bool
	UsageKnown, AvailableKnown                          bool
	Allocatable                                         uint64
	AllocatableKnown                                    bool
	Modified, Reclaimable, Free                         uint64
	ModifiedKnown                                       bool
	SwapKnown, CachedKnown                              bool
	Total, Used, Available, Cached, SwapTotal, SwapUsed uint64
	Percent                                             float64
}
type Mount struct {
	Path, FSType      string
	Total, Used, Free uint64
	Percent           float64
}
type Disk struct {
	CombinedBusy                             float64
	ReadBusy, WriteBusy                      float64
	ID, Name                                 string
	ReadRate, WriteRate, Busy                float64
	ReadBytes, WriteBytes, ReadOps, WriteOps uint64
	Mounts                                   []Mount
}
type Network struct {
	DefaultRoute, ExcludeFromTotals bool
	Name                            string
	Addresses                       []string
	Up                              bool
	ReceiveRate, SendRate           float64
	Received, Sent, Errors, Drops   uint64
}
type Process struct {
	MemoryKnown, ThreadsKnown bool
	PID, PPID                 int32
	Name                      string
	Created                   int64
	CPU                       float64
	RSS, VMS                  uint64
	Threads                   int32
}
type Detail struct {
	Process
	User, Command, Executable, Status string
	Err                               string
}
type Snapshot struct {
	Limits                 platform.Limits
	At                     time.Time
	Host, OS, Arch, Kernel string
	Uptime                 uint64
	CPU                    CPU
	Memory                 Memory
	Disks                  []Disk
	Networks               []Network
	GPUs                   []platform.GPU
	NPUs                   []platform.NPU
	Volumes                []Mount
	Processes              []Process
	ProcessCount           int
	ProcessesAt            time.Time
	Warnings               []string
}
type Source interface {
	Sample(context.Context, bool) Snapshot
	Detail(context.Context, Process) (Detail, error)
	Terminate(context.Context, Process) error
}
