// Package engine is MonoModule's sound and sequencer core.
//
// It is pure Go: no cgo, no I/O on the audio path, no dependency outside the
// standard library. That keeps it testable on any computer and lets the host
// (the Push-specific part) stay thin. The host calls Engine.Render once per
// audio block and Engine's setters from its other goroutines; one mutex
// guards the lot (see engine.go).
//
// The machines (synth models) are original DSP written for this project. The
// names follow the Elektron Monomachine families (TRX, EFM, PI, GND, UW)
// because that is the vocabulary people know, not because any Elektron code
// or sample is used. This project is not affiliated with Elektron.
package engine

import "math"

// clampf limits v to [lo, hi].
func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// lerp is a + (b-a)*t.
func lerp(a, b, t float32) float32 { return a + (b-a)*t }

// expMap maps t in [0,1] onto [lo,hi] exponentially (lo must be > 0).
func expMap(t, lo, hi float32) float32 {
	return lo * float32(math.Pow(float64(hi/lo), float64(clampf(t, 0, 1))))
}

// dbToLin converts decibels to a linear gain.
func dbToLin(db float32) float32 { return float32(math.Pow(10, float64(db)/20)) }

// decayCoef is the per-sample multiplier for an exponential decay that
// reaches -60 dB after sec seconds. sec <= 0 returns 0 (instant).
func decayCoef(sr float64, sec float32) float32 {
	if sec <= 0 {
		return 0
	}
	return float32(math.Exp(math.Log(0.001) / (float64(sec) * sr)))
}

// softClip is a cheap tanh look-alike, exact at 0, saturating at +-1.
func softClip(x float32) float32 {
	if x > 3 {
		return 1
	}
	if x < -3 {
		return -1
	}
	x2 := x * x
	return x * (27 + x2) / (27 + 9*x2)
}

// drive is a soft-clip distortion. amt 0 is a bypass; the result is scaled so
// a full-scale input stays near full scale whatever the amount.
func drive(x, amt float32) float32 {
	if amt <= 0.002 {
		return x
	}
	k := 1 + amt*18
	return softClip(x*k) / softClip(k)
}

// fsin is sin(2*pi*p) for a phase p in cycles. Any finite p works. It is a
// parabola with one correction step: error under 0.1%, several times faster
// than math.Sin, which matters with 16 voices at 44.1 kHz and up.
func fsin(p float32) float32 {
	p -= float32(math.Floor(float64(p)))
	x := (p - 0.5) * (2 * math.Pi) // -pi..pi
	y := 1.27323954*x - 0.405284735*x*absf(x)
	y = 0.225*(y*absf(y)-y) + y
	return -y // sin(2*pi*p) = -sin(x) because x = 2*pi*p - pi
}

func absf(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// rng is a xorshift32 noise source. The zero value is replaced by a seed on
// first use, so it is safe as a struct field.
type rng struct{ s uint32 }

func (r *rng) next() float32 {
	if r.s == 0 {
		r.s = 0x9E3779B9
	}
	r.s ^= r.s << 13
	r.s ^= r.s >> 17
	r.s ^= r.s << 5
	return float32(int32(r.s)) / 2147483648
}

// seed sets the generator state (a zero seed is bumped to a fixed value).
func (r *rng) seed(s uint32) {
	if s == 0 {
		s = 0x9E3779B9
	}
	r.s = s
}

// isBad reports NaN or infinity.
func isBad(x float32) bool {
	return x != x || x > 1e12 || x < -1e12
}

func sgn(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}
