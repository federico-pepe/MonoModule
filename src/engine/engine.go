package engine

import (
	"errors"
	"io"
	"math"
	"sync"
	"time"
)

// maxChunk is the largest piece Render works on at once; scratch buffers are
// this long.
const maxChunk = 512

// SyncMode says what drives the sequencer clock.
type SyncMode int

const (
	SyncInternal SyncMode = iota // own tempo
	SyncMIDI                     // MIDI clock from Live or other gear
)

// Position is where the sequencer is, for the screens.
type Position struct {
	Playing bool
	Step    int // 0-based step of the pattern playing (the last one fired)
	Pattern int // pattern index 0..127
	Next    int // queued pattern, -1 none
	Row     int // song row, -1 when not in song mode
	Repeat  int // repeats of the row played so far
	Tick    float64
}

// trackState is the live state of one track besides its voice: the value of
// every parameter as played (kit value, parameter lock, slide), the effect
// chain after the voice, and the arpeggiator.
type trackState struct {
	mute bool

	cur         [NParams]float32 // what is playing now, normalised 0..1
	lockMask    uint64
	lockVal     [NParams]float32
	slideRemain int // samples left in a parameter slide
	hasCur      bool

	eq             [2]biquad
	eqOn           bool
	srrAcc, srrVal [2]float32
	fh1, fh2       [2]svf // FX machines: the FLT page applied to stereo
	fl1, fl2       [2]svf

	peak float32
	arp  arpState
}

// Engine is the whole instrument. One mutex guards it: Render holds it for a
// block, every other method for a few microseconds.
type Engine struct {
	mu sync.Mutex
	sr float64

	proj *Project
	kit  *Kit // the working kit (what you hear and edit)
	kitN int  // slot the working kit came from, -1 none

	voices [NumTracks]*voice
	tr     [NumTracks]trackState
	fx     [NumTracks]*fxState
	dly    [NumTracks]*trackDelay
	master *masterFX
	waves  [NumUserWave]*UserWave
	utab   [NumUserWave]*[waveLen]float32
	age    uint64

	// transport
	bpm        float64
	playing    bool
	tick       float64 // ticks since the pattern started
	aclk       float64 // free-running clock of the arpeggiators, in ticks
	nextStep   int
	playStep   int
	curPat     int
	queued     int
	songMode   bool
	song       int
	songRow    int
	songRep    int
	songTrans  int
	rec        bool
	stepCursor int // step recording position
	bend       float32

	// external sync
	sync      SyncMode
	extTicks  float64
	extTarget float64
	extLast   time.Time
	extBPM    float64

	live [128]liveNote // notes being played that are recorded

	// scratch
	busL, busR [NumTracks][]float32
	inL, inR   [NumTracks][]float32
	mixL, mixR []float32
	meter      float32

	dirty bool
}

// liveNote remembers a recorded key press so its release can set the length.
type liveNote struct {
	on      bool
	track   int
	pat     int
	step    int
	startTk float64
}

// New makes an engine at the given sample rate with the factory project.
func New(sr float64) *Engine {
	if sr < 8000 {
		sr = 44100
	}
	e := &Engine{sr: sr, bpm: 120, queued: -1, kitN: 0, songRow: -1}
	for i := range e.voices {
		e.voices[i] = newVoice(i)
		e.busL[i], e.busR[i] = make([]float32, maxChunk), make([]float32, maxChunk)
		e.inL[i], e.inR[i] = make([]float32, maxChunk), make([]float32, maxChunk)
	}
	e.mixL, e.mixR = make([]float32, maxChunk), make([]float32, maxChunk)
	e.buildFX()
	e.SetProject(NewProject())
	return e
}

func (e *Engine) buildFX() {
	for i := range e.fx {
		e.fx[i] = newFXState(e.sr)
		e.dly[i] = newTrackDelay(e.sr)
	}
	e.master = newMasterFX(e.sr)
}

// SampleRate is the rate the engine renders at.
func (e *Engine) SampleRate() float64 { return e.sr }

