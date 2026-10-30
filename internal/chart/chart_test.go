package chart

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestHistoryWrapChronological(t *testing.T) {
	var h History
	now := time.Unix(10000, 0)
	for i := 0; i < Capacity+8; i++ {
		h.Add(now.Add(time.Duration(i)*time.Second), float64(i))
	}
	p := h.Points()
	if len(p) != Capacity || p[0].Value != 8 || p[len(p)-1].Value != Capacity+7 {
		t.Fatal("ring did not retain newest samples")
	}
	p[0].Value = -1
	if h.Points()[0].Value != 8 {
		t.Fatal("mutable history exposed")
	}
}
func TestRenderTimeDimensionsAndGrid(t *testing.T) {
	now := time.Unix(100, 0)
	o := Options{Width: 10, Height: 2, Max: 100, Now: now, Range: 10 * time.Second, StyleEmpty: markEmptyBlocks}
	text := Render([][]Point{{{now.Add(-10 * time.Second), 0}, {now, 100}}}, o)
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		t.Fatal(text)
	}
	for _, l := range lines {
		if len([]rune(l)) != 10 {
			t.Fatalf("width: %q", l)
		}
	}
	if []rune(lines[0])[9] != '▀' {
		t.Fatalf("newest point not at right edge: %q", lines[0])
	}
	blank := Render(nil, o)
	o.Grid = true
	if Render(nil, o) == blank {
		t.Fatal("grid missing")
	}
	o.Width = 0
	if Render(nil, o) != "" {
		t.Fatal("zero size")
	}
}

func TestTimestampPlacementAndNoGapBridge(t *testing.T) {
	now := time.Unix(100, 0)
	o := Options{Width: 10, Height: 2, Max: 100, Now: now, Range: 10 * time.Second, StyleEmpty: markEmptyBlocks}
	out := Render([][]Point{{{now.Add(-5 * time.Second), 100}}}, o)
	first := []rune(strings.Split(out, "\n")[0])
	if first[4] != '▀' && first[5] != '▀' {
		t.Fatalf("timestamp not centered: %q", out)
	}
	out = Render([][]Point{{{now.Add(-9 * time.Second), 100}, {now.Add(-5 * time.Second), math.NaN()}, {now, 100}}}, o)
	if []rune(strings.Split(out, "\n")[0])[5] != '░' {
		t.Fatal("bridged a missing sample")
	}
}

func BenchmarkRender(b *testing.B) {
	now := time.Now()
	p := make([]Point, 1800)
	for i := range p {
		p[i] = Point{now.Add(time.Duration(i-1799) * time.Second / 2), 50 + 40*math.Sin(float64(i)/20)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Render([][]Point{p}, Options{Width: 140, Height: 30, Max: 100, Now: now, Range: 15 * time.Minute, Grid: true, Fill: true})
	}
}

func TestLongRangeDoesNotBridgeMissingTime(t *testing.T) {
	now := time.Now()
	s := []Point{{now.Add(-time.Minute), 100}, {now, 100}}
	out := Render([][]Point{s}, Options{Width: 100, Height: 2, Max: 100, Now: now, Range: 15 * time.Minute, MaxGap: 3 * time.Second})
	if []rune(strings.Split(out, "\n")[0])[97] != ' ' {
		t.Fatal("long chart range bridged missing time")
	}
}

func TestFilledFrameAt100Percent(t *testing.T) {
	now := time.Now()
	o := Options{Width: 12, Height: 6, Max: 100, Now: now, Range: time.Second, Fill: true}
	out := Render([][]Point{{{now.Add(-time.Second), 100}, {now, 100}}}, o)
	for _, line := range strings.Split(out, "\n") {
		if line != strings.Repeat("█", 12) {
			t.Fatalf("100%% did not fill frame: %q", line)
		}
	}
}
func TestFillFollowsCurveAndPreservesGaps(t *testing.T) {
	now := time.Now()
	o := Options{Width: 10, Height: 4, Max: 100, Now: now, Range: time.Second, Fill: true, StyleEmpty: markEmptyBlocks}
	out := Render([][]Point{{{now.Add(-time.Second), 50}, {now, 50}}}, o)
	lines := strings.Split(out, "\n")
	if lines[0] != strings.Repeat("░", 10) || lines[3] != strings.Repeat("█", 10) {
		t.Fatalf("fill not below the curve: %q", out)
	}
	out = Render([][]Point{{{now.Add(-time.Second), 100}, {now.Add(-500 * time.Millisecond), math.NaN()}, {now, 100}}}, o)
	for _, line := range strings.Split(out, "\n") {
		if []rune(line)[5] != '░' {
			t.Fatal("filled a missing-data gap")
		}
	}
}

func TestAreaUsesOnlyBlocks(t *testing.T) {
	now := time.Now()
	out := Render([][]Point{{{now.Add(-time.Second), 30}, {now, 100}}}, Options{Width: 30, Height: 12, Max: 100, Now: now, Range: time.Second, Fill: true, Grid: true})
	for _, r := range out {
		if r != '█' && r != '▀' && r != '▄' && r != ' ' && r != '\n' {
			t.Fatalf("non-block glyph %q", r)
		}
	}
}

func TestBlocksScaleAndHalfCells(t *testing.T) {
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		value float64
		want  []string
	}{
		{0, []string{"░░░░░░", "░░░░░░", "░░░░░░", "░░░░░░"}},
		{12.5, []string{"░░░░░░", "░░░░░░", "░░░░░░", "▄▄▄▄▄▄"}},
		{50, []string{"░░░░░░", "░░░░░░", "██████", "██████"}},
		{100, []string{"██████", "██████", "██████", "██████"}},
	} {
		out := Render([][]Point{{{now.Add(-time.Second), tc.value}, {now, tc.value}}}, Options{Width: 6, Height: 4, Max: 100, Now: now, Range: time.Second, Fill: true, StyleEmpty: markEmptyBlocks})
		if out != strings.Join(tc.want, "\n") {
			t.Fatalf("blocks at %g%%: %q", tc.value, out)
		}
	}
}

