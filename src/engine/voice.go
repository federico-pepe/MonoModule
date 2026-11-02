package engine

import "math"

// subBlock is the control rate: envelopes' coefficients, LFOs and filter
// cutoffs are updated every subBlock samples.
const subBlock = 32

// gens holds one of every generator, so switching machine never allocates.
type gens struct {
	sin   gndSin
	nois  gndNois
	saw   swSaw
	pulse swPulse
	ens   swEns
	sid   sidGen
	vo    voGen
	fmS   fmStat
	fmP   fmPar
	fmD   fmDyn
	dpW   dpWave
	bbox  dpBbox
	ddrw  dpDdrw
	dens  dpDens
}

const (
	stIdle = iota
	stAtk
	stHold
	stDec
	stRel
)

// lfoState is one of the three LFOs of a voice.
type lfoState struct {
	phase, hold, rndv float32
	cycles            int
	done              bool
	r                 rng
}

// voice is one of the six hardware voices: a generator, the AHDR amplifier,
// the two-filter section with its envelope, and three LFOs. In normal use
// voice i plays track i; in POLY mode all six play the poly track.
type voice struct {
	idx   int
	owner int // track whose setup this voice follows
	kind  int // machine index of the owner
	g     gens

	note, curNote, vel float32
	pitchMod           float32
	hasNote            bool
	active             bool
	age                uint64
	offLeft            int // samples until the automatic note off; <=0 none
	noteID             int // the note a live key started, for NoteOff matching; -1 none

	stage    int
	lvl      float32
	holdLeft int
	fstage   int
	fenv     float32
	hp1, hp2 svf
	lp1, lp2 svf
	lfo      [3]lfoState
	eff      [NParams]float32 // parameters after the LFOs, last sub-block
	buf      [subBlock]float32
	pb       [PerPage]float32
	peak     float32
}

func newVoice(idx int) *voice {
	v := &voice{idx: idx, owner: idx, noteID: -1}
	for i := range v.lfo {
		v.lfo[i].r.seed(uint32(idx*31+i*7) + 1)
	}
	return v
}

// setOwner makes the voice follow track owner, whose machine is kind.
func (v *voice) setOwner(owner, kind int) {
	if owner == v.owner && kind == v.kind {
		return
	}
	v.owner = owner
	if kind != v.kind {
		v.kind = kind
		v.g = gens{}
		v.silence()
	}
}

// silence stops the sound at once.
func (v *voice) silence() {
	v.active, v.stage, v.lvl, v.fstage, v.fenv, v.offLeft, v.noteID = false, stIdle, 0, 0, 0, 0, -1
	v.hp1.reset()
	v.hp2.reset()
	v.lp1.reset()
	v.lp2.reset()
}

func (v *voice) isFX() bool { return v.kind >= mFxThru }

// genTrig and genRender dispatch on the machine.
func (v *voice) genTrig(note, vel float32, p *[PerPage]float32) {
	switch v.kind {
	case mGndSin:
		v.g.sin.trig(note, vel, p)
	case mGndNois:
		v.g.nois.trig(note, vel, p)
	case mSwSaw:
		v.g.saw.trig(note, vel, p)
	case mSwPulse:
		v.g.pulse.trig(note, vel, p)
	case mSwEns:
		v.g.ens.trig(note, vel, p)
	case mSid:
		v.g.sid.trig(note, vel, p)
	case mVo:
		v.g.vo.trig(note, vel, p)
	case mFmStat:
		v.g.fmS.trig(note, vel, p)
	case mFmPar:
		v.g.fmP.trig(note, vel, p)
	case mFmDyn:
		v.g.fmD.trig(note, vel, p)
	case mDpWave:
		v.g.dpW.trig(note, vel, p)
	case mDpBbox:
		v.g.bbox.trig(note, vel, p)
	case mDpDdrw:
		v.g.ddrw.trig(note, vel, p)
	case mDpDens:
		v.g.dens.trig(note, vel, p)
	}
}

