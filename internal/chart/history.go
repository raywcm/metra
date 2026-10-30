package chart

import "time"

type Point struct {
	At    time.Time
	Value float64
}

const Capacity = 1802 // 15 minutes at the minimum 500 ms sample interval.
type History struct {
	points []Point
	next   int
	full   bool
}

func (h *History) Add(at time.Time, value float64) {
	if h.points == nil {
		h.points = make([]Point, Capacity)
	}
	h.points[h.next] = Point{at, value}
	h.next = (h.next + 1) % len(h.points)
	if h.next == 0 {
		h.full = true
	}
}
func (h *History) Points() []Point {
	n := h.next
	start := 0
	if h.full {
		n = len(h.points)
		start = h.next
	}
	out := make([]Point, n)
	for i := range out {
		out[i] = h.points[(start+i)%len(h.points)]
	}
	return out
}

type Store map[string]*History

func (s Store) Add(key string, at time.Time, value float64) {
	if s[key] == nil {
		s[key] = &History{}
	}
	s[key].Add(at, value)
}

// Forget removed devices to bound memory when interfaces/disks are hot-plugged.
func (s Store) Retain(keys map[string]bool) {
	for k := range s {
		if !keys[k] {
			delete(s, k)
		}
	}
}
func (s Store) Points(key string) []Point {
	if h := s[key]; h != nil {
		return h.Points()
	}
	return nil
}