func TestBlocksRoundToNearestSubcell(t *testing.T) {
	now := time.Unix(100, 0)
	for _, height := range []int{1, 4, 8, 15, 40} {
		quantum := 100.0 / float64(2*height)
		for _, tc := range []struct {
			value float64
			cells int
		}{
			{0, 0}, {0.00001, 0}, {0.04, 0},
			{0.49 * quantum, 0}, {0.5 * quantum, 1}, {0.51 * quantum, 1},
			{1.49 * quantum, 1}, {1.5 * quantum, 2},
			{100, 2 * height},
		} {
			o := Options{Width: 3, Height: height, Max: 100, Now: now, Range: time.Second, Fill: true, StyleEmpty: markEmptyBlocks}
			out := Render([][]Point{{{now.Add(-time.Second), tc.value}, {now, tc.value}}}, o)
			occupied := 0
			for _, r := range out {
				switch r {
				case '█':
					occupied += 2
				case '▀', '▄':
					occupied++
				}
			}
			if occupied != tc.cells*o.Width {
				t.Fatalf("height %d, value %g%%: occupied %d subcells, want %d: %q", height, tc.value, occupied, tc.cells*o.Width, out)
			}
		}
	}
	// Real idle PCIe / HBM readings must not create a colored baseline in a
	// shared plot, even though the underlying counters are slightly positive.
	o := Options{Width: 12, Height: 6, Max: 100, Now: now, Range: time.Second, Fill: true, StyleEmpty: markEmptyBlocks}
	series := [][]Point{{{now.Add(-time.Second), 0}, {now, 0}}, {{now.Add(-time.Second), 0.002}, {now, 0.04}}}
	if got := Render(series, o); strings.Trim(got, "░\n") != "" {
		t.Fatal("near-zero secondary series produced a colored baseline", got)
	}
}

func TestBlocksPreserveGapsAndSeriesColors(t *testing.T) {
	now := time.Unix(100, 0)
	series := [][]Point{{{now.Add(-time.Second), 100}, {now.Add(-500 * time.Millisecond), math.NaN()}, {now, 100}}}
	opts := Options{Width: 9, Height: 3, Max: 100, Now: now, Range: time.Second, Fill: true, StyleEmpty: markEmptyBlocks}
	for _, line := range strings.Split(Render(series, opts), "\n") {
		if line != "█░░░░░░░█" {
			t.Fatalf("filled missing history: %q", line)
		}
	}
	wrap := func(tag string) func(...string) string {
		return func(s ...string) string { return "<" + tag + ">" + strings.Join(s, "") + "</" + tag + ">" }
	}
	opts.StyleEmpty = wrap("empty")
	opts.SeriesLine = []func(...string) string{wrap("read"), wrap("write")}
	opts.SeriesFill = opts.SeriesLine
	series = append(series, []Point{{now.Add(-time.Second), 0}, {now, 50}})
	out := Render(series, opts)
	for _, tag := range []string{"<read>", "<write>", "<empty>"} {
		if !strings.Contains(out, tag) {
			t.Fatalf("missing %s in %q", tag, out)
		}
	}
}

