//go:build darwin

package platform

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func DefaultInterfaces(ctx context.Context) map[string]bool {
	routes := make(map[string]bool)
	for _, family := range []string{"-inet", "-inet6"} {
		query, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		data, err := exec.CommandContext(query, "/sbin/route", "-n", "get", family, "default").Output()
		cancel()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "interface:" {
				routes[fields[1]] = true
				break
			}
		}
	}
	return routes
}
