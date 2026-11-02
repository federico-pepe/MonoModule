package engine

import "math"

// The synth machines' sound generators. They are original DSP written for
// this project; the machine names and parameter layout follow the
// Monomachine's model. Every generator is a plain struct embedded in a voice
// (no interfaces, no allocation at run time). Parameters arrive normalised
// 0..1. trig starts a note; render adds a block of mono samples to out.

// waveSource gives the generators access to user waveform tables.
type waveSource interface {
	userTable(i int) *[waveLen]float32
}

// genCtx is what a generator needs besides its parameters.
type genCtx struct {
	sr  float32
	bpm float32
	src waveSource
}

func pow2(x float32) float32 { return float32(math.Exp2(float64(x))) }

// semis turns a normalised 0..1 parameter into +-range semitones around 0.5.
func semis(p, rng float32) float32 { return (p - 0.5) * 2 * rng }

func tabRead(t *[waveLen]float32, ph float32) float32 {
	ph -= float32(math.Floor(float64(ph)))
	x := ph * waveLen
	i := int(x)
	fr := x - float32(i)
	a := t[i&(waveLen-1)]
	b := t[(i+1)&(waveLen-1)]
	return a + (b-a)*fr
}

func (c *genCtx) table(i int) *[waveLen]float32 {
	if i < NumFactoryWaves {
		if i < 0 {
			i = 0
		}
		return &factoryWaves[i]
	}
	if c.src != nil {
		if t := c.src.userTable(i - NumFactoryWaves); t != nil {
			return t
		}
	}
	return &factoryWaves[0]
}

// crush quantises x to fewer bits as amt goes from 0 to 1 (0 = off).
func crush(x, amt float32) float32 {
	if amt < 0.02 {
		return x
	}
	lv := pow2(14 - amt*12) // 16384 .. 4 levels
	return float32(math.Floor(float64(x*lv)+0.5)) / lv
}

func wrap1(p float32) float32 {
	if p >= 1 {
		p--
	}
	return p
}

// sawBlep is a band-limited rising saw, phase in 0..1.
func sawBlep(ph, inc float32) float32 { return 2*ph - 1 - polyBlep(ph, inc) }

// pulseBlep is a band-limited pulse of width w.
func pulseBlep(ph, inc, w float32) float32 {
	y := float32(-1)
	if ph < w {
		y = 1
	}
	t2 := ph - w
	if t2 < 0 {
		t2++
	}
	return y + polyBlep(ph, inc) - polyBlep(t2, inc)
}

// drifter is a slow random pitch wobble.
type drifter struct {
	v, tgt float32
	r      rng
}

// next returns a frequency multiplier close to 1 for amount amt (0..1).
func (d *drifter) next(amt float32) float32 {
	if amt < 0.01 {
		return 1
	}
	d.tgt = d.r.next()
	d.v += (d.tgt - d.v) * 0.03
	return 1 + d.v*amt*0.02
}

// ------------------------------------------------------------ GND

type gndEmpty struct{}

func (g *gndEmpty) trig(float32, float32, *[8]float32) {}
func (g *gndEmpty) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
}

type gndSin struct {
	ph, sub, fb float32
	d           drifter
}

func (g *gndSin) trig(_, _ float32, p *[8]float32) { g.ph = p[6]; g.sub = 0; g.fb = 0 }

func (g *gndSin) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[0], 1)/12)
	f *= g.d.next(p[5])
	inc := f / c.sr
	harm, fold, sub, fb := p[1], p[2], p[3], p[4]*0.7
	for i := range out {
		g.ph = wrap1(g.ph + inc)
		s := fsin(g.ph + fb*g.fb)
		g.fb = s
		y := s + harm*0.5*fsin(2*g.ph) + harm*0.3*fsin(3*g.ph)
		if fold > 0.01 {
			y = fsin(y * (0.25 + fold*0.9))
		}
		g.sub = wrap1(g.sub + inc*0.5)
		y += sub * 0.6 * fsin(g.sub)
		out[i] += crush(y*0.8, p[7])
	}
}