// Make unoccupied cells readable without ANSI colors in assertions.
func TestFirstSeriesRemainsForeground(t *testing.T) {
	now := time.Unix(100, 0)
	wrap := func(tag string) func(...string) string {
		return func(s ...string) string { return tag + strings.Join(s, "") + tag }
	}
	o := Options{Width: 3, Height: 5, Max: 100, Now: now, Range: time.Second, Fill: true,
		SeriesLine: []func(...string) string{wrap("A"), wrap("B")},
		SeriesFill: []func(...string) string{wrap("A"), wrap("B")}}
	horizontal := func(v float64) []Point { return []Point{{now.Add(-time.Second), v}, {now, v}} }
	for _, values := range [][2]float64{{80, 20}, {6.4, 1.1}, {8, 5.8}} {
		out := Render([][]Point{horizontal(values[0]), horizontal(values[1])}, o)
		if strings.Contains(out, "B") || !strings.Contains(out, "A") {
			t.Fatalf("smaller second series covered the foreground: %v %q", values, out)
		}
	}
	out := Render([][]Point{horizontal(20), horizontal(80)}, o)
	if !strings.Contains(out, "B") || !strings.Contains(out, "A") {
		t.Fatal("secondary series above the foreground should remain visible", out)
	}
}

func markEmptyBlocks(s ...string) string {
	return strings.ReplaceAll(strings.Join(s, ""), "█", "░")
}

func TestScrollingOnlyTranslatesHistoricalCurve(t *testing.T) {
	now := time.Unix(100, 123_000_000)
	points := make([]Point, 16)
	for i := range points {
		points[i] = Point{now.Add(-11*time.Second + time.Duration(i)*617*time.Millisecond), float64((i*37 + 13) % 101)}
	}
	for _, width := range []int{17, 67, 119} {
		o := Options{Width: width, Height: 9, Max: 100, Now: now, Range: 13 * time.Second, Fill: true, StyleEmpty: markEmptyBlocks}
		before := strings.Split(Render([][]Point{points}, o), "\n")
		step := int64(o.Range / time.Duration(width-1))
		for ms := 17; ms < 7000; ms += 29 {
			o.Now = now.Add(time.Duration(ms) * time.Millisecond)
			shift := int(o.Now.UnixNano()/step - now.UnixNano()/step)
			after := strings.Split(Render([][]Point{points}, o), "\n")
			for row := range before {
				if string([]rune(before[row])[shift:]) != string([]rune(after[row])[:width-shift]) {
					t.Fatalf("historical curve changed vertically at width %d, +%d ms, row %d", width, ms, row)
				}
			}
		}
	}
}

func TestNewSamplesOnlyExtendTheCurveTail(t *testing.T) {
	now := time.Unix(100, 123_000_000)
	points := []Point{{now.Add(-5 * time.Second), 15}, {now.Add(-3 * time.Second), 87}, {now.Add(-time.Second), 23}}
	o := Options{Width: 67, Height: 9, Max: 100, Now: now, Range: 13 * time.Second, Fill: true, StyleEmpty: markEmptyBlocks}
	before := strings.Split(Render([][]Point{points}, o), "\n")
	step := int64(o.Range / time.Duration(o.Width-1))
	lastColumn := int(points[len(points)-1].At.UnixNano()/step-now.UnixNano()/step) + o.Width - 1
	o.Now = now.Add(370 * time.Millisecond)
	shift := int(o.Now.UnixNano()/step - now.UnixNano()/step)
	points = append(points, Point{o.Now, 91})
	after := strings.Split(Render([][]Point{points}, o), "\n")
	for row := range before {
		if string([]rune(before[row])[shift:lastColumn]) != string([]rune(after[row])[:lastColumn-shift]) {
			t.Fatalf("new sample redrew historical segment at row %d", row)
		}
	}
}
