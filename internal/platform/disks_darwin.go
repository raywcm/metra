//go:build darwin

package platform

import (
	"path/filepath"
	"regexp"
)

var physicalDisk = regexp.MustCompile(`^disk[0-9]+$`)

func PhysicalDisk(name string) bool { return physicalDisk.MatchString(name) }
func MountBelongs(device, disk string) bool {
	name := filepath.Base(device)
	return name == disk || regexp.MustCompile(`^`+regexp.QuoteMeta(disk)+`s[0-9]+(s[0-9]+)?$`).MatchString(name)
}