type gndNois struct {
	r              rng
	b0, b1, b2, br float32
	hold           float32
	cnt            float32
	hp, lp         svf
	prev           float32
}

func (g *gndNois) trig(float32, float32, *[8]float32) {}

func (g *gndNois) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	g.hp.set(expMap(p[0], 20, 12000), 0.7, float64(c.sr))
	g.lp.set(expMap(p[1], 60, 18000), lerp(0.7, 8, p[2]*p[2]), float64(c.sr))
	color, crack := p[3], p[4]
	rate := 1 - p[5]*0.98 // sample&hold: 1 = every sample
	tilt := p[7]
	for i := range out {
		w := g.r.next()
		g.b0 = 0.99765*g.b0 + w*0.0990460
		g.b1 = 0.96300*g.b1 + w*0.2965164
		g.b2 = 0.57000*g.b2 + w*1.0526913
		pink := (g.b0 + g.b1 + g.b2 + w*0.1848) * 0.25
		g.br = (g.br + w*0.02) * 0.998
		x := w*(1-color) + color*(pink*0.7+g.br*6*color)
		g.cnt += rate
		if g.cnt >= 1 {
			g.cnt--
			g.hold = x
		}
		x = g.hold
		if crack > 0.01 && absf(g.r.next()) > 1-crack*0.02 {
			x += g.r.next() * 2
		}
		if tilt > 0.01 {
			x += (x - g.prev) * tilt * 1.5
		}
		g.prev = x
		_, _, x = g.hp.tick(x)
		x, _, _ = g.lp.tick(x)
		out[i] += x * (0.4 + p[6]*0.6)
	}
}

// ------------------------------------------------------------ SWAVE

type swSaw struct {
	ph  [5]float32
	sq  sqOsc
	s1  float32
	s2  float32
	d   drifter
	rnd rng
}

func (g *swSaw) trig(_, _ float32, p *[8]float32) {
	for i := range g.ph {
		g.ph[i] = g.rnd.next()*0.5 + 0.5
	}
}

func (g *swSaw) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[6], 1)/12) * g.d.next(p[7])
	det := p[0] * p[0] * 0.06
	mul := [5]float32{1, 1 + det, 1 - det, 1 + 2*det, 1 - 2*det}
	lv := [5]float32{1, p[1], p[1], p[2], p[2]}
	var inc [5]float32
	for k := range inc {
		inc[k] = f * mul[k] / c.sr
	}
	norm := 1 / (1 + 2*p[1] + 2*p[2] + p[3]*0.8 + p[4]*0.8 + p[5]*0.8)
	for i := range out {
		var y float32
		for k := 0; k < 5; k++ {
			g.ph[k] = wrap1(g.ph[k] + inc[k])
			y += sawBlep(g.ph[k], inc[k]) * lv[k]
		}
		if p[3] > 0.01 {
			y += g.sq.tick(inc[0]*0.5) * p[3] * 0.8
		}
		g.s1 = wrap1(g.s1 + inc[0]*0.5)
		g.s2 = wrap1(g.s2 + inc[0]*0.25)
		y += fsin(g.s1)*p[4]*0.8 + fsin(g.s2)*p[5]*0.8
		out[i] += y * norm * 0.7
	}
}

type swPulse struct {
	ph   [5]float32
	sq   sqOsc
	lfo  float32
	d    drifter
	rnd  rng
	init bool
}

func (g *swPulse) trig(_, _ float32, p *[8]float32) {
	for i := range g.ph {
		g.ph[i] = g.rnd.next()*0.5 + 0.5
	}
}

