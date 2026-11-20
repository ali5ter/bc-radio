package tui

import (
	"math/rand"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
)

// particles is a band-mapped dust field: each band owns one vertical lane
// of the panel, and the lane's dust — motes that rise from the floor,
// drift, and fade — is that band's activity. A loud band throws a dense,
// fast plume; a quiet one a slow trickle; at rest, one faint mote per
// lane. It is the spectrum re-expressed as grain: the pattern the eye
// reads is the shape of the music, and nothing ever leaves its lane, so
// the field cannot smear into uniform noise.
//
// The field is stateful — positions and velocities persist across frames —
// and the RNG is seeded once with a fixed value, so a given sequence of
// band frames always produces the same field: renders are a deterministic
// function of the input.
type particles struct {
	rng  *rand.Rand
	lane []lane
}

// lane is one band's column of dust.
type lane struct {
	motes []particle
}

// particle is one mote, in normalized [0,1] screen coordinates: y = 1 is
// the floor it rises from, y = 0 the ceiling where it dies.
type particle struct {
	x, y, vx, vy float64
	age, ttl     float64 // seconds
}

// Tuning: population and motion are sized for a 60 fps render cadence.
// Each lane holds particleBase motes at rest and up to particleBase +
// particleSpan at full band scale, so the whole field ranges from
// particleMinPop to particleMaxPop motes.
const (
	particleBase   = 1
	particleSpan   = 15
	particleMinPop = contract.BandCount * particleBase
	particleMaxPop = contract.BandCount * (particleBase + particleSpan)
	particleFrame  = 1.0 / 60.0 // one Render call == one frame == this much simulated time
)

// newParticles builds a particles visualizer with its deterministic seed.
func newParticles() *particles {
	return &particles{
		rng:  rand.New(rand.NewSource(1)),
		lane: make([]lane, contract.BandCount),
	}
}

// Name implements visualizer.
func (p *particles) Name() string { return "particles" }

// Render implements visualizer. It advances the simulation by one frame:
// age the motes, let the dead die, spawn toward each lane's energy-driven
// target, then rasterize. A paused frame skips the whole step — the field
// holds its last positions and re-rasterizes them, the same frozen pause
// cue as the spectrum and scope, whose inputs pin on their own.
func (p *particles) Render(f frame) string {
	bands := f.bands
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if len(p.lane) != len(bands) {
		p.lane = make([]lane, len(bands))
	}
	if !f.paused {
		laneW := 1.0 / float64(max(len(p.lane), 1))
		for k := range p.lane {
			l := &p.lane[k]
			// Age and move; keep what is still alive and on screen.
			alive := l.motes[:0]
			for _, m := range l.motes {
				m.age += particleFrame
				if m.age >= m.ttl || m.y <= 0 {
					continue
				}
				m.vx *= 0.985 // drag
				m.x += m.vx * particleFrame
				m.y += m.vy * particleFrame
				alive = append(alive, m)
			}
			// The louder the band, the denser and faster its lane.
			b := bands[k]
			target := particleBase + int(b*float64(particleSpan)+0.5)
			for len(alive) < target {
				alive = append(alive, p.spawn(k, b, laneW))
			}
			if len(alive) > target {
				// Surplus are the oldest (spawn order), so drop the front.
				alive = alive[len(alive)-target:]
			}
			l.motes = alive
		}
	}

	// 0 blank, 1 dim (aging), 2 bright (fresh): the runs get styled below.
	grid := make([]rune, w*h)
	colOf := make([]int, w*h)
	for i := range grid {
		grid[i] = ' '
	}
	for k := range p.lane {
		for _, m := range p.lane[k].motes {
			cx := int(m.x * float64(w))
			cy := int(m.y * float64(h))
			if cx < 0 {
				cx = 0
			}
			if cx >= w {
				cx = w - 1
			}
			if cy < 0 {
				cy = 0
			}
			if cy >= h {
				cy = h - 1
			}
			tier := 1
			if m.age < m.ttl/4 {
				tier = 2 // fresh motes burn bright, then age to a dim trail
			}
			i := cy*w + cx
			if colOf[i] < tier {
				grid[i] = '•'
				colOf[i] = tier
			}
		}
	}
	// Aging motes read as a finer mark than fresh ones.
	for i := range grid {
		if colOf[i] == 1 {
			grid[i] = '·'
		}
	}
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
			style := plain
			switch c {
			case 1:
				style = styleDim
			case 2:
				style = styleVisual
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

// spawn emits one mote for band k from the floor of its lane, rising with
// speed scaled by the band: loud bands shoot, quiet ones creep.
func (p *particles) spawn(k int, b, laneW float64) particle {
	speed := (0.15 + 0.6*b) * (0.6 + 0.8*p.rng.Float64())
	return particle{
		// Lane centre, jittered over most of the lane width: the column
		// stays a column.
		x:   (float64(k) + 0.5 + (p.rng.Float64()-0.5)*0.6) * laneW,
		y:   1.0,
		vx:  (p.rng.Float64() - 0.5) * 0.006,
		vy:  -speed,
		age: 0,
		ttl: 2.0 + 2.0*p.rng.Float64(),
	}
}
