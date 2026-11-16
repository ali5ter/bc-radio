package audio

import (
	"math"
	"math/rand"
	"testing"
)

// naiveDFT computes the same transform fftInPlace should, in O(n^2) — the
// reference against which the radix-2 code is checked.
func naiveDFT(x []complex128) []complex128 {
	n := len(x)
	out := make([]complex128, n)
	for k := 0; k < n; k++ {
		var s complex128
		for i := 0; i < n; i++ {
			angle := -2 * math.Pi * float64(k*i) / float64(n)
			s += x[i] * complex(math.Cos(angle), math.Sin(angle))
		}
		out[k] = s
	}
	return out
}

// TestFFTInPlaceMatchesDFT pins the transform against the naive O(n^2)
// definition. An impulse must come back all ones; a complex exponential
// at bin k must come back as n at bin k and (numerical zero) elsewhere;
// and random inputs must agree with the naive DFT to float noise.
func TestFFTInPlaceMatchesDFT(t *testing.T) {
	for _, n := range []int{8, 64} {
		w := buildTwiddles(n)

		in := make([]complex128, n)
		in[0] = 1
		fftInPlace(in, w)
		for k, v := range in {
			if v != 1 {
				t.Fatalf("n=%d impulse: out[%d] = %v, want 1+0i", n, k, v)
			}
		}

		in = make([]complex128, n)
		for i := range in {
			angle := 2 * math.Pi * 3 * float64(i) / float64(n)
			in[i] = complex(math.Cos(angle), math.Sin(angle))
		}
		fftInPlace(in, w)
		d := in[3] - complex(float64(n), 0)
		if math.Hypot(real(d), imag(d)) > 1e-9 {
			t.Fatalf("n=%d exp3: out[3] = %v, want ~%d+0i", n, in[3], n)
		}
		for k, v := range in {
			if k == 3 {
				continue
			}
			if math.Hypot(real(v), imag(v)) > 1e-12 {
				t.Fatalf("n=%d exp3: out[%d] = %v, want ~0", n, k, v)
			}
		}

		rng := rand.New(rand.NewSource(7))
		ref := make([]complex128, n)
		for i := range ref {
			ref[i] = complex(rng.Float64()*2-1, rng.Float64()*2-1)
		}
		got := make([]complex128, n)
		copy(got, ref)
		fftInPlace(got, w)
		want := naiveDFT(ref)
		for k := range got {
			d := got[k] - want[k]
			if math.Hypot(real(d), imag(d)) > 1e-9 {
				t.Fatalf("n=%d random: out[%d] differs from naive DFT by %v", n, k, d)
			}
		}
	}
}

// TestFFTParseval pins energy conservation at the analyser's actual size:
// sum|Y|^2 = N * sum|x|^2 for a real windowed input.
func TestFFTParseval(t *testing.T) {
	n := 2048
	w := buildTwiddles(n)
	rng := rand.New(rand.NewSource(11))
	in := make([]complex128, n)
	for i := range in {
		in[i] = complex(rng.Float64()*2-1, 0)
	}
	in2 := make([]complex128, n)
	copy(in2, in)
	fftInPlace(in2, w)

	var energyIn, energyOut float64
	for i := range in {
		energyIn += real(in[i]) * real(in[i])
		re, im := real(in2[i]), imag(in2[i])
		energyOut += re*re + im*im
	}
	want := float64(n) * energyIn
	if got, diff := energyOut, math.Abs(energyOut-want); diff/want > 1e-10 {
		t.Fatalf("Parseval: sum|Y|^2 = %v, want N*sum|x|^2 = %v (rel diff %v)", got, want, diff/want)
	}
}
