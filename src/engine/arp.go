package engine

import "math"

// arpState is one track's arpeggiator. Its input is the keys held on a
// controller (live) plus the notes of the sequencer step that is sounding
// (seq); the arpeggiator walks through them at the speed of the kit.
type arpState struct {
	live    [16]uint8
	liveVel float32
	nLive   int
	held    int // physical keys down, for LATCH
	seq     [MaxChord]uint8
	nSeq    int
	seqVel  float32
	seqLeft int // samples until the sequencer's notes are dropped

	on       bool
	nextTick float64
	pos      int
	r        rng
}

func (a *arpState) reset() {
	r := a.r
	*a = arpState{r: r}
}

// notes collects the sorted, distinct notes of live and seq.
func (a *arpState) notes(out *[24]uint8) int {
	n := 0
	add := func(v uint8) {
		for i := 0; i < n; i++ {
			if out[i] == v {
				return
			}
		}
		if n < len(out) {
			out[n] = v
			n++
		}
	}
	for i := 0; i < a.nLive; i++ {
		add(a.live[i])
	}
	for i := 0; i < a.nSeq; i++ {
		add(a.seq[i])
	}
	for i := 1; i < n; i++ { // insertion sort
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return n
}

// wants reports whether the arpeggiator of track t should be running.
func (e *Engine) arpWants(t int) bool {
	ts := &e.kit.Track[t]
	if ts.Arp.Mode == ArpOff || Machines[ts.Machine].FX {
		return false
	}
	if pat := e.proj.Pattern(e.curPat); pat.Poly && pat.PolyTrack != t {
		return false
	}
	a := &e.tr[t].arp
	var buf [24]uint8
	n := a.notes(&buf)
	if ts.Arp.Play == 2 { // SID mode: a single key plays normally
		return n >= 2
	}
	return n >= 1
}

func (e *Engine) arpInterval(t int) float64 {
	return float64(arpSpeedTicks[clampi(e.kit.Track[t].Arp.Speed, 0, len(arpSpeedTicks)-1)])
}

// arpSamplesUntil is how many samples until the next arpeggiator event, or
// -1 when none runs.
func (e *Engine) arpSamplesUntil(tps float64) int {
	best := -1
	for t := 0; t < NumTracks; t++ {
		if !e.arpWants(t) {
			continue
		}
		a := &e.tr[t].arp
		s := 0
		if a.on {
			if d := math.Ceil((a.nextTick - e.aclk) / tps); d > 0 {
				s = int(d)
			}
		}
		if best < 0 || s < best {
			best = s
		}
	}
	return best
}

// arpAge counts down the lifetime of sequencer notes held by arpeggiators.
func (e *Engine) arpAge(n int) {
	for t := range e.tr {
		a := &e.tr[t].arp
		if a.seqLeft > 0 {
			a.seqLeft -= n
			if a.seqLeft <= 0 {
				a.seqLeft, a.nSeq = 0, 0
			}
		}
	}
}

// arpEventsLocked fires every arpeggiator step that is due.
func (e *Engine) arpEventsLocked(tps float64) {
	for t := 0; t < NumTracks; t++ {
		a := &e.tr[t].arp
		if !e.arpWants(t) {
			a.on = false
			continue
		}
		if !a.on {
			a.on, a.nextTick, a.pos = true, e.aclk, 0
		}
		iv := e.arpInterval(t)
		for k := 0; k < 4 && a.nextTick <= e.aclk+1e-6; k++ {
			e.arpFire(t, a, iv, tps)
			a.nextTick += iv
		}
		if a.nextTick < e.aclk-iv*4 { // fell far behind (tempo jump)
			a.nextTick = e.aclk
		}
	}
}

func (e *Engine) arpFire(t int, a *arpState, iv, tps float64) {
	var base [24]uint8
	n := a.notes(&base)
	if n == 0 {
		return
	}
	ts := &e.kit.Track[t]
	rng := clampi(ts.Arp.Range, 1, 4)
	total := n * rng
	var idx int
	switch ts.Arp.Mode {
	case ArpUp:
		idx = a.pos % total
	case ArpDown:
		idx = total - 1 - a.pos%total
	case ArpUpDown, ArpDownUp:
		if total == 1 {
			idx = 0
		} else {
			period := 2*total - 2
			p := a.pos % period
			if p >= total {
				p = period - p
			}
			idx = p
			if ts.Arp.Mode == ArpDownUp {
				idx = total - 1 - p
			}
		}
	default:
		idx = int(math.Abs(float64(a.r.next())) * float64(total))
		if idx >= total {
			idx = total - 1
		}
	}
	a.pos++
	note := int(base[idx%n]) + 12*(idx/n)
	if note > 127 {
		note = 127
	}
	vel := a.liveVel
	if a.nSeq > 0 && a.nLive == 0 {
		vel = a.seqVel
	}
	if vel <= 0 {
		vel = 0.8
	}
	gate := int(iv / tps * float64(ts.Arp.Len) / 127)
	if gate < 40 {
		gate = 40
	}
	v := e.allocVoice(t)
	e.age++
	v.noteOn(float32(note), vel, TrigAll, gate, -1, e.age)
}

// arpSetSeq hands the notes of a sequencer step to the track's arpeggiator.
func (e *Engine) arpSetSeq(t int, notes []uint8, vel float32, lenSamples int) {
	a := &e.tr[t].arp
	a.nSeq = copy(a.seq[:], notes)
	a.seqVel = vel
	a.seqLeft = lenSamples
}

// arpAddLive adds a held key. It reports whether the arpeggiator will play
// it (false: the caller plays the note directly, as SID mode does for one key).
func (e *Engine) arpAddLive(t, note int, vel float32) bool {
	a := &e.tr[t].arp
	ts := &e.kit.Track[t]
	if ts.Arp.Play == 1 && a.held == 0 { // LATCH: a new chord replaces the old
		a.nLive = 0
	}
	a.held++
	a.liveVel = vel
	for i := 0; i < a.nLive; i++ {
		if int(a.live[i]) == note {
			return e.arpWants(t)
		}
	}
	if a.nLive < len(a.live) {
		a.live[a.nLive] = uint8(note)
		a.nLive++
	}
	return e.arpWants(t)
}

func (e *Engine) arpRemoveLive(t, note int) {
	a := &e.tr[t].arp
	if a.held > 0 {
		a.held--
	}
	if e.kit.Track[t].Arp.Play == 1 { // latched: keep the notes
		return
	}
	for i := 0; i < a.nLive; i++ {
		if int(a.live[i]) == note {
			copy(a.live[i:], a.live[i+1:a.nLive])
			a.nLive--
			break
		}
	}
}
