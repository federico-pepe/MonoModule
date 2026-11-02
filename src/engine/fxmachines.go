package engine

import "math"

// The FX machines: effects that sit on a track and process the audio of the
// tracks routed to them (see TrackSetup.Src). Original DSP.

type comb struct {
	buf   []float32
	i     int
	store float32
}

func (c *comb) tick(x, fb, damp float32) float32 {
	y := c.buf[c.i]
	c.store = y*(1-damp) + c.store*damp
	c.buf[c.i] = x + c.store*fb
	c.i++
	if c.i == len(c.buf) {
		c.i = 0
	}
	return y
}

type allpass struct {
	buf []float32
	i   int
}

func (a *allpass) tick(x float32) float32 {
	b := a.buf[a.i]
	y := -x + b
	a.buf[a.i] = x + b*0.5
	a.i++
	if a.i == len(a.buf) {
		a.i = 0
	}
	return y
}

// freeverb constants (Jezar, public domain), for 44.1 kHz.
var (
	combTuning    = [8]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	allpassTuning = [4]int{556, 441, 341, 225}
	stereoSpread  = 23
)

// fxState holds the memory of every FX machine on one track.
type fxState struct {
	sr float64
	// reverb
	combL, combR [8]comb
	apL, apR     [4]allpass
	pre          *delayLine
	gate, gateT  float32
	revHP, revLP [2]onePoleLP
	// chorus / flanger
	line [2]*delayLine
	ph   [6]float32
	// phaser
	apz [2][12]float32
	fbz [2]float32
	// ring modulator
	rph, rlfo float32
	// dynamix
	env  float32
	scHP onePoleLP
}

func newFXState(sr float64) *fxState {
	f := &fxState{sr: sr}
	scale := sr / 44100
	for i := range f.combL {
		f.combL[i].buf = make([]float32, int(float64(combTuning[i])*scale))
		f.combR[i].buf = make([]float32, int(float64(combTuning[i]+stereoSpread)*scale))
	}
	for i := range f.apL {
		f.apL[i].buf = make([]float32, int(float64(allpassTuning[i])*scale))
		f.apR[i].buf = make([]float32, int(float64(allpassTuning[i]+stereoSpread)*scale))
	}
	f.pre = newDelayLine(int(sr*0.13) + 8)
	f.line[0] = newDelayLine(int(sr*0.06) + 8)
	f.line[1] = newDelayLine(int(sr*0.06) + 8)
	return f
}

func (f *fxState) reset() {
	for i := range f.combL {
		for j := range f.combL[i].buf {
			f.combL[i].buf[j] = 0
		}
		for j := range f.combR[i].buf {
			f.combR[i].buf[j] = 0
		}
		f.combL[i].store, f.combR[i].store = 0, 0
	}
	for i := range f.apL {
		for j := range f.apL[i].buf {
			f.apL[i].buf[j] = 0
		}
		for j := range f.apR[i].buf {
			f.apR[i].buf[j] = 0
		}
	}
	f.pre.clear()
	f.line[0].clear()
	f.line[1].clear()
	f.apz = [2][12]float32{}
	f.fbz = [2]float32{}
	f.env = 0
	f.gate = 0
}

// process runs FX machine kind (a Machines index) over n samples. in and out
// may be the same slices.
func (f *fxState) process(kind int, inL, inR, outL, outR []float32, n int, p *[8]float32, bpm float64) {
	switch Machines[kind].Name {
	case "FX-REVERB":
		f.reverb(inL, inR, outL, outR, n, p)
	case "FX-CHORUS":
		f.chorus(inL, inR, outL, outR, n, p)
	case "FX-FLANGER":
		f.flanger(inL, inR, outL, outR, n, p)
	case "FX-PHASER":
		f.phaser(inL, inR, outL, outR, n, p)
	case "FX-RINGMOD":
		f.ring(inL, inR, outL, outR, n, p)
	case "FX-DYNAMIX":
		f.dynamix(inL, inR, outL, outR, n, p)
	default: // FX-THRU
		g := p[0] * 2
		bl, br := float32(1), float32(1)
		if p[1] > 0.5 {
			bl = 1 - (p[1]-0.5)*2
		} else {
			br = p[1] * 2
		}
		wid := p[3] * 2
		for i := 0; i < n; i++ {
			l, r := inL[i], inR[i]
			m, s := (l+r)*0.5, (l-r)*0.5*wid
			l, r = (m+s)*g*bl, (m-s)*g*br
			if p[2] > 0.01 {
				l, r = drive(l, p[2]), drive(r, p[2])
			}
			outL[i], outR[i] = l, r
		}
	}
}