// SetSampleRate changes the rate the engine renders at (Live may open the
// card at 44.1 or 48 kHz). Effects are rebuilt, so call it when the audio
// stream (re)starts, not while sound matters.
func (e *Engine) SetSampleRate(sr float64) {
	if sr < 8000 || sr > 192000 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if sr == e.sr {
		return
	}
	e.sr = sr
	e.buildFX()
	for _, v := range e.voices {
		v.silence()
		v.g = gens{}
	}
	for t := range e.tr {
		e.tr[t].hasCur = false
	}
}

// SetProject replaces everything: kits, patterns, songs and user waves. The
// first kit is loaded and the sequencer rewound.
func (e *Engine) SetProject(p *Project) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.proj = p
	e.bpm = p.Tempo
	if e.bpm < 30 || e.bpm > 300 {
		e.bpm = 120
	}
	for i := range e.waves {
		e.setWaveLocked(i, p.Waves[i])
	}
	e.playing = false
	e.curPat, e.queued, e.songRow, e.songTrans = 0, -1, -1, 0
	e.loadKitLocked(0)
	if pk := p.Pattern(0).Kit; pk >= 0 {
		e.loadKitLocked(pk)
	}
	if w := p.Working; w != nil {
		c := *w
		e.kit, e.kitN = &c, p.WorkingN
		e.syncVoicesLocked()
	}
	e.rewindLocked()
	e.dirty = true
}

// Project is the data the engine plays. The host saves it; do not change it
// while the engine runs except through Engine's methods.
func (e *Engine) Project() *Project { return e.proj }

// userTable makes Engine a waveSource for DPRO machines.
func (e *Engine) userTable(i int) *[waveLen]float32 {
	if i < 0 || i >= NumUserWave {
		return nil
	}
	return e.utab[i]
}

// ---------------------------------------------------------------- kits

func (e *Engine) polyActive() bool { return e.proj.Pattern(e.curPat).Poly }

// syncVoicesLocked points every voice at the track it plays and its machine.
func (e *Engine) syncVoicesLocked() {
	pat := e.proj.Pattern(e.curPat)
	for i, v := range e.voices {
		owner := i
		if pat.Poly {
			owner = pat.PolyTrack
		}
		v.setOwner(owner, e.kit.Track[owner].Machine)
	}
}

func (e *Engine) loadKitLocked(n int) {
	if n < 0 || n >= NumKits {
		return
	}
	k := e.proj.Kit(n)
	c := *k
	e.kit = &c
	e.kitN = n
	for t := range e.tr {
		e.tr[t].lockMask = 0
		e.tr[t].hasCur = false
		e.tr[t].arp.reset()
	}
	e.syncVoicesLocked()
}

// LoadKit copies kit n into the working kit.
func (e *Engine) LoadKit(n int) {
	e.mu.Lock()
	e.loadKitLocked(n)
	e.dirty = true
	e.mu.Unlock()
}

// SaveKit stores the working kit in slot n (and makes n the current slot).
func (e *Engine) SaveKit(n int, name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n < 0 || n >= NumKits {
		return
	}
	c := *e.kit
	if name != "" {
		c.Name = name
	}
	e.proj.Kits[n] = &c
	e.kit.Name = c.Name
	e.kitN = n
	e.dirty = true
}

// KitNumber is the slot the working kit was loaded from or saved to.
func (e *Engine) KitNumber() int { e.mu.Lock(); defer e.mu.Unlock(); return e.kitN }

// KitName is the working kit's name.
func (e *Engine) KitName() string { e.mu.Lock(); defer e.mu.Unlock(); return e.kit.Name }

// SetKitName renames the working kit.
func (e *Engine) SetKitName(n string) {
	e.mu.Lock()
	e.kit.Name = CleanName(n, 12)
	e.dirty = true
	e.mu.Unlock()
}

// ResetKit loads the factory kit into the working kit ("init kit"). The slot
// it came from does not change until it is saved.
func (e *Engine) ResetKit() {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := *DefaultKit(e.kit.Name)
	e.kit = &c
	for t := range e.tr {
		e.tr[t].lockMask = 0
		e.tr[t].hasCur = false
		e.tr[t].arp.reset()
	}
	e.syncVoicesLocked()
	e.dirty = true
}

// CleanName keeps printable ASCII, upper case, up to n characters.
func CleanName(s string, n int) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b = append(b, byte(r-32))
		case r >= 32 && r < 127:
			b = append(b, byte(r))
		}
		if len(b) >= n {
			break
		}
	}
	return string(b)
}

