package collector

import (
	"slices"
	"time"

	"github.com/raywcm/metra/internal/platform"
	netstat "github.com/shirou/gopsutil/v4/net"
)

type networkCounter struct {
	index          int
	at             time.Time
	received, sent uint64
}

// Interface metadata and byte counters come from separate OS queries. Either
// can supply an inventory when the other returns partial data or fails.
func (c *Collector) sampleNetworks(now time.Time, interfaces []netstat.InterfaceStat, counters []netstat.IOCountersStat, interfacesErr, counterErr error) []Network {
	inventory := make(map[string]*netstat.InterfaceStat)
	if interfacesErr != nil && counterErr != nil {
		for name := range c.nets {
			inventory[name] = nil
		}
	}
	byName := make(map[string]netstat.IOCountersStat, len(counters))
	for _, counter := range counters {
		inventory[counter.Name] = nil
		byName[counter.Name] = counter
	}
	for i := range interfaces {
		inventory[interfaces[i].Name] = &interfaces[i]
	}

	next := make(map[string]networkCounter)
	var networks []Network
	for name, iface := range inventory {
		meta := platform.NetworkInfo{Name: name, DefaultRoute: c.defaultInterfaces[name], Known: iface != nil}
		if iface != nil {
			meta.Up = slices.Contains(iface.Flags, "up")
			meta.Loopback = slices.Contains(iface.Flags, "loopback")
			for _, addr := range iface.Addrs {
				meta.Addresses = append(meta.Addresses, addr.Addr)
			}
		}
		if !platform.UsefulNetwork(meta) {
			continue
		}

		before := c.nets[name]
		if iface != nil && iface.Index > 0 && iface.Index != before.index {
			before = networkCounter{index: iface.Index}
		}
		v, known := byName[name]
		n := Network{
			Name: name, DefaultRoute: meta.DefaultRoute, Up: meta.Up, Addresses: meta.Addresses,
			MetadataUnavailable: !meta.Known, CountersUnavailable: !known,
			ReceiveRate: missing(), SendRate: missing(),
		}
		if known {
			n.Received, n.Sent = v.BytesRecv, v.BytesSent
			n.Errors, n.Drops = v.Errin+v.Errout, v.Dropin+v.Dropout
			if !before.at.IsZero() {
				elapsed := now.Sub(before.at).Seconds()
				n.ReceiveRate = rate(n.Received, before.received, elapsed)
				n.SendRate = rate(n.Sent, before.sent, elapsed)
			}
			before.at, before.received, before.sent = now, n.Received, n.Sent
		}
		next[name] = before
		networks = append(networks, n)
	}
	c.nets = next
	orderNetworks(networks)
	return networks
}
