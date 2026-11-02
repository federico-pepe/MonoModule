package engine

import "math"

// svf is a trapezoidal (TPT) state-variable filter. It stays stable while its
// cutoff is modulated, which the per-track LFO and parameter slides do.
type svf struct {
	s1, s2         float32
	k, a1, a2, a3  float32
	hasCoefficient bool
}

// set picks the cutoff (Hz) and Q.
func (f *svf) set(fc, q float32, sr float64) {
	maxF := float32(sr) * 0.45
	if fc > maxF {
		fc = maxF
	}
	if fc < 10 {
		fc = 10
	}
	if q < 0.3 {
		q = 0.3
	}
	g := float32(math.Tan(math.Pi * float64(fc) / sr))
	f.k = 1 / q
	f.a1 = 1 / (1 + g*(g+f.k))
	f.a2 = g * f.a1
	f.a3 = g * f.a2
	f.hasCoefficient = true
}

// tick filters one sample and returns the low-pass, band-pass (unity gain at
// the peak) and high-pass outputs.
func (f *svf) tick(x float32) (lp, bp, hp float32) {
	v3 := x - f.s2
	v1 := f.a1*f.s1 + f.a2*v3
	v2 := f.s2 + f.a2*f.s1 + f.a3*v3
	f.s1 = 2*v1 - f.s1
	f.s2 = 2*v2 - f.s2
	return v2, f.k * v1, x - f.k*v1 - v2
}

func (f *svf) reset() { f.s1, f.s2 = 0, 0 }

// biquad is a direct-form-II-transposed biquad.
type biquad struct {
	b0, b1, b2, a1, a2 float32
	z1, z2             float32
	active             bool
}

func (b *biquad) tick(x float32) float32 {
	y := b.b0*x + b.z1
	b.z1 = b.b1*x - b.a1*y + b.z2
	b.z2 = b.b2*x - b.a2*y
	return y
}

func (b *biquad) reset() { b.z1, b.z2 = 0, 0 }

func (b *biquad) setRaw(b0, b1, b2, a0, a1, a2 float64) {
	b.b0, b.b1, b.b2 = float32(b0/a0), float32(b1/a0), float32(b2/a0)
	b.a1, b.a2 = float32(a1/a0), float32(a2/a0)
	b.active = true
}

// peaking is an RBJ peaking EQ.
func (b *biquad) peaking(fc, q, gainDB float32, sr float64) {
	if fc > float32(sr)*0.45 {
		fc = float32(sr) * 0.45
	}
	A := math.Pow(10, float64(gainDB)/40)
	w0 := 2 * math.Pi * float64(fc) / sr
	alpha := math.Sin(w0) / (2 * float64(q))
	c := math.Cos(w0)
	b.setRaw(1+alpha*A, -2*c, 1-alpha*A, 1+alpha/A, -2*c, 1-alpha/A)
}

// lowShelf is an RBJ low shelf (slope 1).
func (b *biquad) lowShelf(fc, gainDB float32, sr float64) {
	A := math.Pow(10, float64(gainDB)/40)
	w0 := 2 * math.Pi * float64(fc) / sr
	cs, sn := math.Cos(w0), math.Sin(w0)
	alpha := sn / 2 * math.Sqrt(2)
	sa := 2 * math.Sqrt(A) * alpha
	b.setRaw(
		A*((A+1)-(A-1)*cs+sa),
		2*A*((A-1)-(A+1)*cs),
		A*((A+1)-(A-1)*cs-sa),
		(A+1)+(A-1)*cs+sa,
		-2*((A-1)+(A+1)*cs),
		(A+1)+(A-1)*cs-sa,
	)
}

// highShelf is an RBJ high shelf (slope 1).
func (b *biquad) highShelf(fc, gainDB float32, sr float64) {
	A := math.Pow(10, float64(gainDB)/40)
	w0 := 2 * math.Pi * float64(fc) / sr
	cs, sn := math.Cos(w0), math.Sin(w0)
	alpha := sn / 2 * math.Sqrt(2)
	sa := 2 * math.Sqrt(A) * alpha
	b.setRaw(
		A*((A+1)+(A-1)*cs+sa),
		-2*A*((A-1)+(A+1)*cs),
		A*((A+1)+(A-1)*cs-sa),
		(A+1)-(A-1)*cs+sa,
		2*((A-1)-(A+1)*cs),
		(A+1)-(A-1)*cs-sa,
	)
}

// polyBlep is the band-limiting correction for a discontinuity at phase 0.
func polyBlep(t, dt float32) float32 {
	if t < dt {
		t /= dt
		return t + t - t*t - 1
	}
	if t > 1-dt {
		t = (t - 1) / dt
		return t*t + t + t + 1
	}
	return 0
}

// sqOsc is a band-limited (PolyBLEP) square oscillator.
type sqOsc struct{ phase float32 }

func (o *sqOsc) tick(inc float32) float32 {
	if inc > 0.45 {
		inc = 0.45
	}
	t := o.phase
	y := float32(1)
	if t >= 0.5 {
		y = -1
	}
	t2 := t + 0.5
	if t2 >= 1 {
		t2--
	}
	y += polyBlep(t, inc) - polyBlep(t2, inc)
	o.phase += inc
	if o.phase >= 1 {
		o.phase--
	}
	return y
}

// onePoleLP is a one-pole low-pass, used for cheap tone shaping.
type onePoleLP struct{ y float32 }

func (f *onePoleLP) tick(x, a float32) float32 {
	f.y += a * (x - f.y)
	return f.y
}

// onePoleCoef is the smoothing coefficient for a cutoff in Hz.
func onePoleCoef(fc float32, sr float64) float32 {
	return clampf(1-float32(math.Exp(-2*math.Pi*float64(fc)/sr)), 0, 1)
}

// delayLine is a mono circular buffer with fractional (linear) reads.
type delayLine struct {
	buf []float32
	w   int
}

func newDelayLine(n int) *delayLine {
	if n < 4 {
		n = 4
	}
	return &delayLine{buf: make([]float32, n)}
}

func (d *delayLine) write(x float32) {
	d.buf[d.w] = x
	d.w++
	if d.w == len(d.buf) {
		d.w = 0
	}
}

// read returns the sample written delay samples ago (delay >= 1).
func (d *delayLine) read(delay float32) float32 {
	n := len(d.buf)
	if delay < 1 {
		delay = 1
	}
	if delay > float32(n-2) {
		delay = float32(n - 2)
	}
	pos := float32(d.w) - delay
	for pos < 0 {
		pos += float32(n)
	}
	i := int(pos)
	fr := pos - float32(i)
	a := d.buf[i%n]
	b := d.buf[(i+1)%n]
	return a + (b-a)*fr
}

func (d *delayLine) clear() {
	for i := range d.buf {
		d.buf[i] = 0
	}
}
