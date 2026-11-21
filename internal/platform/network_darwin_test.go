//go:build darwin

package platform

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMacDefaultNetworkMatchesSystemRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	routes := DefaultInterfaces(ctx)
	checked := 0
	for _, family := range []string{"-inet", "-inet6"} {
		output, err := exec.CommandContext(ctx, "/sbin/route", "-n", "get", family, "default").Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "interface:" {
				checked++
				if !routes[fields[1]] {
					t.Errorf("active %s default interface %s missing", family, fields[1])
				}
			}
		}
	}
	if checked == 0 {
		t.Skip("no system default route")
	}
}
