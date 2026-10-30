//go:build linux

package platform

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCPUFrequencyPoliciesAndAffinity(t *testing.T) {
	root := t.TempDir()
	for _, p := range []struct{ name, cpus, mhz string }{{"policy0", "0 1", "2100000"}, {"policy1", "2 3", "3000000"}} {
		dir := filepath.Join(root, p.name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "affected_cpus"), []byte(p.cpus), 0600)
		os.WriteFile(filepath.Join(dir, "scaling_cur_freq"), []byte(p.mhz), 0600)
	}
	f := readCPUFrequency(context.Background(), root, "", []int{1, 2})
	if f.MHz != 2550 || f.MinMHz != 2100 || f.MaxMHz != 3000 {
		t.Fatal(f)
	}
	f = readCPUFrequency(context.Background(), root, "", []int{2, 3})
	if f.MHz != 3000 {
		t.Fatal("frequency included disallowed cores", f)
	}
}

func TestCPUFrequencyProcFallbackDoesNotUseBogoMIPS(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cpuinfo")
	os.WriteFile(path, []byte("processor : 0\ncpu MHz : 1000.0\nBogoMIPS : 9000\n\nprocessor : 1\ncpu MHz : 2500.0\n"), 0600)
	f := readCPUFrequency(context.Background(), root, path, []int{1})
	if f.MHz != 2500 {
		t.Fatal(f)
	}
	os.WriteFile(path, []byte("processor : 0\nBogoMIPS : 9000\n"), 0600)
	if !math.IsNaN(readCPUFrequency(context.Background(), root, path, []int{0}).MHz) {
		t.Fatal("invented MHz from BogoMIPS")
	}
}