func (g *swPulse) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[6], 1)/12) * g.d.next(p[7])
	det := p[3] * p[3] * 0.05
	mul := [5]float32{1, 1 + det, 1 - det, 1 + 2*det, 1 - 2*det}
	var inc [5]float32
	for k := range inc {
		inc[k] = f * mul[k] / c.sr
	}
	rate := expMap(p[2], 0.1, 12) / c.sr
	for i := range out {
		g.lfo = wrap1(g.lfo + rate)
		var y float32
		for k := 0; k < 5; k++ {
			g.ph[k] = wrap1(g.ph[k] + inc[k])
			w := p[0]*0.8 + 0.1
			if k < 3 {
				w += p[1] * 0.35 * fsin(g.lfo+float32(k)*0.33)
			}
			w = clampf(w, 0.05, 0.95)
			lv := float32(1)
			if k >= 3 {
				lv = p[4]
			}
			y += pulseBlep(g.ph[k], inc[k], w) * lv
		}
		y += g.sq.tick(inc[0]*0.5) * p[5] * 0.8
		out[i] += y / (3 + 2*p[4] + p[5]*0.8) * 0.7
	}
}

var ensChords = [...][4]float32{
	{0, 0, 0, 0}, {0, 12, 0, 0}, {0, 7, 0, 0}, {0, 7, 12, 0}, {0, 4, 7, 0},
	{0, 3, 7, 0}, {0, 5, 7, 0}, {0, 4, 7, 11}, {0, 3, 7, 10}, {0, 4, 7, 12},
}
var ensCount = [...]int{1, 2, 2, 3, 3, 3, 3, 4, 4, 4}

type swEns struct {
	ph    [8]float32
	buf   [2048]float32
	w     int
	lfo   float32
	d     drifter
	rnd   rng
	cph   float32
	dummy bool
}

func (g *swEns) trig(_, _ float32, p *[8]float32) {
	for i := range g.ph {
		g.ph[i] = g.rnd.next()*0.5 + 0.5
	}
}

func (g *swEns) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[6], 1)/12) * g.d.next(p[7])
	ci := clampi(int(p[2]*10), 0, len(ensChords)-1)
	n := ensCount[ci]
	var inc [8]float32
	var amp float32 = 1 / float32(2*n)
	for k := 0; k < n; k++ {
		iv := ensChords[ci][k]
		if k < int(p[3]*4) { // inversion: raise the lowest notes an octave
			iv += 12
		}
		det := p[0] * 0.03 * (1 + p[1]*float32(k))
		base := f * pow2(iv/12)
		inc[2*k] = base * (1 + det) / c.sr
		inc[2*k+1] = base * (1 - det) / c.sr
	}
	crate := 0.6 / c.sr
	depth := (0.002 + p[5]*0.006) * c.sr
	for i := range out {
		var y float32
		for k := 0; k < 2*n; k++ {
			g.ph[k] = wrap1(g.ph[k] + inc[k])
			y += sawBlep(g.ph[k], inc[k])
		}
		y *= amp
		g.buf[g.w&2047] = y
		g.cph = wrap1(g.cph + crate)
		d := 0.008*c.sr + depth*fsin(g.cph)
		pos := float32(g.w) - d
		ip := int(math.Floor(float64(pos)))
		fr := pos - float32(ip)
		a, b := g.buf[ip&2047], g.buf[(ip+1)&2047]
		ch := a + (b-a)*fr
		g.w++
		out[i] += (y + ch*p[4]) / (1 + p[4]*0.7) * 0.8
	}
}

// ------------------------------------------------------------ SID

type sidGen struct {
	ph, mph, pwph, nph float32
	noise              float32
	lfsr               uint32
	d                  drifter
}

func (g *sidGen) trig(_, _ float32, p *[8]float32) {
	g.ph, g.mph = 0, 0
	if g.lfsr == 0 {
		g.lfsr = 0x7FFFF8
	}
}

