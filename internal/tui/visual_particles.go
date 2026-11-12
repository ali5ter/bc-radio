package tui

import (
	"math"
	"math/rand"
)

// particles is a reactive particle field: bass energy spawns sparks that
// arc upward, drift, and fade. It is the only stateful visualizer —
// positions and velocities persist across frames. The RNG is seeded once
// with a fixed value, so a given sequence of band frames always produces
// the same field: renders are a deterministic function of the input.
type particles struct {
	rng *rand.Rand
	ps  []particle
}

// particle is one spark, in normalized [0,1] screen coordinates.
type particle struct {
	x, y, vx, vy float64
	life         float64 // seconds remaining
}

// Tuning: population and motion are sized for a 60 fps render cadence.
const (
	particleMinPop = 16
	particleMaxPop = 256
	particleFrame  = 1.0 / 60.0 // one Render call == one frame == this much simulated time
)

// newParticles builds a particles visualizer with its deterministic seed.
func newParticles() *particles {
	return &particles{rng: rand.New(rand.NewSource(1))}
}

// Name implements visualizer.
func (p *particles) Name() string { return "particles" }

// bassEnergy is the mean of the four lowest bands, the kick and bass drum
// band: what drives the field's energy.
func bassEnergy(bands []float64) float64 {
	n := 4
	if n > len(bands) {
		n = len(bands)
	}
	if n == 0 {
		return 0
	}
	sum := 0.0
	for k := 0; k < n; k++ {
		sum += bands[k]
	}
	return sum / float64(n)
}

// Render implements visualizer. It advances the simulation by one frame:
// age the particles, let them die, spawn toward the energy-driven target
// population, then rasterize.
func (p *particles) Render(bands []float64, w, h int) string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	energy := bassEnergy(bands)

	// Age and move; keep what is still alive and on screen.
	alive := p.ps[:0]
	for _, pt := range p.ps {
		pt.life -= particleFrame
		if pt.life <= 0 {
			continue
		}
		pt.vx *= 0.985                          // drag
		pt.vy = pt.vy*0.985 - 0.6*particleFrame // drag, then a light fall
		pt.x += pt.vx * particleFrame
		pt.y += pt.vy * particleFrame
		if pt.x < 0 || pt.x > 1 || pt.y < 0 || pt.y > 1 {
			continue
		}
		alive = append(alive, pt)
	}

	// The louder the bass, the more sparks: 16 at rest, 256 at full scale.
	target := particleMinPop + int(energy*float64(particleMaxPop-particleMinPop)+0.5)
	for len(alive) < target {
		alive = append(alive, p.spawn(energy))
	}
	if len(alive) > target {
		// Surplus are the oldest (spawn order), so drop the front.
		alive = alive[len(alive)-target:]
	}
	p.ps = alive

	grid := make([]rune, w*h)
	for i := range grid {
		grid[i] = ' '
	}
	for _, pt := range p.ps {
		cx := int(pt.x * float64(w))
		cy := int(pt.y * float64(h))
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
		r := '·'
		if math.Hypot(pt.vx, pt.vy) > 0.3 {
			r = '•'
		}
		grid[cy*w+cx] = r
	}
	var out string
	for y := 0; y < h; y++ {
		if y > 0 {
			out += "\n"
		}
		out += styleVisual.Render(string(grid[y*w : (y+1)*w]))
	}
	return out
}

// spawn emits one spark from the lower half of the field, biased upward,
// with speed and lifetime scaled by the current bass energy.
func (p *particles) spawn(energy float64) particle {
	speed := 0.08 + 0.7*energy*p.rng.Float64()
	ang := math.Pi * (0.15 + 0.7*p.rng.Float64()) // upward fan
	return particle{
		x:    0.5 + (p.rng.Float64()-0.5)*0.7,
		y:    0.7 + 0.3*p.rng.Float64(),
		vx:   math.Cos(ang) * speed,
		vy:   -math.Sin(ang) * speed,
		life: 1.0 + 2.5*p.rng.Float64(),
	}
}
