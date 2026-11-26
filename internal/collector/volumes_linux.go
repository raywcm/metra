package collector

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/common"
	"github.com/shirou/gopsutil/v4/disk"
)

func mountPartitions(ctx context.Context) ([]mountTarget, error) {
	env, _ := ctx.Value(common.EnvKey).(common.EnvMap)
	proc := cmp.Or(env[common.HostProcEnvKey], os.Getenv("HOST_PROC"), "/proc")
	file := cmp.Or(env[common.HostProcMountinfo], os.Getenv("HOST_PROC_MOUNTINFO"), filepath.Join(proc, "self/mountinfo"))
	sys := cmp.Or(env[common.HostSysEnvKey], os.Getenv("HOST_SYS"), "/sys")
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) && filepath.Base(file) == "mountinfo" {
		// Some restricted procfs views only expose mounts. Keep gopsutil's
		// fallback in the same namespace as the paths passed to statfs.
		settings := common.EnvMap{}
		maps.Copy(settings, env)
		settings[common.HostProcMountinfo] = file
		parts, err := disk.PartitionsWithContext(context.WithValue(ctx, common.EnvKey, settings), true)
		if err != nil {
			return nil, err
		}
		var result []mountTarget
		for _, p := range parts {
			result = append(result, mountTarget{path: p.Mountpoint, device: p.Device, fsType: p.Fstype, remote: networkFilesystem(p.Fstype, p.Device)})
		}
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	return visibleMounts(string(data), sys)
}

var mountEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`, `\043`, "#")

// The parent ID establishes stacking order, independent of the order of the
// lines in mountinfo. Resolve that topology before filtering pseudo mounts or
// merging bind mounts: either can hide a data volume and all its children.
func visibleMounts(data, sys string) ([]mountTarget, error) {
	type record struct {
		mountTarget
		parent uint64
	}
	records := make(map[uint64]record)
	for line := range strings.SplitSeq(data, "\n") {
		if line == "" {
			continue
		}
		left, right, ok := strings.Cut(line, " - ")
		fields, fs := strings.SplitN(left, " ", 7), strings.SplitN(right, " ", 3)
		if !ok || len(fields) < 6 || len(fs) < 3 || !strings.HasPrefix(fields[4], "/") {
			return nil, fmt.Errorf("invalid mountinfo entry")
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		parent, parentErr := strconv.ParseUint(fields[1], 10, 64)
		if _, exists := records[id]; err != nil || parentErr != nil || id == 0 || exists {
			return nil, fmt.Errorf("invalid mountinfo IDs %q and %q", fields[0], fields[1])
		}
		device := mountEscapes.Replace(fs[1])
		if device == "/dev/root" {
			if link, err := os.Readlink(filepath.Join(sys, "dev/block", fields[2])); err == nil {
				device = filepath.Join("/dev", filepath.Base(link))
			}
		}
		m := mountTarget{path: mountEscapes.Replace(fields[4]), fsType: fs[0], device: device,
			mountID: id, fsDevice: fields[2], remote: networkFilesystem(fs[0], device)}
		records[id] = record{m, parent}
	}

	type attachment struct {
		parent uint64
		path   string
	}
	children := make(map[attachment]mountTarget, len(records))
	paths := make(map[string]bool)
	for _, r := range records {
		if _, listed := records[r.parent]; !listed || r.parent == r.mountID {
			r.parent = 0 // A chroot may omit its enclosing mount.
		}
		children[attachment{r.parent, r.path}] = r.mountTarget
		paths[r.path] = true
	}
	visible := make(map[string]uint64)
	var result []mountTarget
	// A prefix sorts before its descendants. Enter each path from its
	// nearest visible ancestor, then follow layers at exactly that path.
	for _, path := range slices.Sorted(maps.Keys(paths)) {
		var parent uint64
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			parent = visible[dir]
			if parent != 0 || dir == "/" {
				break
			}
		}
		top, ok := children[attachment{parent, path}]
		if !ok {
			continue
		}
		for {
			next, ok := children[attachment{top.mountID, path}]
			if !ok {
				break
			}
			top = next
		}
		visible[path] = top.mountID
		result = append(result, top)
	}
	return result, nil
}

func networkFilesystem(fs, device string) bool {
	switch fs {
	case "nfs", "nfs4", "cifs", "smb3", "smbfs", "ceph", "afs", "9p", "glusterfs", "lustre":
		return true
	}
	return fs == "fuse" || strings.HasPrefix(fs, "fuse.") ||
		strings.HasPrefix(device, "//") || (!strings.HasPrefix(device, "/") && strings.Contains(device, ":"))
}