// ---------------------------------------------------------- parameters

func okTrack(t int) bool { return t >= 0 && t < NumTracks }

// Param is a parameter of the working kit, 0..127.
func (e *Engine) Param(track, p int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) || p < 0 || p >= NParams {
		return 0
	}
	return int(e.kit.Track[track].P[p])
}

// SetParam sets a parameter of the working kit.
func (e *Engine) SetParam(track, p, v int) {
	e.mu.Lock()
	e.kit.Set(track, p, v)
	e.dirty = true
	e.mu.Unlock()
}

// Machine is the machine index on a track.
func (e *Engine) Machine(track int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) {
		return 0
	}
	return e.kit.Track[track].Machine
}

// SetMachine puts a machine on a track (its SYN parameters reset to the
// machine's defaults).
func (e *Engine) SetMachine(track, machine int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) || machine < 0 || machine >= len(Machines) {
		return
	}
	e.kit.SetMachine(track, machine)
	if Machines[machine].FX && e.proj.Pattern(e.curPat).PolyTrack == track {
		// an FX machine cannot be the poly track; the pattern leaves poly mode
		e.proj.Pattern(e.curPat).Poly = false
	}
	e.tr[track].arp.reset()
	e.syncVoicesLocked()
	e.dirty = true
}

// TrackSetup returns a copy of a track's kit settings.
func (e *Engine) TrackSetup(track int) TrackSetup {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) {
		return TrackSetup{}
	}
	return e.kit.Track[track]
}

// SetTrackOptions changes the track settings that are not parameters. f
// receives a copy and returns the new value.
func (e *Engine) SetTrackOptions(track int, f func(TrackSetup) TrackSetup) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) {
		return
	}
	ts := f(e.kit.Track[track])
	ts.Machine, ts.P = e.kit.Track[track].Machine, e.kit.Track[track].P // those have their own setters
	ts.Arp.normalise()
	ts.Trans = clampi(ts.Trans, -48, 48)
	ts.Src &= (1 << uint(track)) - 1
	old := e.kit.Track[track].Arp
	e.kit.Track[track] = ts
	if ts.Arp != old {
		e.tr[track].arp.reset()
		if !(ts.Arp.Mode != ArpOff) {
			e.releaseTrackLocked(track)
		}
	}
	e.dirty = true
}

// Master is a master strip parameter of the working kit.
func (e *Engine) Master(i int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 || i >= NMaster {
		return 0
	}
	return int(e.kit.Master[i])
}

// SetMaster sets a master strip parameter.
func (e *Engine) SetMaster(i, v int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 || i >= NMaster {
		return
	}
	e.kit.Master[i] = uint8(clampi(v, 0, 127))
	e.dirty = true
}

// SetMute mutes or unmutes a track.
func (e *Engine) SetMute(track int, m bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if okTrack(track) {
		e.tr[track].mute = m
	}
}

// Muted reports a track's mute state.
func (e *Engine) Muted(track int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return okTrack(track) && e.tr[track].mute
}

// MuteMask is the mute state of the six tracks, bit 0 is track 1.
func (e *Engine) MuteMask() uint8 {
	e.mu.Lock()
	defer e.mu.Unlock()
	var m uint8
	for i := range e.tr {
		if e.tr[i].mute {
			m |= 1 << uint(i)
		}
	}
	return m
}

// SetBend sets the pitch bend in semitones (MIDI pitch wheel).
func (e *Engine) SetBend(semis float64) {
	e.mu.Lock()
	e.bend = float32(clampf(float32(semis), -12, 12))
	e.mu.Unlock()
}

// Peak returns the loudest sample the track made since the last call.
func (e *Engine) Peak(track int) float32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) {
		return 0
	}
	p := e.tr[track].peak
	e.tr[track].peak = 0
	return p
}

// MasterPeak is the loudest output sample since the last call.
func (e *Engine) MasterPeak() float32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.meter
	e.meter = 0
	return p
}

// TakeDirty reports (and clears) whether the project changed since the last
// call, so the host knows when to save.
func (e *Engine) TakeDirty() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := e.dirty
	e.dirty = false
	return d
}

// VoiceInfo is for the screens: how many voices sound and on which notes.
func (e *Engine) ActiveVoices() (n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.voices {
		if v.active {
			n++
		}
	}
	return
}

