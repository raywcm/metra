package chart

import (
	"math"
	"strings"
	"time"
)

type Options struct {
	MaxGap                          time.Duration
	Width, Height                   int
	Min, Max                        float64
	Now                             time.Time
	Range                           time.Duration
	Grid, Fill                      bool
	StyleGrid, StyleLine, StyleFill func(...string) string
	StyleEmpty                      func(...string) string
	SeriesLine, SeriesFill          []func(...string) string
}

// Render uses 1×2 block subcells, with separate colors for occupied and empty
// cells. Samples live on a fixed time lattice: moving the viewport translates
// existing segments without changing their rasterized shape.
func Render(series [][]Point, o Options) string {
	if o.Width <= 0 || o.Height <= 0 {
		return ""
	}
	// Bound total work rather than clipping either axis of a valid viewport.
	if o.Width > 250000/o.Height {
		return ""
	}
	const cellW, cellH = 1, 2
	w, h := o.Width*cellW, o.Height*cellH
	columns, rows := w/cellW, h/cellH
	n := columns * rows
	boundary, area, grid := make([]byte, n), make([]byte, n), make([]byte, n)
	lineOwners, fillOwners := make([]int, n), make([]int, n)
	plot := func(dst []byte, x, y int) {
		if x < 0 || x >= w || y < 0 || y >= h {
			return
		}
		bits := byte(1 << (y % cellH))
		dst[y/cellH*columns+x/cellW] |= bits
	}
	if o.Grid {
		for y := 0; y < h; y += max(cellH, rows/4*cellH) {
			for x := 0; x < w; x += 3 * cellW {
				plot(grid, x, y)
			}
		}
		for x := 0; x < w; x += max(2*cellW, (w/6)/cellW*cellW) {
			for y := 0; y < h; y += 2 * cellH {
				plot(grid, x, y)
			}
		}
	}
	if o.Range <= 0 {
		o.Range = time.Minute
	}
	if o.Max <= o.Min {
		o.Max = o.Min + 1
	}
	if o.MaxGap <= 0 {
		o.MaxGap = 5 * time.Second
	}
	step := max(time.Nanosecond, o.Range/time.Duration(max(1, w-1)))
	endColumn := timeColumn(o.Now, step)
	// Earlier legend entries are foreground layers. Draw them last so a
	// smaller second series cannot recolor the first series' occupied blocks.
	for owner := len(series) - 1; owner >= 0; owner-- {
		points := series[owner]
		// An envelope avoids filling the same vertical column for every raster dot.
		tops := make([]int, w)
		for i := range tops {
			tops[i] = h
		}
		add := func(x, y int) {
			plot(boundary, x, y)
			if x >= 0 && x < w && y >= 0 && y < h {
				lineOwners[y/cellH*columns+x/cellW] = owner
			}
			if x >= 0 && x < w && y < tops[x] {
				tops[x] = y
			}
		}
		prev := false
		px, py := 0, 0
		previousTime := time.Time{}
		for _, p := range points {
			if p.At.After(o.Now) || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
				prev = false
				continue
			}
			x := int(timeColumn(p.At, step)-endColumn) + w - 1
			normalized := math.Max(0, math.Min(1, (p.Value-o.Min)/(o.Max-o.Min)))
			// Round to the nearest half-character. Values below half a subcell
			// stay empty instead of creating a minimum-height colored baseline.
			y := h - int(math.Round(normalized*float64(h)))
			if x < 0 {
				// Keep the preceding sample so a segment entering the viewport is
				// cropped, rather than redrawn from a new left-hand endpoint.
				px, py, previousTime, prev = x, y, p.At, true
				continue
			}
			if prev && p.At.Sub(previousTime) <= o.MaxGap {
				line(px, py, x, y, add)
			} else {
				add(x, y)
			}
			px, py, previousTime, prev = x, y, p.At, true
		}
		if o.Fill {
			for x, top := range tops {
				for y := top; y < h; y++ {
					plot(area, x, y)
					idx := y/cellH*columns + x/cellW
					fillOwners[idx] = owner
					if boundary[idx]&(1<<uint(y%cellH)) != 0 {
						lineOwners[idx] = owner
					}
				}
			}
		}
	}
	var b strings.Builder
	b.Grow(n*3 + o.Height)
	for y := 0; y < rows; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		var run strings.Builder
		kind := -1
		flush := func() {
			if run.Len() == 0 {
				return
			}
			s := run.String()
			owner := kind / 4
			switch kind % 4 {
			case 0:
				if o.StyleEmpty != nil {
					s = o.StyleEmpty(s)
				}
			case 1:
				if owner < len(o.SeriesLine) && o.SeriesLine[owner] != nil {
					s = o.SeriesLine[owner](s)
					break
				}
				if o.StyleLine != nil {
					s = o.StyleLine(s)
				}
			case 2:
				if owner < len(o.SeriesFill) && o.SeriesFill[owner] != nil {
					s = o.SeriesFill[owner](s)
					break
				}
				if o.StyleFill != nil {
					s = o.StyleFill(s)
				}
			case 3:
				if o.StyleGrid != nil {
					s = o.StyleGrid(s)
				}
			}
			b.WriteString(s)
			run.Reset()
		}
		for x := 0; x < columns; x++ {
			idx := y*columns + x
			bits := boundary[idx]
			next := 1
			if o.Fill {
				bits |= area[idx]
			}
			if bits == 0 {
				bits = grid[idx]
				next = 3
			} else if boundary[idx] == 0 {
				next = 2
			}
			if bits == 0 {
				next = 0
			}
			if next == 1 {
				next += lineOwners[idx] * 4
			} else if next == 2 {
				next += fillOwners[idx] * 4
			}
			if next != kind {
				flush()
				kind = next
			}
			if bits == 0 && o.StyleEmpty == nil {
				run.WriteByte(' ')
			} else {
				run.WriteRune([4]rune{'█', '▀', '▄', '█'}[bits])
			}
		}
		flush()
	}
	return b.String()
}

// Floor division also keeps the lattice consistent for pre-epoch timestamps.
func timeColumn(at time.Time, step time.Duration) int64 {
	n, d := at.UnixNano(), int64(step)
	column := n / d
	if n < 0 && n%d != 0 {
		column--
	}
	return column
}

func line(x0, y0, x1, y1 int, plot func(int, int)) {
	dx := abs(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -abs(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	e := dx + dy
	for {
		plot(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func Maximum(series ...[]Point) float64 {
	v := 1.0
	for _, s := range series {
		for _, p := range s {
			if !math.IsInf(p.Value, 0) && !math.IsNaN(p.Value) && p.Value > v {
				v = p.Value
			}
		}
	}
	return v * 1.15
}
