//go:build darwin

package platform

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type smcVersion struct {
	Major, Minor, Build, Reserved uint8
	Release                       uint16
}
type smcLimits struct {
	Version, Length  uint16
	CPU, GPU, Memory uint32
}
type smcKeyInfo struct {
	Size, Type uint32
	Attributes uint8
}
type smcParameters struct {
	Key                     uint32
	Version                 smcVersion
	Limits                  smcLimits
	Info                    smcKeyInfo
	Result, Status, Command uint8
	Data                    uint32
	Bytes                   [32]byte
}
type smcBindings struct {
	self    func() uint32
	service func(uint32, uintptr) uint32
	open    func(uint32, uint32, uint32, *uint32) int32
	close   func(uint32) int32
	read    func(uint32, uint32, *smcParameters, uintptr, *smcParameters, *uintptr) int32
}

var smcOnce sync.Once
var smcAPI *smcBindings

func smcKey(s string) uint32 { return binary.BigEndian.Uint32([]byte(s)) }

func nativeCPUTemperatures(ctx context.Context) []Temperature {
	a, err := nativeIOKit()
	if err != nil {
		return nil
	}
	smcOnce.Do(func() {
		defer func() {
			if recover() != nil {
				smcAPI = nil
			}
		}()
		lib, e := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if e != nil {
			return
		}
		b := &smcBindings{}
		for name, target := range map[string]any{"mach_task_self": &b.self, "IOServiceGetMatchingService": &b.service, "IOServiceOpen": &b.open, "IOServiceClose": &b.close, "IOConnectCallStructMethod": &b.read} {
			purego.RegisterLibFunc(target, lib, name)
		}
		smcAPI = b
	})
	b := smcAPI
	if b == nil {
		return nil
	}
	service := b.service(0, a.matching("AppleSMC"))
	if service == 0 {
		return nil
	}
	defer a.releaseObject(service)
	var connection uint32
	if b.open(service, b.self(), 0, &connection) != 0 {
		return nil
	}
	defer b.close(connection)
	// CPU-only keys. Do not substitute GPU, battery or ambient data.
	keys := []string{"TC0D", "TC0P", "Tp01", "Tp05", "Tp09", "Tp0D", "Tp0T", "Tp0X", "Tp0b", "Tp0f", "Tp0j", "Tp1h", "Tp1l", "Tp1p", "Tp1t"}
	var out []Temperature
	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}
		in := smcParameters{Key: smcKey(key), Command: 9}
		var value smcParameters
		size := unsafe.Sizeof(value)
		if b.read(connection, 2, &in, unsafe.Sizeof(in), &value, &size) != 0 || value.Result != 0 {
			continue
		}
		in.Info = value.Info
		in.Command = 5
		value = smcParameters{}
		size = unsafe.Sizeof(value)
		if b.read(connection, 2, &in, unsafe.Sizeof(in), &value, &size) != 0 || value.Result != 0 {
			continue
		}
		temp := math.NaN()
		if in.Info.Size == 4 && (in.Info.Type == smcKey("flt ") || in.Info.Type == smcKey("flt\x00")) {
			temp = float64(math.Float32frombits(binary.LittleEndian.Uint32(value.Bytes[:4])))
		}
		if in.Info.Size == 2 && in.Info.Type == smcKey("sp78") {
			temp = float64(int16(binary.BigEndian.Uint16(value.Bytes[:2]))) / 256
		}
		if !math.IsNaN(temp) && !math.IsInf(temp, 0) && temp > 0 && temp <= 150 {
			out = append(out, Temperature{Name: "CPU SMC " + key, Celsius: temp})
		}
	}
	return out
}