func (v *voice) genRender(out []float32, hz float32, c *genCtx, p *[PerPage]float32) {
	switch v.kind {
	case mGndSin:
		v.g.sin.render(out, hz, c, p)
	case mGndNois:
		v.g.nois.render(out, hz, c, p)
	case mSwSaw:
		v.g.saw.render(out, hz, c, p)
	case mSwPulse:
		v.g.pulse.render(out, hz, c, p)
	case mSwEns:
		v.g.ens.render(out, hz, c, p)
	case mSid:
		v.g.sid.render(out, hz, c, p)
	case mVo:
		v.g.vo.render(out, hz, c, p)
	case mFmStat:
		v.g.fmS.render(out, hz, c, p)
	case mFmPar:
		v.g.fmP.render(out, hz, c, p)
	case mFmDyn:
		v.g.fmD.render(out, hz, c, p)
	case mDpWave:
		v.g.dpW.render(out, hz, c, p)
	case mDpBbox:
		v.g.bbox.render(out, hz, c, p)
	case mDpDdrw:
		v.g.ddrw.render(out, hz, c, p)
	case mDpDens:
		v.g.dens.render(out, hz, c, p)
	}
}

// noteOn starts or changes the voice's note. mask says which parts restart
// (a mask of 0 is a "trigless" trig: only the pitch moves). offSamples > 0
// schedules the note off; 0 or less holds until noteOff.
func (v *voice) noteOn(note, vel float32, mask uint8, offSamples int, id int, age uint64) {
	v.note = note
	v.vel = vel
	v.age = age
	v.noteID = id
	if !v.hasNote || !v.active && mask&TrigAmp != 0 && v.eff[PORT] < 0.005 {
		v.curNote = note
	}
	if v.eff[PORT] < 0.005 {
		v.curNote = note
	}
	v.hasNote = true
	if mask&TrigAmp != 0 {
		if !v.isFX() {
			for i := range v.pb {
				v.pb[i] = v.eff[i]
			}
			v.genTrig(note, vel, &v.pb)
		}
		v.stage = stAtk
		v.active = !v.isFX()
		v.offLeft = offSamples
	} else if offSamples > 0 && v.active {
		v.offLeft = offSamples
	}
	if mask&TrigFlt != 0 {
		v.fstage, v.fenv = 1, 0
	}
	if mask&TrigLFO != 0 {
		for i := range v.lfo {
			v.lfoTrig(i)
		}
	}
}

func (v *voice) noteOff() {
	if v.active && v.stage != stIdle && v.stage != stRel {
		v.stage = stRel
	}
	v.offLeft = 0
	v.noteID = -1
}

// lfoTrig restarts LFO i according to its TRIG mode.
func (v *voice) lfoTrig(i int) {
	l := &v.lfo[i]
	mode := lfoTrigMode(v.eff[LFOBase(i)+LTRIG])
	if mode == 0 { // FREE never restarts
		return
	}
	if mode == 2 { // HOLD latches the running wave
		l.hold = lfoWave(clampi(int(v.eff[LFOBase(i)+LWAVE]*11), 0, 10), l.phase, l)
		return
	}
	l.phase, l.cycles, l.done = 0, 0, false
	l.rndv = l.r.next()
}

func lfoTrigMode(p float32) int { return clampi(int(p*5), 0, 4) }

func lfoWave(w int, ph float32, l *lfoState) float32 {
	mirror := false
	if w >= 6 {
		w -= 6
		mirror = true
	}
	var y float32
	switch w {
	case 0:
		if ph < 0.5 {
			y = ph*4 - 1
		} else {
			y = 3 - ph*4
		}
	case 1:
		y = fsin(ph)
	case 2:
		y = 1
		if ph >= 0.5 {
			y = -1
		}
	case 3:
		y = 2*ph - 1
	case 4:
		y = 2*float32(math.Exp(float64(-5*ph))) - 1
	default:
		return l.rndv
	}
	if mirror {
		return -y
	}
	return y
}