func (g *sidGen) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[6], 1)/12) * g.d.next(p[7])
	inc := f / c.sr
	mode := clampi(int(p[3]*4), 0, 3)
	mf := f * pow2((semis(p[4], 24)+semis(p[5], 1))/12)
	minc := mf / c.sr
	wave := clampi(int(p[0]*5), 0, 4)
	pwr := expMap(p[2], 0.2, 8) / c.sr
	if g.lfsr == 0 {
		g.lfsr = 0x7FFFF8
	}
	for i := range out {
		g.ph += inc
		if g.ph >= 1 {
			g.ph--
		}
		g.mph += minc
		if g.mph >= 1 {
			g.mph--
			if mode >= 2 {
				g.ph = 0
			}
		}
		g.pwph = wrap1(g.pwph + pwr)
		pw := clampf(0.1+p[1]*0.8+p[2]*0.3*fsin(g.pwph), 0.04, 0.96)
		var y float32
		switch wave {
		case 0:
			y = 4*absf(g.ph-0.5) - 1
		case 1:
			y = 2*g.ph - 1
		case 2:
			y = -1
			if g.ph < pw {
				y = 1
			}
		case 3: // mixed: the lower of triangle and saw, as combined SID waves behave
			tri := 4*absf(g.ph-0.5) - 1
			saw := 2*g.ph - 1
			y = tri
			if saw < tri {
				y = saw
			}
			y = y*1.2 - 0.1
		default:
			g.nph += inc * 6
			if g.nph >= 1 {
				g.nph--
				b := ((g.lfsr >> 22) ^ (g.lfsr >> 17)) & 1
				g.lfsr = ((g.lfsr << 1) | b) & 0x7FFFFF
				g.noise = float32(int32(g.lfsr&0xFF)-128) / 128
			}
			y = g.noise
		}
		if mode == 1 || mode == 3 {
			y *= 4*absf(g.mph-0.5) - 1
		}
		out[i] += y * 0.6
	}
}

// ------------------------------------------------------------ VO

type vowel struct{ f [4]float32 }

var vowels = [8]vowel{
	{[4]float32{800, 1150, 2900, 3900}}, // a
	{[4]float32{400, 1700, 2600, 3500}}, // e
	{[4]float32{270, 2140, 2950, 3900}}, // i
	{[4]float32{450, 800, 2830, 3800}},  // o
	{[4]float32{325, 700, 2700, 3800}},  // u
	{[4]float32{660, 1720, 2410, 3500}}, // ae
	{[4]float32{500, 1500, 2500, 3500}}, // oe
	{[4]float32{300, 1650, 2400, 3500}}, // ue
}

type consonant struct {
	freq, q float32
	voiced  bool
	ms      float32
}

var consonants = [20]consonant{
	{6500, 2.5, false, 90}, {3500, 2, false, 90}, {4500, 1.2, false, 70}, {6000, 1, false, 70},
	{1500, 0.8, false, 50}, {2000, 1, false, 25}, {3500, 1.2, false, 20}, {800, 0.8, false, 20},
	{3200, 2, false, 60}, {6000, 2.5, true, 80}, {4500, 1.2, true, 70}, {600, 0.8, true, 30},
	{2500, 1, true, 30}, {1800, 1, true, 30}, {250, 3, true, 80}, {300, 3, true, 70},
	{400, 2, true, 70}, {500, 2.5, true, 60}, {350, 2, true, 60}, {2200, 2, true, 60},
}

type voGen struct {
	ph      float32
	form    [4]svf
	cons    svf
	r       rng
	consRem int
	consIdx int
	lp      float32
	d       drifter
}

func (g *voGen) trig(_, _ float32, p *[8]float32) {
	ci := int(p[3] * 20)
	if ci <= 0 {
		g.consRem = 0
		return
	}
	g.consIdx = clampi(ci-1, 0, 19)
	g.consRem = -1 // set on first render, when the sample rate is known
}

