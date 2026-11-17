package tui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
)

// ghost is the ring's colOf marker: the dim style, one level below the
// bands (the centre's -1 stays the neutral plain).
const ghost = -2

// radial is a circular bar meter: contract.BandCount spokes from a centre
// dot, band k pointing k/16 of a turn clockwise from the top, each spoke's
// length set by its band and drawn in the spectrum's low-to-high gradient
// colour, over a faint static ring at full radius. The ring is the meter's
// silhouette in quiet passages — a constant shape to read the rest against,
// never mistaken for data, the way the spectrum's peak cap is a ghost layer
// behind the live bars. A ring dot sits at every band direction, so a spoke
// at full scale lands on its own dot and replaces it.
type radial struct{}

// newRadial builds a radial visualizer.
func newRadial() *radial { return &radial{} }

// Name implements visualizer.
func (r *radial) Name() string { return "radial" }

// Render implements visualizer. The ghost ring is stamped first, then each
// spoke is a trail of dots along its angle, half a cell apart, over it; a
// row is assembled by grouping runs of one colour, so a frame is a few
// hundred style renders, not one per cell.
func (r *radial) Render(f frame) string {
	bands := f.bands
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	cx, cy := (w-1)/2, (h-1)/2
	fcx, fcy := float64(cx), float64(cy)
	// The longest radius that still fits the box: a full-scale frame is a
	// complete circle, never a clipped one.
	maxR := float64(min(cx, w-1-cx, cy, h-1-cy))
	grid := make([]rune, w*h)
	colOf := make([]int, w*h)
	for i := range grid {
		grid[i] = ' '
		colOf[i] = -1
	}
	put := func(x, y, k int) {
		if x < 0 || x >= w || y < 0 || y >= h {
			return
		}
		i := y*w + x
		grid[i] = '•'
		colOf[i] = k
	}
	// The ghost ring: a faint outline at full radius, stamped beneath the
	// spokes. Its steps are a multiple of the 16 band directions (the arc
	// between steps ≈ half a cell), so the outline is dense at any size and
	// a spoke at full scale lands exactly on its own ring dot and replaces
	// it — the ghost is a scale reference, never a second datum.
	if maxR >= 1 {
		steps := 16 * max(1, int(maxR*math.Pi/4+0.5))
		for s := 0; s < steps; s++ {
			a := 2 * math.Pi * float64(s) / float64(steps)
			x := int(fcx + math.Sin(a)*maxR + 0.5)
			y := int(fcy - math.Cos(a)*maxR + 0.5)
			if x < 0 || x >= w || y < 0 || y >= h {
				continue
			}
			i := y*w + x
			grid[i] = '·'
			colOf[i] = ghost
		}
	}
	for k := 0; k < len(bands) && k < contract.BandCount; k++ {
		// Clockwise from the top: band 0 points up, band 4 right, band 8
		// down — the low bands sweep the left side, the high bands the
		// right, so a bass-heavy mix visibly weights left.
		ang := 2 * math.Pi * float64(k) / float64(contract.BandCount)
		dx, dy := math.Sin(ang), -math.Cos(ang)
		for s := 0.5; s <= bands[k]*maxR; s += 0.5 {
			put(int(fcx+dx*s+0.5), int(fcy+dy*s+0.5), k)
		}
	}
	// The centre is shared by every spoke: it keeps the neutral colour.
	grid[cy*w+cx] = '•'
	colOf[cy*w+cx] = -1
	plain := lipgloss.NewStyle()
	var out strings.Builder
	out.Grow(w * (h + 1))
	for y := 0; y < h; y++ {
		x := 0
		for x < w {
			c := colOf[y*w+x]
			end := x + 1
			for end < w && colOf[y*w+end] == c {
				end++
			}
			style := plain // the centre dot
			switch {
			case c >= 0 && c < len(styleBands):
				style = styleBands[c]
			case c == ghost:
				style = styleDim
			}
			out.WriteString(style.Render(string(grid[y*w+x : y*w+end])))
			x = end
		}
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