// pitchRanges are the PTCH destination ranges of an LFO, in semitones.
var pitchRanges = [8]float32{1, 2, 3, 5, 7, 12, 24, 48}

// resolve computes eff for one sub-block: the track's parameters plus the
// three LFOs. m is the sub-block length.
func (v *voice) resolve(base *[NParams]float32, m int, sr, bpm float32) {
	v.eff = *base
	v.pitchMod = 0
	for n := 0; n < 3; n++ {
		b := LFOBase(n)
		l := &v.lfo[n]
		p := v.eff[b : b+PerPage]
		depth := (p[LDPTH] - 0.5) * 2
		wave := clampi(int(p[LWAVE]*11), 0, 10)
		mode := lfoTrigMode(p[LTRIG])
		cyc := (p[LSPD]*127 + 1) / 128 * 4 * pow2(p[LMULT]*7)
		inc := cyc * bpm / 60 * float32(m) / sr
		running := true
		switch mode {
		case 3:
			running = !l.done
		case 4:
			running = !l.done
		}
		if running {
			l.phase += inc
			for l.phase >= 1 {
				l.phase--
				l.cycles++
				l.rndv = l.r.next()
				if mode == 3 {
					l.phase, l.done = 1, true
					break
				}
			}
			if mode == 4 && l.phase >= 0.5 {
				l.phase, l.done = 0.5, true
			}
		}
		if depth > -0.008 && depth < 0.008 {
			continue
		}
		var y float32
		if mode == 2 {
			y = l.hold
		} else {
			ph := l.phase
			if ph >= 1 {
				ph = 0.9999
			}
			y = lfoWave(wave, ph, l)
		}
		if p[LINTL] >= 0.5 && l.cycles&1 == 1 {
			y = 0
		}
		page := clampi(int(p[LPAGE]*8), 0, 7)
		dest := clampi(int(p[LDEST]*8), 0, 7)
		if page == 7 {
			v.pitchMod += y * depth * pitchRanges[dest]
			continue
		}
		if page == PageLFO1+n { // an LFO never modulates its own page
			continue
		}
		v.eff[page*PerPage+dest] += y * depth
	}
	for i := range v.eff {
		v.eff[i] = clampf(v.eff[i], 0, 1)
	}
}

// ampTimes returns the envelope times in seconds from the AMP page.
func ampAtk(p float32) float32 {
	if p < 0.004 {
		return 0
	}
	return expMap(p, 0.002, 3)
}