// ---------------------------------------------------------------- waves

// ErrWaveMemory means the user waveform memory is full.
var ErrWaveMemory = errors.New("user waveform memory is full")

// UserWave is a loaded user waveform: DPRO-WAVE plays it as a single cycle.
type UserWave struct {
	Name string
	Rate int
	Data []float32
}

func (e *Engine) setWaveLocked(slot int, w *UserWave) {
	e.waves[slot] = w
	if w == nil {
		e.utab[slot] = nil
		return
	}
	t := new([waveLen]float32)
	n := len(w.Data)
	for i := 0; i < waveLen; i++ {
		x := float64(i) / waveLen * float64(n)
		j := int(x)
		fr := float32(x - float64(j))
		a := w.Data[j%n]
		b := w.Data[(j+1)%n]
		t[i] = a + (b-a)*fr
	}
	normalise(t)
	e.utab[slot] = t
}

func (e *Engine) waveSamplesLocked(except int) int {
	n := 0
	for i, w := range e.waves {
		if w != nil && i != except {
			n += len(w.Data)
		}
	}
	return n
}

// SetWave puts a decoded sample in a slot. data is mono float.
func (e *Engine) SetWave(slot int, name string, rate int, data []float32) error {
	if slot < 0 || slot >= NumUserWave {
		return errors.New("bad waveform slot")
	}
	if len(data) < 32 {
		return errors.New("sample is too short")
	}
	if len(data) > MaxWaveSeconds*rate {
		return ErrWaveTooLong
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.waveSamplesLocked(slot)+len(data) > MaxWaveSamples {
		return ErrWaveMemory
	}
	w := &UserWave{Name: CleanWaveName(name), Rate: rate, Data: data}
	e.setWaveLocked(slot, w)
	e.proj.Waves[slot] = w
	e.dirty = true
	return nil
}

// LoadWAV decodes a WAV stream into a slot.
func (e *Engine) LoadWAV(slot int, name string, r io.Reader) error {
	data, rate, err := DecodeWAV(r)
	if err != nil {
		return err
	}
	return e.SetWave(slot, name, rate, data)
}

// ClearWave empties a slot.
func (e *Engine) ClearWave(slot int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if slot >= 0 && slot < NumUserWave {
		e.setWaveLocked(slot, nil)
		e.proj.Waves[slot] = nil
		e.dirty = true
	}
}

// Wave returns the wave in a slot (or nil). The wave is immutable.
func (e *Engine) Wave(slot int) *UserWave {
	e.mu.Lock()
	defer e.mu.Unlock()
	if slot < 0 || slot >= NumUserWave {
		return nil
	}
	return e.waves[slot]
}

// WaveMemory is the used and total user waveform samples.
func (e *Engine) WaveMemory() (used, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.waveSamplesLocked(-1), MaxWaveSamples
}

// ------------------------------------------------------------- render

// Render fills outL and outR with the next block of audio. The slices must
// have the same length. It allocates nothing while playing; loading a kit
// from the sequencer (a pattern with a kit, a song row) may.
func (e *Engine) Render(outL, outR []float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := len(outL)
	if len(outR) < n {
		n = len(outR)
	}
	for off := 0; off < n; {
		chunk := n - off
		if chunk > maxChunk {
			chunk = maxChunk
		}
		tps := e.ticksPerSample()
		var ft float64
		if e.playing {
			pat := e.proj.Pattern(e.curPat)
			ft = e.fireTime(pat, e.nextStep)
			if need := math.Ceil((ft - e.tick) / tps); need <= 0 {
				chunk = 0
			} else if int(need) < chunk {
				chunk = int(need)
			}
		}
		if an := e.arpSamplesUntil(tps); an >= 0 && an < chunk {
			chunk = an
		}
		if chunk > 0 {
			e.renderChunk(outL[off:off+chunk], outR[off:off+chunk])
			e.arpAge(chunk)
			if e.playing {
				e.advanceTicks(float64(chunk), tps)
			}
			e.aclk += float64(chunk) * tps
			off += chunk
		}
		if e.playing && e.tick+1e-6 >= ft {
			e.stepEventLocked()
		}
		e.arpEventsLocked(tps)
	}
}

func (e *Engine) currentBPM() float64 {
	if e.sync == SyncMIDI && e.extBPM > 0 {
		return e.extBPM
	}
	return e.bpm
}

func (e *Engine) primary(t int) *voice {
	if e.polyActive() && e.proj.Pattern(e.curPat).PolyTrack == t {
		return e.voices[0]
	}
	return e.voices[t]
}

func (e *Engine) renderChunk(outL, outR []float32) {
	n := len(outL)
	bpm := e.currentBPM()
	ctx := genCtx{sr: float32(e.sr), bpm: float32(bpm), src: e}
	for i := 0; i < n; i++ {
		e.mixL[i], e.mixR[i] = 0, 0
	}
	for t := 0; t < NumTracks; t++ {
		for i := 0; i < n; i++ {
			e.busL[t][i], e.busR[t][i], e.inL[t][i], e.inR[t][i] = 0, 0, 0, 0
		}
		e.resolveTrack(t, n)
	}
	poly := e.proj.Pattern(e.curPat)
	for _, v := range e.voices {
		ts := &e.kit.Track[v.owner]
		v.render(n, &e.tr[v.owner].cur, &ctx, ts.KeyLP, ts.KeyHP, e.bend, e.busL[v.owner][:n], e.busR[v.owner][:n])
	}
	for t := 0; t < NumTracks; t++ {
		if poly.Poly && t != poly.PolyTrack {
			continue // the other tracks are off in poly mode
		}
		e.trackChain(t, n, bpm)
	}
	e.master.process(&e.kit.Master, n, e.mixL[:n], e.mixR[:n])
	for i := 0; i < n; i++ {
		outL[i], outR[i] = e.mixL[i], e.mixR[i]
		if a := absf(outL[i]); a > e.meter {
			e.meter = a
		}
		if a := absf(outR[i]); a > e.meter {
			e.meter = a
		}
	}
	for _, v := range e.voices {
		if v.peak > e.tr[v.owner].peak {
			e.tr[v.owner].peak = v.peak
		}
		v.peak = 0
	}
}

// resolveTrack fills tr.cur from the kit values, the lock held by the last
// trig and any slide in progress.
func (e *Engine) resolveTrack(t, n int) {
	tr := &e.tr[t]
	base := &e.kit.Track[t].P
	slideF := float32(1)
	if tr.slideRemain > 0 {
		slideF = clampf(float32(n)/float32(tr.slideRemain), 0, 1)
		tr.slideRemain -= n
		if tr.slideRemain < 0 {
			tr.slideRemain = 0
		}
	}
	for i := 0; i < NParams; i++ {
		target := float32(base[i]) / 127
		locked := tr.lockMask&(1<<uint(i)) != 0
		if locked {
			target = tr.lockVal[i]
		}
		if !tr.hasCur || !locked || slideF >= 1 {
			tr.cur[i] = target
		} else {
			tr.cur[i] += (target - tr.cur[i]) * slideF
		}
	}
	tr.hasCur = true
}

// trackChain finishes a track: FX machine, EQ, sample-rate reduction, delay,
// then routes it to an FX machine that lists it as a source, or to the mix.
func (e *Engine) trackChain(t, n int, bpm float64) {
	ts := &e.kit.Track[t]
	tr := &e.tr[t]
	L, R := e.busL[t][:n], e.busR[t][:n]
	pv := e.primary(t)
	eff := &pv.eff
	sr := e.sr

	if Machines[ts.Machine].FX {
		e.fx[t].process(ts.Machine, e.inL[t][:n], e.inR[t][:n], L, R, n, (*[PerPage]float32)(eff[0:PerPage]), bpm)
		// FLT page without envelope, AMP page distortion, volume and balance
		bx := eff[BASE]
		lx := clampf(bx+eff[WDTH], 0, 1)
		hpOn, lpOn := bx > 0.004, lx < 0.996
		for ch := 0; ch < 2; ch++ {
			if hpOn {
				fc := expMap(bx, 20, 20000)
				tr.fh1[ch].set(fc, 0.65, sr)
				tr.fh2[ch].set(fc, lerp(0.7, 14, eff[HPQ]*eff[HPQ]), sr)
			}
			if lpOn {
				fc := expMap(lx, 20, 20000)
				tr.fl1[ch].set(fc, 0.65, sr)
				tr.fl2[ch].set(fc, lerp(0.7, 14, eff[LPQ]*eff[LPQ]), sr)
			}
		}
		g := volGain(eff[VOL])
		pan := eff[PAN] * (math.Pi / 2)
		gl := g * float32(math.Cos(float64(pan))) * 1.4142
		gr := g * float32(math.Sin(float64(pan))) * 1.4142
		dist := eff[DIST]
		for i := 0; i < n; i++ {
			l, r := L[i], R[i]
			if dist > 0.002 {
				l, r = drive(l, dist), drive(r, dist)
			}
			if hpOn {
				_, _, l = tr.fh1[0].tick(l)
				_, _, l = tr.fh2[0].tick(l)
				_, _, r = tr.fh1[1].tick(r)
				_, _, r = tr.fh2[1].tick(r)
			}
			if lpOn {
				l, _, _ = tr.fl1[0].tick(l)
				l, _, _ = tr.fl2[0].tick(l)
				r, _, _ = tr.fl1[1].tick(r)
				r, _, _ = tr.fl2[1].tick(r)
			}
			if isBad(l) || isBad(r) {
				l, r = 0, 0
				e.fx[t].reset()
				for ch := 0; ch < 2; ch++ {
					tr.fh1[ch].reset()
					tr.fh2[ch].reset()
					tr.fl1[ch].reset()
					tr.fl2[ch].reset()
				}
			}
			L[i], R[i] = softClip(l*gl), softClip(r*gr)
		}
	}

	// EQ and sample-rate reduction (FX page)
	eqG := (eff[EQG] - 0.5) * 72
	tr.eqOn = absf(eqG) > 0.25
	if tr.eqOn {
		tr.eq[0].peaking(expMap(eff[EQF], 40, 12000), 0.9, eqG, sr)
		tr.eq[1] = tr.eq[0].withState(tr.eq[1])
	}
	srrStep := 1 + eff[SRR]*eff[SRR]*47
	srrOn := srrStep > 1.02
	if tr.eqOn || srrOn {
		for i := 0; i < n; i++ {
			l, r := L[i], R[i]
			if tr.eqOn {
				l, r = tr.eq[0].tick(l), tr.eq[1].tick(r)
			}
			if srrOn {
				tr.srrAcc[0]++
				if tr.srrAcc[0] >= srrStep {
					tr.srrAcc[0] -= srrStep
					tr.srrVal[0], tr.srrVal[1] = l, r
				}
				l, r = tr.srrVal[0], tr.srrVal[1]
			}
			if isBad(l) || isBad(r) {
				l, r = 0, 0
				tr.eq[0].reset()
				tr.eq[1].reset()
			}
			L[i], R[i] = l, r
		}
	}
	e.dly[t].process(L, R, n, eff[PerPage*PageFx:PerPage*PageFx+PerPage], bpm)

	if tr.mute {
		for i := 0; i < n; i++ {
			L[i], R[i] = 0, 0
		}
	}
	// route
	routed := false
	for f := t + 1; f < NumTracks; f++ {
		if Machines[e.kit.Track[f].Machine].FX && e.kit.Track[f].Src&(1<<uint(t)) != 0 {
			fl, fr := e.inL[f][:n], e.inR[f][:n]
			for i := 0; i < n; i++ {
				fl[i] += L[i]
				fr[i] += R[i]
			}
			routed = true
		}
	}
	if !routed {
		for i := 0; i < n; i++ {
			e.mixL[i] += L[i]
			e.mixR[i] += R[i]
		}
	}
}

// ticksPerSample is how many sequencer ticks one audio sample lasts.
func (e *Engine) ticksPerSample() float64 {
	return e.currentBPM() * TicksPerBeat / (60 * e.sr)
}

func (e *Engine) advanceTicks(samples, tps float64) {
	d := samples * tps
	e.tick += d
	if e.sync == SyncMIDI {
		// follow the incoming clock: ease towards where the pulses say we are
		e.extTicks += d
		if e.playing && e.extBPM > 0 {
			corr := (e.extTarget - e.extTicks) * 0.05
			if math.Abs(corr) > 24 { // lost the clock: jump
				corr = e.extTarget - e.extTicks
			}
			e.tick += corr
			e.extTicks += corr
		}
	}
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