func (g *voGen) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * g.d.next(0.3)
	inc := f / c.sr
	v1 := clampf(p[0], 0, 1) * 7
	v2 := clampf(p[1], 0, 1) * 7
	morph := p[2]
	interp := func(v float32, k int) float32 {
		i := int(v)
		if i > 6 {
			i = 6
		}
		fr := v - float32(i)
		return lerp(vowels[i].f[k], vowels[i+1].f[k], fr)
	}
	gains := [4]float32{1, 0.55, 0.3, 0.12}
	for k := 0; k < 4; k++ {
		fc := lerp(interp(v1, k), interp(v2, k), morph)
		g.form[k].set(fc, 6+float32(k)*2, float64(c.sr))
	}
	cons := consonants[g.consIdx]
	if g.consRem < 0 {
		g.consRem = int(cons.ms * (0.3 + p[4]*1.7) * c.sr / 1000)
	}
	g.cons.set(cons.freq, cons.q, float64(c.sr))
	cvol := p[5] * 1.5
	sib := p[6]
	breath := p[7] * 0.4
	for i := range out {
		g.ph = wrap1(g.ph + inc)
		src := sawBlep(g.ph, inc)
		g.lp += (src - g.lp) * 0.35
		n := g.r.next()
		src = g.lp + n*breath*0.3
		vgain := float32(1)
		var cs float32
		if g.consRem > 0 {
			g.consRem--
			_, bp, hp := g.cons.tick(n)
			cs = bp*cvol + hp*sib*cvol*0.5
			if !cons.voiced {
				vgain = 0.15
			}
		}
		var y float32
		for k := 0; k < 4; k++ {
			_, bp, _ := g.form[k].tick(src)
			y += bp * gains[k]
		}
		out[i] += y*vgain*0.7 + cs*0.5
	}
}

// ------------------------------------------------------------ FM+

var fmRatios = [16]float32{0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3, 3.5, 4, 5, 6, 7, 8, 10, 12}

func fmRatio(p float32) float32 { return fmRatios[clampi(int(p*16), 0, 15)] }

type fmStat struct {
	p0, p1, p2, fb, lp float32
	d                  drifter
}

func (g *fmStat) trig(_, _ float32, p *[8]float32) { g.p0, g.p1, g.p2, g.fb = 0, 0, 0, 0 }

func (g *fmStat) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[6], 1)/12) * g.d.next(p[7])
	inc := f / c.sr
	i1, i2 := inc*fmRatio(p[0]), inc*fmRatio(p[2])
	l1, l2 := p[1]*3, p[3]*3
	fb := p[4] * 0.6
	a := lerp(0.04, 1, p[5]*p[5])
	for i := range out {
		g.p0 = wrap1(g.p0 + inc)
		g.p1 = wrap1(g.p1 + i1)
		g.p2 = wrap1(g.p2 + i2)
		m2 := fsin(g.p2 + fb*g.fb)
		g.fb = m2
		m1 := fsin(g.p1 + m2*l2*0.5)
		y := fsin(g.p0 + m1*l1*0.5)
		g.lp += (y - g.lp) * a
		out[i] += g.lp * 0.7
	}
}

type fmPar struct {
	p0, p1, p2, p3 float32
	t              float32
	d              drifter
}

func (g *fmPar) trig(_, _ float32, p *[8]float32) { g.t = 0; g.p0, g.p1, g.p2, g.p3 = 0, 0, 0, 0 }

func (g *fmPar) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * pow2(semis(p[7], 1)/12) * g.d.next(0)
	inc := f / c.sr
	r := [3]float32{inc * fmRatio(p[0]), inc * fmRatio(p[1]), inc * fmRatio(p[2])}
	lv := [3]float32{p[3] * 2.4, p[4] * 2.4, p[5] * 2.4}
	tau := expMap(p[6], 0.02, 2)
	dec := float32(math.Exp(-1 / float64(tau*c.sr)))
	env := float32(1)
	if p[6] < 0.01 {
		dec = 1
	}
	for i := range out {
		g.p0 = wrap1(g.p0 + inc)
		g.p1 = wrap1(g.p1 + r[0])
		g.p2 = wrap1(g.p2 + r[1])
		g.p3 = wrap1(g.p3 + r[2])
		e := g.t
		_ = e
		m := fsin(g.p1)*lv[0] + fsin(g.p2)*lv[1] + fsin(g.p3)*lv[2]
		out[i] += fsin(g.p0+m*0.25*(0.15+0.85*g.envNow(&env, dec))) * 0.7
	}
}