// render makes n samples (n <= any size) for the voice and adds them,
// panned, into busL/busR. base is the owner track's resolved parameters.
func (v *voice) render(n int, base *[NParams]float32, ctx *genCtx, keyLP, keyHP bool, bend float32, busL, busR []float32) {
	sr := ctx.sr
	for off := 0; off < n; off += subBlock {
		m := n - off
		if m > subBlock {
			m = subBlock
		}
		v.resolve(base, m, sr, ctx.bpm)
		e := &v.eff
		if v.isFX() || !v.active {
			continue
		}
		if v.offLeft > 0 {
			v.offLeft -= m
			if v.offLeft <= 0 {
				v.offLeft = 0
				v.noteOff()
			}
		}
		// pitch with portamento
		if e[PORT] < 0.005 {
			v.curNote = v.note
		} else {
			tau := expMap(e[PORT], 0.005, 1.5)
			v.curNote += (v.note - v.curNote) * (1 - float32(math.Exp(float64(-float32(m)/(sr*tau)))))
		}
		semi := v.curNote + v.pitchMod + bend
		hz := 440 * pow2((semi-69)/12)
		if hz > sr*0.45 {
			hz = sr * 0.45
		}
		for i := 0; i < PerPage; i++ {
			v.pb[i] = e[i]
		}
		buf := v.buf[:m]
		for i := range buf {
			buf[i] = 0
		}
		v.genRender(buf, hz, ctx, &v.pb)

		// envelopes' coefficients
		atkSec := ampAtk(e[ATK])
		atkInc := float32(1)
		if atkSec > 0 {
			atkInc = 1 / (atkSec * sr)
		}
		holdInf := e[HOLD] > 0.992
		holdN := int(expMap(e[HOLD], 0.002, 4) * sr)
		decC := decayCoef(float64(sr), expMap(e[DEC], 0.01, 6))
		relC := decayCoef(float64(sr), expMap(e[REL], 0.005, 6))
		// filter
		fatk := float32(0)
		if e[FATK] > 0.004 {
			fatk = 1 / (expMap(e[FATK], 0.002, 2) * sr)
		} else {
			fatk = 1
		}
		fdec := decayCoef(float64(sr), expMap(e[FDEC], 0.01, 3))
		fe := v.fenv
		bx := e[BASE] + fe*(e[BOFS]*2-1)
		wx := e[WDTH] + fe*(e[WOFS]*2-1)
		kt := (v.curNote - 60) / 120
		if keyHP {
			bx += kt
		}
		bx = clampf(bx, 0, 1)
		lx := bx + wx
		if keyLP {
			lx += kt
		}
		lx = clampf(lx, 0, 1)
		hpOn, lpOn := bx > 0.004, lx < 0.996
		if hpOn {
			fc := expMap(bx, 20, 20000)
			v.hp1.set(fc, 0.65, float64(sr))
			v.hp2.set(fc, lerp(0.7, 14, e[HPQ]*e[HPQ]), float64(sr))
		}
		if lpOn {
			fc := expMap(lx, 20, 20000)
			v.lp1.set(fc, 0.65, float64(sr))
			v.lp2.set(fc, lerp(0.7, 14, e[LPQ]*e[LPQ]), float64(sr))
		}
		dist := e[DIST]
		g := volGain(e[VOL]) * v.vel
		pan := e[PAN] * (math.Pi / 2)
		gl := g * float32(math.Cos(float64(pan)))
		gr := g * float32(math.Sin(float64(pan)))

		for i, x := range buf {
			// amplifier AHDR
			switch v.stage {
			case stAtk:
				v.lvl += atkInc
				if v.lvl >= 1 {
					v.lvl = 1
					v.stage = stHold
					v.holdLeft = holdN
				}
			case stHold:
				if !holdInf {
					v.holdLeft--
					if v.holdLeft <= 0 {
						v.stage = stDec
					}
				}
			case stDec:
				v.lvl *= decC
				if v.lvl < 0.001 {
					v.lvl, v.stage, v.active = 0, stIdle, false
				}
			case stRel:
				v.lvl *= relC
				if v.lvl < 0.001 {
					v.lvl, v.stage, v.active = 0, stIdle, false
				}
			}
			// filter envelope
			switch v.fstage {
			case 1:
				v.fenv += fatk
				if v.fenv >= 1 {
					v.fenv, v.fstage = 1, 2
				}
			case 2:
				v.fenv *= fdec
				if v.fenv < 0.001 {
					v.fenv, v.fstage = 0, 0
				}
			}
			x *= v.lvl
			if dist > 0.002 {
				x = drive(x, dist)
			}
			if hpOn {
				_, _, x = v.hp1.tick(x)
				_, _, x = v.hp2.tick(x)
			}
			if lpOn {
				x, _, _ = v.lp1.tick(x)
				x, _, _ = v.lp2.tick(x)
			}
			if isBad(x) {
				x = 0
				v.hp1.reset()
				v.hp2.reset()
				v.lp1.reset()
				v.lp2.reset()
			}
			x = softClip(x)
			busL[off+i] += x * gl
			busR[off+i] += x * gr
			if a := absf(x * g); a > v.peak {
				v.peak = a
			}
		}
	}
}

// volGain: volume 100 of 127 is about -4 dB, 127 about +4 dB.
func volGain(v float32) float32 { return v * v * 1.6 }