func (f *fxState) reverb(inL, inR, outL, outR []float32, n int, p *[8]float32) {
	room := 0.7 + p[0]*0.28 + (p[7]-0.5)*0.02
	damp := p[1] * 0.5
	pre := float32(f.sr) * (0.002 + p[5]*0.12)
	hpA := onePoleCoef(expMap(p[3], 20, 2000), f.sr)
	lpA := onePoleCoef(expMap(p[4], 500, 18000), f.sr)
	wet := p[6]
	gateSens := p[2]
	for i := 0; i < n; i++ {
		in := (inL[i] + inR[i]) * 0.5
		f.pre.write(in)
		rin := f.pre.read(pre) * 0.015
		var rl, rr float32
		for c := 0; c < 8; c++ {
			rl += f.combL[c].tick(rin, room, damp)
			rr += f.combR[c].tick(rin, room, damp)
		}
		for a := 0; a < 4; a++ {
			rl = f.apL[a].tick(rl)
			rr = f.apR[a].tick(rr)
		}
		rl *= 3
		rr *= 3
		// filters on the wet signal
		rl -= f.revHP[0].tick(rl, hpA)
		rr -= f.revHP[1].tick(rr, hpA)
		rl = f.revLP[0].tick(rl, lpA)
		rr = f.revLP[1].tick(rr, lpA)
		if gateSens > 0.01 { // gated space: the tail is cut when the input stops
			a := absf(in)
			if a > f.gateT {
				f.gateT = a
			} else {
				f.gateT += (a - f.gateT) * 0.0003
			}
			target := float32(1)
			if f.gateT < gateSens*0.1 {
				target = 0
			}
			f.gate += (target - f.gate) * 0.002
			rl *= f.gate
			rr *= f.gate
		}
		outL[i] = inL[i]*(1-wet) + rl*wet
		outR[i] = inR[i]*(1-wet) + rr*wet
	}
}

func (f *fxState) chorus(inL, inR, outL, outR []float32, n int, p *[8]float32) {
	depth := float32(f.sr) * 0.006 * p[0]
	base := float32(f.sr) * (0.005 + 0.02*p[2])
	rate := expMap(p[1], 0.1, 5) / float32(f.sr)
	fb := p[4] * 0.6
	mix := p[5]
	for i := 0; i < n; i++ {
		f.ph[0] = wrap1(f.ph[0] + rate)
		in := (inL[i] + inR[i]) * 0.5
		var wl, wr float32
		for k := 0; k < 3; k++ {
			pl := f.ph[0] + float32(k)/3
			pr := f.ph[0] + float32(k)/3 + 0.5*p[3]
			wl += f.line[0].read(base + depth*fsin(pl))
			wr += f.line[1].read(base + depth*fsin(pr))
		}
		wl, wr = wl/3, wr/3
		f.line[0].write(in + wl*fb)
		f.line[1].write(in + wr*fb)
		outL[i] = inL[i]*(1-mix*0.5) + wl*mix*0.8
		outR[i] = inR[i]*(1-mix*0.5) + wr*mix*0.8
	}
}

func (f *fxState) flanger(inL, inR, outL, outR []float32, n int, p *[8]float32) {
	depth := float32(f.sr) * 0.004 * p[0]
	base := float32(f.sr) * (0.0008 + 0.005*p[2])
	rate := expMap(p[1], 0.05, 4) / float32(f.sr)
	fb := p[3] * 0.85
	if p[6] > 0.5 {
		fb = -fb
	}
	mix := p[4]
	hpA := onePoleCoef(expMap(p[7], 20, 3000), f.sr)
	var hp [2]float32
	for i := 0; i < n; i++ {
		f.ph[1] = wrap1(f.ph[1] + rate)
		dl := base + depth*(0.5+0.5*fsin(f.ph[1]))
		dr := base + depth*(0.5+0.5*fsin(f.ph[1]+0.5*p[5]))
		wl := f.line[0].read(dl)
		wr := f.line[1].read(dr)
		hp[0] += (wl - hp[0]) * hpA
		hp[1] += (wr - hp[1]) * hpA
		f.line[0].write(inL[i] + (wl-hp[0])*fb)
		f.line[1].write(inR[i] + (wr-hp[1])*fb)
		outL[i] = inL[i]*(1-mix*0.5) + wl*mix
		outR[i] = inR[i]*(1-mix*0.5) + wr*mix
	}
}

