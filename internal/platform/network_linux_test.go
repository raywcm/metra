//go:build linux

package platform

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultRouteInterfaces(t *testing.T) {
	v4 := "eth0 00000000 0100000A 0003 0 0 100 00000000 0 0 0\neth1 00000000 00000000 0001 0 0 0 000000FF 0 0 0\ndown0 00000000 00000000 0000 0 0 0 00000000 0 0 0"
	v6 := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000400 00000000 00000000 00000001 enp1s0"
	got := defaultInterfaces(v4, v6)
	if !got["eth0"] || !got["enp1s0"] || len(got) != 2 {
		t.Fatal(got)
	}
}

func TestDefaultRouteKeepsLowestMetricPerFamily(t *testing.T) {
	v4 := strings.Join([]string{
		"wifi0 00000000 0100000A 0003 0 0 600 00000000",
		"bad0 00000000 0100000A 0003 0 0 invalid 00000000",
		"eth1 00000000 0100000A 0003 0 0 100 00000000",
		"eth0 00000000 0100000A 0003 0 0 100 00000000",
		"reject0 00000000 0100000A 0201 0 0 1 00000000",
	}, "\n")
	zero := strings.Repeat("0", 32)
	v6 := strings.Join([]string{
		zero + " 00 " + zero + " 00 " + zero + " 000000ff 0 0 00000001 backup6",
		zero + " 00 " + zero + " 00 " + zero + " 0000000a 0 0 00000001 primary6",
		zero + " 00 20010db8000000000000000000000000 40 " + zero + " 00000001 0 0 00000001 private6",
	}, "\n")
	want := map[string]bool{"eth0": true, "eth1": true, "primary6": true}
	if got := defaultInterfaces(v4, v6); !reflect.DeepEqual(got, want) {
		t.Errorf("default interfaces = %v, want %v", got, want)
	}
}