// envNow advances a decaying modulation envelope kept in g.t.
func (g *fmPar) envNow(env *float32, dec float32) float32 {
	if g.t == 0 {
		g.t = 1
	}
	g.t *= dec
	return g.t
}

type fmDyn struct {
	p0, p1, p2, fb float32
	t              float32 // samples since the trig
	d              drifter
}

func (g *fmDyn) trig(_, _ float32, p *[8]float32) { g.t = 0; g.p0, g.p1, g.p2, g.fb = 0, 0, 0, 0 }

func (g *fmDyn) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	f := hz * g.d.next(0)
	tau := expMap(p[6], 0.02, 3) * c.sr
	for i := range out {
		g.t++
		x := g.t / tau
		expE := float32(math.Exp(float64(-x)))
		linE := 1 - x/4
		if linE < 0 {
			linE = 0
		}
		env := lerp(expE, linE, p[7])
		fm := f * (1 + (pow2(p[5]*2*env)-1)*0.5)
		inc := f / c.sr
		g.p0 = wrap1(g.p0 + inc)
		g.p1 = wrap1(g.p1 + fm*fmRatio(p[0])/c.sr)
		g.p2 = wrap1(g.p2 + fm*fmRatio(p[2])/c.sr)
		m2 := fsin(g.p2 + p[4]*0.6*g.fb)
		g.fb = m2
		m1 := fsin(g.p1 + m2*p[3]*3*env*0.5)
		out[i] += fsin(g.p0+m1*p[1]*3*env*0.5) * 0.7
	}
}

// ------------------------------------------------------------ DigiPro

type dpWave struct {
	ph, sl, pm float32
	d          drifter
}

func (g *dpWave) trig(_, _ float32, p *[8]float32) { g.ph, g.sl = 0, 0 }

func (g *dpWave) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	n := NumFactoryWaves + NumUserWave
	wi := clampi(int(p[0]*float32(n)), 0, n-1)
	ta, tb := c.table(wi), c.table(clampi(wi+1, 0, n-1))
	morph := p[1]
	f := hz * pow2(semis(p[5], 1)/12) * g.d.next(p[7])
	inc := f / c.sr
	ratio := 1 + p[2]*3
	prate := expMap(p[4], 0.05, 200) / c.sr
	for i := range out {
		g.ph += inc
		if g.ph >= 1 {
			g.ph--
			if p[2] > 0.01 {
				g.sl = 0
			}
		}
		rp := g.ph
		if p[2] > 0.01 {
			g.sl += inc * ratio
			rp = g.sl
		}
		g.pm = wrap1(g.pm + prate)
		rp += p[3] * 0.5 * fsin(g.pm)
		a, b := tabRead(ta, rp), tabRead(tb, rp)
		out[i] += crush(lerp(a, b, morph), p[6]) * 0.7
	}
}

type dpBbox struct {
	idx, left int
	pos       float32
	start     float32
	rtrg      int
	rcnt      int
	rspace    int
	active    bool
}

func (g *dpBbox) trig(note, _ float32, p *[8]float32) {
	g.idx = ((int(note)-36)%24 + 24 + int(p[0]*23.99)) % 24
	g.start = p[2] * 0.9
	g.pos = g.start * float32(len(bboxSamples[g.idx]))
	if p[6] > 0.5 {
		g.pos = float32(len(bboxSamples[g.idx])-1) * (1 - g.start)
	}
	g.rtrg = int(p[3] * 8)
	g.rcnt = 0
	g.rspace = -1
	g.active = true
}

