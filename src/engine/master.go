package engine

import "math"

// limiter is a soft knee at 0.7: linear below, saturating to 1 above.
func limiter(x float32) float32 {
	a := absf(x)
	if a < 0.7 {
		return x
	}
	y := 0.7 + 0.3*softClip((a-0.7)/0.3)
	if x < 0 {
		return -y
	}
	return y
}

// masterFX is the strip on the output: compressor, tilt EQ, volume, limiter.
// The Monomachine has none of this; it keeps levels sane on the Push.
type masterFX struct {
	sr     float64
	env    float32
	lo, hi [2]biquad
	loOn   bool
	hiOn   bool
}

func newMasterFX(sr float64) *masterFX { return &masterFX{sr: sr} }

func (g *masterFX) reset() {
	g.env = 0
	for i := 0; i < 2; i++ {
		g.lo[i].reset()
		g.hi[i].reset()
	}
}

// withState copies the coefficients of b into a fresh biquad that keeps the
// filter memory of old (so the right channel keeps its own state).
func (b biquad) withState(old biquad) biquad {
	b.z1, b.z2 = old.z1, old.z2
	return b
}

// process works in place on L and R.
func (g *masterFX) process(mp *[NMaster]uint8, n int, L, R []float32) {
	f := func(i int) float32 { return float32(mp[i]) / 127 }
	thrDB := -(1 - f(MCTHR)) * 40
	ratio := 1 + f(MCRAT)*f(MCRAT)*19
	atkC := float32(1 - math.Exp(-1/(0.004*g.sr)))
	relC := float32(1 - math.Exp(-1/(float64(expMap(f(MCREL), 0.03, 1.0))*g.sr)))
	makeup := dbToLin(f(MCMKP) * 12)
	cmix := f(MCMIX)
	loG := (f(MELG) - 0.5) * 24
	hiG := (f(MEHG) - 0.5) * 24
	g.loOn, g.hiOn = absf(loG) > 0.2, absf(hiG) > 0.2
	if g.loOn {
		g.lo[0].lowShelf(120, loG, g.sr)
		g.lo[1] = g.lo[0].withState(g.lo[1])
	}
	if g.hiOn {
		g.hi[0].highShelf(8000, hiG, g.sr)
		g.hi[1] = g.hi[0].withState(g.hi[1])
	}
	mv := f(MVOL)
	mgain := mv * mv * 1.4
	for i := 0; i < n; i++ {
		l, r := L[i], R[i]
		if cmix > 0.002 {
			det := absf(l)
			if a := absf(r); a > det {
				det = a
			}
			if det > g.env {
				g.env += (det - g.env) * atkC
			} else {
				g.env += (det - g.env) * relC
			}
			if g.env > 1e-5 {
				envDB := 20 * float32(math.Log10(float64(g.env)))
				gain := float32(1)
				if over := envDB - thrDB; over > 0 {
					gain = dbToLin(-over * (1 - 1/ratio))
				}
				gain *= makeup
				l += (l*gain - l) * cmix
				r += (r*gain - r) * cmix
			}
		}
		if g.loOn {
			l = g.lo[0].tick(l)
			r = g.lo[1].tick(r)
		}
		if g.hiOn {
			l = g.hi[0].tick(l)
			r = g.hi[1].tick(r)
		}
		l, r = limiter(l*mgain), limiter(r*mgain)
		if isBad(l) || isBad(r) {
			l, r = 0, 0
			g.reset()
		}
		L[i], R[i] = l, r
	}
}

// trackDelay is the per-track delay: one tap, tempo synced in 1/256 notes,
// with feedback, a high-pass and a low-pass in the loop. A negative send
// keeps the stereo image; a positive one ping-pongs.
type trackDelay struct {
	sr       float64
	l, r     *delayLine
	lp, hp   [2]onePoleLP
	tail     int
	smoothed float32
}

func newTrackDelay(sr float64) *trackDelay {
	return &trackDelay{sr: sr, l: newDelayLine(int(sr*3.4) + 8), r: newDelayLine(int(sr*3.4) + 8)}
}

func (d *trackDelay) clear() {
	d.l.clear()
	d.r.clear()
	for i := range d.lp {
		d.lp[i].y, d.hp[i].y = 0, 0
	}
}

// process adds the delay of (inL, inR) to outL/outR in place. p is the
// track's FX page (normalised).
func (d *trackDelay) process(L, R []float32, n int, p []float32, bpm float64) {
	send := (p[DSND-PerPage*PageFx] - 0.5) * 2
	amt := absf(send)
	if amt < 0.004 {
		if d.tail > 0 {
			d.tail -= n
			if d.tail <= 0 {
				d.clear()
			}
		}
		if d.tail <= 0 {
			return
		}
	} else {
		d.tail = int(d.sr * 4)
	}
	ping := send > 0
	units := p[DTIM-PerPage*PageFx]*127 + 1
	target := float32(float64(units) * 60 / bpm / 64 * d.sr)
	maxT := float32(len(d.l.buf) - 4)
	if target > maxT {
		target = maxT
	}
	fb := p[DFB-PerPage*PageFx] * 0.95
	base := p[DBAS-PerPage*PageFx]
	hpA := onePoleCoef(expMap(base, 20, 8000), d.sr)
	lpX := clampf(base+p[DWID-PerPage*PageFx], 0, 1)
	lpA := onePoleCoef(expMap(lpX, 200, 20000), d.sr)
	for i := 0; i < n; i++ {
		d.smoothed += (target - d.smoothed) * 0.0008
		l := d.l.read(d.smoothed)
		r := d.r.read(d.smoothed)
		fl := d.lp[0].tick(l, lpA)
		fr := d.lp[1].tick(r, lpA)
		fl -= d.hp[0].tick(fl, hpA)
		fr -= d.hp[1].tick(fr, hpA)
		if ping {
			m := (L[i] + R[i]) * 0.5 * amt
			d.l.write(m + fr*fb)
			d.r.write(fl * fb)
		} else {
			d.l.write(L[i]*amt + fl*fb)
			d.r.write(R[i]*amt + fr*fb)
		}
		L[i] += l
		R[i] += r
	}
}
