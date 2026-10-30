//go:build darwin

package platform

import "context"

type appleNPU struct{}

func NewNPU() NPUBackend { return appleNPU{} }
func (appleNPU) Sample(ctx context.Context) ([]NPU, error) {
	a, e := nativeIOKit()
	if e != nil {
		return nil, e
	}
	var out []NPU
	for _, class := range []string{"H11ANEIn", "AppleH11ANEInterface", "H1xANELoadBalancer"} {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		e = a.objects(class, func(entry uint32) {
			n := EmptyNPU(a.id(entry, "npu"), "Apple Neural Engine", "IORegistry")
			stats := a.prop(entry, "PerformanceStatistics")
			if stats != 0 {
				n.Utilization = a.num(a.value(stats, "Device Utilization %"))
				n.Power = a.num(a.value(stats, "Power (W)"))
				a.release(stats)
			}
			out = append(out, n)
		})
		if e != nil {
			return nil, e
		}
		if len(out) > 0 {
			break
		}
	}
	return out, nil
}
