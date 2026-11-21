package collector

import (
	"context"
	"runtime"
	"slices"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

type processCounter struct {
	total   float64
	created int64
	at      time.Time
}

type processReader func(context.Context, int32) (Process, processCounter, bool)

type processSweep struct {
	pending  []int32
	retrying bool
	rows     []Process
	counters map[int32]processCounter
}

func (c *Collector) readProcesses(ctx context.Context, pids []int32, read processReader) ([]Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resuming := c.sweep != nil
	if !resuming {
		c.sweep = &processSweep{pending: pids, rows: make([]Process, 0, len(pids)), counters: make(map[int32]processCounter, len(pids))}
	}
	scan := c.sweep
	for len(scan.pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid := scan.pending[0]
		row, current, ok := read(ctx, pid)
		// Retry an interrupted read with the next budget. A consistently
		// unreadable PID must not hold the entire list indefinitely.
		if !ok && ctx.Err() != nil && !scan.retrying {
			scan.retrying = true
			return nil, ctx.Err()
		}
		scan.retrying = false
		scan.pending = scan.pending[1:]
		if !ok {
			if ctx.Err() != nil {
				// The completed sweep checks this PID against the live inventory.
				scan.rows = append(scan.rows, Process{PID: pid, Name: "N/A", CPU: missing()})
			}
			continue
		}
		prev, hadCounter := c.proc[pid]
		if !current.at.IsZero() {
			if hadCounter && current.created > 0 && current.created == prev.created && current.at.After(prev.at) && current.total >= prev.total {
				row.CPU = (current.total - prev.total) / current.at.Sub(prev.at).Seconds() * 100
			}
			scan.counters[pid] = current
		} else if hadCounter && row.Created > 0 && row.Created == prev.created {
			scan.counters[pid] = prev
		}
		scan.rows = append(scan.rows, row)
	}
	if resuming {
		// The PID inventory may change between chunks. New arrivals are
		// sampled on the next sweep, and departed PIDs must disappear now.
		live := make(map[int32]bool, len(pids))
		for _, pid := range pids {
			live[pid] = true
		}
		scan.rows = slices.DeleteFunc(scan.rows, func(row Process) bool {
			if live[row.PID] {
				return false
			}
			delete(scan.counters, row.PID)
			return true
		})
	}
	c.sweep = nil
	c.proc = scan.counters
	return scan.rows, nil
}

func readProcess(ctx context.Context, pid int32) (Process, processCounter, bool) {
	p, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return Process{}, processCounter{}, false
	}
	name, err := p.NameWithContext(ctx)
	if err != nil {
		name = "N/A"
	}
	created, _ := p.CreateTimeWithContext(ctx)
	ppid, _ := p.PpidWithContext(ctx)
	mi, memErr := p.MemoryInfoWithContext(ctx)
	threads, threadsErr := p.NumThreadsWithContext(ctx)
	row := Process{PID: pid, PPID: ppid, Name: name, Created: created, CPU: missing(), Threads: threads, ThreadsKnown: threadsErr == nil && threads > 0}
	if mi != nil && memErr == nil && (runtime.GOOS != "darwin" || row.ThreadsKnown) {
		row.MemoryKnown = true
		row.RSS = mi.RSS
		row.VMS = mi.VMS
	}
	var counter processCounter
	times, err := p.TimesWithContext(ctx)
	if ctx.Err() != nil {
		return Process{}, processCounter{}, false
	}
	if err == nil && (runtime.GOOS != "darwin" || row.ThreadsKnown) {
		counter = processCounter{total: times.User + times.System, created: created, at: time.Now()}
	}
	return row, counter, true
}
