//go:build darwin

package platform

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestNativeAccelerators(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gpus, err := NewGPU().Sample(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, g := range gpus {
		if g.ID == "" || seen[g.ID] {
			t.Fatal("invalid GPU identity")
		}
		seen[g.ID] = true
		if !math.IsNaN(g.Utilization) && (g.Utilization < 0 || g.Utilization > 100) {
			t.Fatal(g.Utilization)
		}
	}
	npus, err := NewNPU().Sample(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range npus {
		if n.ID == "" || n.Name == "" {
			t.Fatal("invalid NPU identity")
		}
		if !math.IsNaN(n.Utilization) && (n.Utilization < 0 || n.Utilization > 100) {
			t.Fatal(n.Utilization)
		}
	}
}
