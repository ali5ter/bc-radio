package audio

import "math"

// This file holds the shared FFT building blocks the analyser's band
// computation is built from. The transform is a hand-rolled radix-2
// Cooley–Tukey in pure Go (no dependency — the same spirit as the v1
// "no FFT library" decision, applied to the tool the log-scaled range-
// averaged bands actually need): window and twiddle tables are
// precomputed once, the working buffers are reused, and the steady-state
// publish path allocates nothing.

// fftInPlace runs a radix-2 Cooley–Tukey FFT on cbuf in place. len(cbuf)
// must be a power of two. w must hold len(cbuf)/2 precomputed twiddle
// factors where w[k] = e^{−2πi·k/n}.
func fftInPlace(cbuf []complex128, w []complex128) {
	n := len(cbuf)
	if n < 2 {
		return
	}
	// Bit-reversal permutation: reorder cbuf so the butterflies operate on
	// in-order pairs.
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			cbuf[i], cbuf[j] = cbuf[j], cbuf[i]
		}
	}
	// Butterfly stages using the shared twiddle table. At stage size, the
	// stride into the n-sized root-of-unity table is n/size so w is reused
	// across sizes.
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		step := n / size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				t := w[k*step] * cbuf[start+k+half]
				u := cbuf[start+k]
				cbuf[start+k] = u + t
				cbuf[start+k+half] = u - t
			}
		}
	}
}

// buildTwiddles returns the first n/2 complex nth-roots of unity used by
// fftInPlace for an n-point transform.
func buildTwiddles(n int) []complex128 {
	w := make([]complex128, n/2)
	for k := range w {
		angle := -2 * math.Pi * float64(k) / float64(n)
		w[k] = complex(math.Cos(angle), math.Sin(angle))
	}
	return w
}

// buildHannWindow returns the Hann window of a transform of size n: the
// standard symmetric Hann, zero at the last sample, coherent gain 0.5.
func buildHannWindow(size int) []float64 {
	window := make([]float64, size)
	for i := range window {
		window[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(size-1)))
	}
	return window
}
