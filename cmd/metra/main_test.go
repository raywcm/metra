//go:build linux || darwin

package main

import (
	"path/filepath"
	"testing"
)

func TestInvalidExplicitInterval(t *testing.T) {
	for _, interval := range []string{"0", "499ms", "10.0001s"} {
		args := []string{"--snapshot", "--config", filepath.Join(t.TempDir(), "settings.json"), "--interval", interval}
		if err := run(args); err == nil {
			t.Errorf("accepted interval outside supported range: %s", interval)
		}
	}
}