func (g *dpBbox) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	if !g.active {
		return
	}
	s := bboxSamples[g.idx]
	rate := pow2(semis(p[1], 12)/12) * bboxRate / c.sr
	if p[6] > 0.5 {
		rate = -rate
	}
	if g.rspace < 0 {
		g.rspace = int(expMap(p[4], 0.01, 0.25) * c.sr)
		g.rcnt = g.rspace
	}
	lim := float32(len(s)) * lerp(0.15, 1, p[7])
	for i := range out {
		if g.rtrg > 0 {
			g.rcnt--
			if g.rcnt <= 0 {
				g.rcnt = g.rspace
				g.rtrg--
				g.pos = g.start * float32(len(s))
				if rate < 0 {
					g.pos = float32(len(s)-1) * (1 - g.start)
				}
			}
		}
		if g.pos < 0 || g.pos >= float32(len(s)-1) || (rate > 0 && g.pos > lim) {
			if g.rtrg == 0 {
				g.active = false
				return
			}
			continue
		}
		ip := int(g.pos)
		fr := g.pos - float32(ip)
		y := s[ip] + (s[ip+1]-s[ip])*fr
		g.pos += rate
		fade := float32(1)
		if rem := lim - g.pos; rate > 0 && rem < 200 {
			fade = clampf(rem/200, 0, 1)
		}
		out[i] += crush(y, p[5]) * 0.9 * fade
	}
}

type dpDdrw struct {
	a, b, sl, scan float32
	d              drifter
}

func (g *dpDdrw) trig(_, _ float32, p *[8]float32) { g.a, g.b, g.sl = 0, p[4], 0 }

func (g *dpDdrw) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	n := NumFactoryWaves + NumUserWave
	ta := c.table(clampi(int(p[0]*float32(n)), 0, n-1))
	tb := c.table(clampi(int(p[1]*float32(n)), 0, n-1))
	f := hz * g.d.next(0)
	inc := f / c.sr
	incB := inc * pow2(semis(p[6], 1)/12)
	srate := expMap(p[3], 0.2, 20) / c.sr
	for i := range out {
		g.a = wrap1(g.a + inc)
		g.b = wrap1(g.b + incB)
		mix := p[2]
		if p[3] > 0.01 {
			g.scan = wrap1(g.scan + srate)
			mix = clampf(mix+0.5*p[3]*fsin(g.scan), 0, 1)
		}
		rb := g.b
		if p[5] > 0.01 {
			rb = wrap1(g.a * (1 + p[5]*3))
		}
		out[i] += crush(lerp(tabRead(ta, g.a), tabRead(tb, rb), mix), p[7]) * 0.7
	}
}

type dpDens struct {
	ph  [4]float32
	buf [2048]float32
	w   int
	cph float32
	d   drifter
	r   rng
}

func (g *dpDens) trig(_, _ float32, p *[8]float32) {
	for i := range g.ph {
		g.ph[i] = g.r.next()*0.5 + 0.5
	}
}

func (g *dpDens) render(out []float32, hz float32, c *genCtx, p *[8]float32) {
	n := NumFactoryWaves + NumUserWave
	t := c.table(clampi(int(p[0]*float32(n)), 0, n-1))
	f := hz * g.d.next(p[7])
	off := [4]float32{0, float32(math.Round(float64(semis(p[1], 24)))), float32(math.Round(float64(semis(p[2], 24)))), float32(math.Round(float64(semis(p[3], 24))))}
	var inc [4]float32
	for k := range inc {
		inc[k] = f * pow2(off[k]/12) * (1 + float32(k)*0.002*p[6]*10) / c.sr
	}
	depth := (0.002 + p[5]*0.006) * c.sr
	for i := range out {
		var y float32
		for k := 0; k < 4; k++ {
			g.ph[k] = wrap1(g.ph[k] + inc[k])
			y += tabRead(t, g.ph[k])
		}
		y *= 0.25
		g.buf[g.w&2047] = y
		g.cph = wrap1(g.cph + 0.5/c.sr)
		d := 0.008*c.sr + depth*fsin(g.cph)
		pos := float32(g.w) - d
		ip := int(math.Floor(float64(pos)))
		fr := pos - float32(ip)
		a, b := g.buf[ip&2047], g.buf[(ip+1)&2047]
		g.w++
		out[i] += (y + (a+(b-a)*fr)*p[4]) / (1 + p[4]*0.7) * 0.9
	}
}
