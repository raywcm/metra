package tui

import (
	"cmp"
	"github.com/raywcm/metra/internal/collector"
	"math"
	"slices"
	"strconv"
	"strings"
)

type processRow struct {
	collector.Process
	Depth int
}

func processRows(ps []collector.Process, query, sortBy string, reverse, tree bool) []processRow {
	query = strings.ToLower(strings.TrimSpace(query))
	byPID := make(map[int32]collector.Process, len(ps))
	for _, p := range ps {
		byPID[p.PID] = p
	}
	keep := make(map[int32]bool, len(ps))
	for _, p := range ps {
		if query == "" || strings.Contains(strings.ToLower(p.Name), query) || strings.Contains(strconv.Itoa(int(p.PID)), query) {
			keep[p.PID] = true
			if tree {
				parent := p.PPID
				seen := map[int32]bool{p.PID: true}
				for !seen[parent] {
					seen[parent] = true
					a, ok := byPID[parent]
					if !ok {
						break
					}
					keep[parent] = true
					parent = a.PPID
				}
			}
		}
	}
	cpuKeys := make(map[int32]float64, len(ps))
	for _, p := range ps {
		key := math.NaN()
		if finite(p.CPU) && p.CPU >= 0 {
			key, _ = strconv.ParseFloat(number(p.CPU), 64)
		}
		cpuKeys[p.PID] = key
	}
	direction := func(n int) int {
		if reverse {
			return -n
		}
		return n
	}
	cpuOrder := func(a, b collector.Process) int {
		av, bv := cpuKeys[a.PID], cpuKeys[b.PID]
		ak, bk := finite(av), finite(bv)
		if ak != bk {
			if ak {
				return -1
			}
			return 1
		}
		if !ak {
			return 0
		}
		return direction(-cmp.Compare(av, bv))
	}
	memoryOrder := func(a, b collector.Process) int {
		if a.MemoryKnown != b.MemoryKnown {
			if a.MemoryKnown {
				return -1
			}
			return 1
		}
		if !a.MemoryKnown {
			return 0
		}
		return direction(-cmp.Compare(a.RSS, b.RSS))
	}
	compare := func(a, b collector.Process) int {
		n := 0
		switch sortBy {
		case "name":
			n = direction(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
		case "pid":
			n = direction(cmp.Compare(a.PID, b.PID))
		case "memory":
			n = memoryOrder(a, b)
			if n == 0 {
				n = cpuOrder(a, b)
			}
		default:
			n = cpuOrder(a, b)
			if n == 0 {
				n = memoryOrder(a, b)
			}
		}
		if n == 0 {
			return cmp.Compare(a.PID, b.PID)
		}
		return n
	}
	filtered := make([]collector.Process, 0, len(keep))
	for _, p := range ps {
		if keep[p.PID] {
			filtered = append(filtered, p)
		}
	}
	slices.SortFunc(filtered, compare)
	out := make([]processRow, 0, len(filtered))
	if !tree {
		for _, p := range filtered {
			out = append(out, processRow{Process: p})
		}
		return out
	}
	children := map[int32][]collector.Process{}
	for _, p := range filtered {
		children[p.PPID] = append(children[p.PPID], p)
	}
	seen := map[int32]bool{}
	var visit func(collector.Process, int)
	visit = func(p collector.Process, depth int) {
		if seen[p.PID] {
			return
		}
		seen[p.PID] = true
		out = append(out, processRow{p, depth})
		for _, child := range children[p.PID] {
			visit(child, min(depth+1, 12))
		}
	}
	for _, p := range filtered {
		if !keep[p.PPID] || p.PPID == p.PID {
			visit(p, 0)
		}
	}
	for _, p := range filtered {
		visit(p, 0)
	}
	return out
}

func (m *Model) processSortArrow() string {
	descending := m.sortBy == "cpu" || m.sortBy == "memory"
	if m.reverse {
		descending = !descending
	}
	if descending {
		return "↓"
	}
	return "↑"
}