func (f *fxState) phaser(inL, inR, outL, outR []float32, n int, p *[8]float32) {
	stages := 2 * (2 + clampi(int(p[3]*5), 0, 4)) // 4..12
	rate := expMap(p[1], 0.03, 6) / float32(f.sr)
	fb := p[2] * 0.85
	mix := p[6]
	for i := 0; i < n; i++ {
		f.ph[2] = wrap1(f.ph[2] + rate)
		for ch := 0; ch < 2; ch++ {
			in := inL[i]
			if ch == 1 {
				in = inR[i]
			}
			lfo := fsin(f.ph[2] + float32(ch)*0.5*p[5])
			fc := 200 * pow2(p[4]*5+p[0]*lfo*2.5)
			if fc > float32(f.sr)*0.4 {
				fc = float32(f.sr) * 0.4
			}
			tn := float32(math.Tan(math.Pi * float64(fc) / f.sr))
			a := (tn - 1) / (tn + 1)
			x := in + f.fbz[ch]*fb
			for s := 0; s < stages; s++ {
				y := a*x + f.apz[ch][s]
				f.apz[ch][s] = x - a*y
				x = y
			}
			f.fbz[ch] = x
			o := in*(1-mix*0.5) - x*mix*0.9
			wid := p[7]
			if ch == 0 {
				outL[i] = o
			} else {
				outR[i] = o
			}
			_ = wid
		}
		m := (outL[i] + outR[i]) * 0.5
		w := p[7]
		outL[i] = m + (outL[i]-m)*w*1.3
		outR[i] = m + (outR[i]-m)*w*1.3
	}
}

func (f *fxState) ring(inL, inR, outL, outR []float32, n int, p *[8]float32) {
	fc := expMap(p[0], 20, 4000) * pow2(semis(p[1], 1)/12)
	fmd := p[4] * 0.6
	fmr := expMap(p[5], 0.1, 30) / float32(f.sr)
	mix := p[3]
	shape := clampi(int(p[2]*3), 0, 2)
	hpA := onePoleCoef(expMap(p[6], 20, 3000), f.sr)
	lpA := onePoleCoef(expMap(p[7], 500, 18000), f.sr)
	var st [4]float32
	for i := 0; i < n; i++ {
		f.rlfo = wrap1(f.rlfo + fmr)
		f.rph = wrap1(f.rph + fc*(1+fmd*fsin(f.rlfo))/float32(f.sr))
		var c float32
		switch shape {
		case 0:
			c = fsin(f.rph)
		case 1:
			c = 4*absf(f.rph-0.5) - 1
		default:
			c = 1
			if f.rph >= 0.5 {
				c = -1
			}
		}
		l, r := inL[i]*c, inR[i]*c
		st[0] += (l - st[0]) * hpA
		st[1] += (r - st[1]) * hpA
		l, r = l-st[0], r-st[1]
		st[2] += (l - st[2]) * lpA
		st[3] += (r - st[3]) * lpA
		outL[i] = inL[i]*(1-mix) + st[2]*mix
		outR[i] = inR[i]*(1-mix) + st[3]*mix
	}
}

func (f *fxState) dynamix(inL, inR, outL, outR []float32, n int, p *[8]float32) {
	thr := -(1 - p[0]) * 40
	ratio := 1 + p[1]*p[1]*19
	atk := float32(1 - math.Exp(-1/(float64(expMap(p[2], 0.001, 0.05))*f.sr)))
	rel := float32(1 - math.Exp(-1/(float64(expMap(p[3], 0.03, 1))*f.sr)))
	mk := dbToLin(p[4] * 12)
	mix := p[5]
	hpA := onePoleCoef(expMap(p[7], 20, 1500), f.sr)
	for i := 0; i < n; i++ {
		l, r := inL[i], inR[i]
		det := (absf(l) + absf(r)) * 0.5
		if p[7] > 0.01 {
			f.scHP.y += (det - f.scHP.y) * hpA
			det = absf(det - f.scHP.y)
		}
		if det > f.env {
			f.env += (det - f.env) * atk
		} else {
			f.env += (det - f.env) * rel
		}
		g := float32(1)
		if f.env > 1e-5 {
			db := 20 * float32(math.Log10(float64(f.env)))
			if over := db - thr; over > 0 {
				g = dbToLin(-over * (1 - 1/ratio))
			}
		}
		g *= mk
		wl, wr := l*g, r*g
		if p[6] > 0.01 {
			wl, wr = limiter(wl*(1+p[6])), limiter(wr*(1+p[6]))
		}
		outL[i] = l*(1-mix) + wl*mix
		outR[i] = r*(1-mix) + wr*mix
	}
}
