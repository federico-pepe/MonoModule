package engine

import (
	"math"
	"testing"
)

const sr = 44100

func newTest() *Engine {
	e := New(sr)
	e.EditPattern(0, func(p *Pattern) { *p = *NewPattern() })
	return e
}

func renderN(e *Engine, n int) ([]float32, []float32) {
	l, r := make([]float32, n), make([]float32, n)
	// render in awkward block sizes to exercise chunking
	for off := 0; off < n; {
		b := 331
		if off+b > n {
			b = n - off
		}
		e.Render(l[off:off+b], r[off:off+b])
		off += b
	}
	return l, r
}

func firstAbove(l, r []float32, th float32) int {
	for i := range l {
		if absf(l[i]) > th || absf(r[i]) > th {
			return i
		}
	}
	return -1
}

func TestStepTiming(t *testing.T) {
	e := newTest()
	e.SetTempo(120)
	e.SetParam(0, ATK, 0)
	e.ToggleStep(0, 4, 48)
	e.Play()
	l, r := renderN(e, sr*2)
	got := firstAbove(l, r, 0.002)
	want := 4 * sr / 8 // a step is 1/8 s at 120 bpm
	if got < want-5 || got > want+300 {
		t.Fatalf("onset at %d, want about %d", got, want)
	}
}

func TestPatternLoopsAtLength(t *testing.T) {
	e := newTest()
	e.EditCurrent(func(p *Pattern) { p.Len = 4 })
	e.ToggleStep(0, 0, 48)
	e.Play()
	l, r := renderN(e, sr)
	// four steps = half a second: onsets at 0, 0.5 s
	second := firstAbove(l[sr/2-200:], r[sr/2-200:], 0.01)
	if second < 0 {
		t.Fatal("no second pass")
	}
}

func TestSwingDelaysStep(t *testing.T) {
	e := newTest()
	e.EditCurrent(func(p *Pattern) { p.SwingAmt = 75 })
	e.ToggleSwing(1)
	e.ToggleStep(0, 1, 48)
	e.Play()
	l, r := renderN(e, sr)
	got := firstAbove(l, r, 0.002)
	step := sr / 8
	want := step + step/2 // 75% is half a step late
	if got < want-5 || got > want+400 {
		t.Fatalf("swung onset %d want about %d", got, want)
	}
}

func TestParameterLock(t *testing.T) {
	e := newTest()
	e.ToggleStep(0, 0, 48)
	e.LockParam(0, 0, VOL, 0)
	e.Play()
	l, r := renderN(e, sr/10)
	if rms(l, r) > 1e-3 {
		t.Fatalf("locked VOL=0 still sounds: %g", rms(l, r))
	}
	if _, ok := e.StepLock(0, 0); !ok {
		t.Fatal("lock missing")
	}
	e.ClearLock(0, 0, -1)
	if _, ok := e.StepLock(0, 0); ok {
		t.Fatal("lock not cleared")
	}
}

func TestAHDR(t *testing.T) {
	e := newTest()
	e.SetMachine(0, mSwSaw)
	e.SetParam(0, ATK, 0)
	e.SetParam(0, HOLD, 10)
	e.SetParam(0, DEC, 20)
	e.SetParam(0, REL, 20)
	e.NoteOn(0, 48, 1)
	l, r := renderN(e, sr*3)
	if rms(l[:2000], r[:2000]) < 0.01 {
		t.Fatal("no attack")
	}
	if rms(l[sr*2:], r[sr*2:]) > 1e-3 {
		t.Fatal("finite hold + decay should end silent")
	}
	// HOLD at 127 never decays while the key is down
	e.SetParam(0, HOLD, 127)
	e.NoteOn(0, 52, 1)
	l, r = renderN(e, sr*2)
	if rms(l[sr:], r[sr:]) < 0.01 {
		t.Fatal("infinite hold decayed")
	}
	e.NoteOff(0, 52)
	l, r = renderN(e, sr*3)
	if rms(l[sr*2:], r[sr*2:]) > 1e-3 {
		t.Fatal("note off did not release")
	}
}

func TestFilterClosesSound(t *testing.T) {
	level := func(wdth int) float64 {
		e := newTest()
		e.SetMachine(0, mSwSaw)
		e.SetParam(0, HOLD, 127)
		e.SetParam(0, BASE, 0)
		e.SetParam(0, WDTH, wdth)
		e.SetParam(0, BOFS, 64)
		e.SetParam(0, WOFS, 64)
		e.NoteOn(0, 60, 1)
		renderN(e, 4000)
		l, r := renderN(e, 8000)
		return rms(l, r)
	}
	open, closed := level(127), level(25)
	if !(closed < open*0.6) {
		t.Fatalf("closing the low-pass should cut level: open %g closed %g", open, closed)
	}
}

func TestPolyChord(t *testing.T) {
	e := newTest()
	e.EditCurrent(func(p *Pattern) { p.Poly, p.PolyTrack = true, 0 })
	e.SetParam(0, HOLD, 127)
	e.EditStep(0, 0, func(s *Step) { *s = NewNoteStep(48, 100); s.AddNote(52); s.AddNote(55) })
	e.Play()
	renderN(e, 3000)
	if n := e.ActiveVoices(); n != 3 {
		t.Fatalf("chord should use 3 voices, got %d", n)
	}
}

func TestPolyStealsOldest(t *testing.T) {
	e := newTest()
	e.EditCurrent(func(p *Pattern) { p.Poly, p.PolyTrack = true, 0 })
	e.SetParam(0, HOLD, 127)
	for i := 0; i < 8; i++ {
		e.NoteOn(0, 40+i, 1)
		renderN(e, 100)
	}
	if n := e.ActiveVoices(); n != 6 {
		t.Fatalf("want 6 voices busy, got %d", n)
	}
}

func TestPolyMutesOtherTracks(t *testing.T) {
	e := newTest()
	e.EditCurrent(func(p *Pattern) { p.Poly, p.PolyTrack = true, 1 })
	e.NoteOn(0, 48, 1) // track 1 is disabled
	if e.ActiveVoices() != 0 {
		t.Fatal("other track played in poly mode")
	}
}

func TestArpOrders(t *testing.T) {
	seq := func(mode int) []int {
		e := newTest()
		e.SetTrackOptions(0, func(ts TrackSetup) TrackSetup { ts.Arp.Mode = mode; ts.Arp.Range = 1; return ts })
		e.SetParam(0, HOLD, 127)
		for _, n := range []int{60, 64, 67} {
			e.NoteOn(0, n, 1)
		}
		var out []int
		a := &e.tr[0].arp
		e.mu.Lock()
		for i := 0; i < 8; i++ {
			e.arpFire(0, a, 24, 1)
			out = append(out, int(e.voices[0].note))
		}
		e.mu.Unlock()
		return out
	}
	eq := func(a, b []int) bool {
		for i := range b {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if g := seq(ArpUp); !eq(g, []int{60, 64, 67, 60, 64, 67, 60, 64}) {
		t.Errorf("up: %v", g)
	}
	if g := seq(ArpDown); !eq(g, []int{67, 64, 60, 67, 64, 60, 67, 64}) {
		t.Errorf("down: %v", g)
	}
	if g := seq(ArpUpDown); !eq(g, []int{60, 64, 67, 64, 60, 64, 67, 64}) {
		t.Errorf("updown: %v", g)
	}
}

func TestArpSIDNeedsTwoKeys(t *testing.T) {
	e := newTest()
	e.SetTrackOptions(0, func(ts TrackSetup) TrackSetup { ts.Arp.Mode = ArpUp; ts.Arp.Play = 2; return ts })
	e.NoteOn(0, 60, 1)
	e.mu.Lock()
	if e.arpWants(0) {
		t.Error("SID mode arpeggiates one key")
	}
	e.mu.Unlock()
	e.NoteOn(0, 64, 1)
	e.mu.Lock()
	if !e.arpWants(0) {
		t.Error("SID mode should arpeggiate two keys")
	}
	e.mu.Unlock()
}

func TestFXRouting(t *testing.T) {
	e := newTest()
	e.SetMachine(1, mFxThru)
	e.SetTrackOptions(1, func(ts TrackSetup) TrackSetup { ts.Src = 1; return ts })
	e.SetParam(0, HOLD, 127)
	e.NoteOn(0, 48, 1)
	l, r := renderN(e, 6000)
	if rms(l, r) < 1e-3 {
		t.Fatal("track 1 through FX-THRU is silent")
	}
}

func TestProjectRoundTrip(t *testing.T) {
	e := newTest()
	e.SetMachine(2, mFmStat)
	e.SetParam(2, DEC, 33)
	e.SaveKit(5, "MYKIT")
	e.ToggleStep(0, 3, 50)
	e.SetStepNote(0, 3, 55, false)
	e.LockParam(0, 3, BASE, 99)
	e.EditCurrent(func(p *Pattern) { p.Len = 32; p.Poly = true; p.Trans = 3 })
	e.EditSong(0, func(s *Song) { s.Rows = []SongRow{{0, 2, 1}, {1, 1, 0}}; s.LoopTo = 0 })
	b, _, err := e.MarshalProject()
	if err != nil {
		t.Fatal(err)
	}
	p, err := UnmarshalProject(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kit(5).Name != "MYKIT" || p.Kit(5).Track[2].Machine != mFmStat || p.Kit(5).Track[2].P[DEC] != 33 {
		t.Error("kit lost")
	}
	pt := p.Pattern(0)
	if pt.Len != 32 || !pt.Poly || pt.Trans != 3 || !pt.Tracks[0].Step[3].On || pt.Tracks[0].Step[3].Note != 55 {
		t.Error("pattern lost")
	}
	if l := pt.Tracks[0].Lock[3]; l == nil || !l.Has(BASE) || l.Val[BASE] != 99 {
		t.Error("lock lost")
	}
	if s := p.Song(0); len(s.Rows) != 2 || s.Rows[0].Repeats != 2 || s.LoopTo != 0 {
		t.Error("song lost")
	}
	if _, err := UnmarshalProject([]byte(`{"format":"x"}`)); err == nil {
		t.Error("wrong format accepted")
	}
}

func TestSongAdvances(t *testing.T) {
	e := newTest()
	e.EditPattern(0, func(p *Pattern) { p.Len = 2 })
	e.EditPattern(1, func(p *Pattern) { p.Len = 2 })
	e.EditSong(0, func(s *Song) { s.Rows = []SongRow{{0, 1, 0}, {1, 1, 0}}; s.LoopTo = -1 })
	e.SetSong(true, 0)
	e.Play()
	renderN(e, sr/8*2+2000) // into pattern 2
	if e.Position().Pattern != 1 {
		t.Fatalf("pattern %d, want 1", e.Position().Pattern)
	}
	renderN(e, sr)
	if e.Playing() {
		t.Fatal("song should stop at its end")
	}
}

func TestMIDIClockTempo(t *testing.T) {
	e := newTest()
	e.SetSync(SyncMIDI)
	now := e.extLast
	for i := 0; i < 100; i++ {
		now = now.Add(20833333) // 24 ppqn at 120 bpm
		e.ExtPulse(now)
	}
	if bpm := e.Tempo(); math.Abs(bpm-120) > 1.5 {
		t.Fatalf("tempo %g", bpm)
	}
}

func TestRecordNotes(t *testing.T) {
	e := newTest()
	e.SetRecord(true)
	e.NoteOn(0, 61, 0.8)
	e.NoteOff(0, 61)
	var on bool
	e.ReadCurrent(func(p *Pattern) { on = p.Tracks[0].Step[0].On && p.Tracks[0].Step[0].Note == 61 })
	if !on {
		t.Fatal("step recording missed the note")
	}
	if e.StepCursor() != 1 {
		t.Fatal("cursor did not advance")
	}
}

func TestRenderAllocFree(t *testing.T) {
	e := New(sr)
	e.SetMachine(5, mFxReverb)
	e.SetTrackOptions(2, func(ts TrackSetup) TrackSetup { ts.Arp.Mode = ArpUp; return ts })
	e.NoteOn(2, 60, 1)
	e.NoteOn(2, 64, 1)
	e.Play()
	l, r := make([]float32, 256), make([]float32, 256)
	for i := 0; i < 200; i++ {
		e.Render(l, r)
	}
	if a := testing.AllocsPerRun(200, func() { e.Render(l, r) }); a != 0 {
		t.Fatalf("Render allocates %v per call", a)
	}
}

func TestWAVUserWave(t *testing.T) {
	e := newTest()
	d := make([]float32, 2000)
	for i := range d {
		d[i] = float32(math.Sin(float64(i) / 2000 * 2 * math.Pi))
	}
	if err := e.SetWave(0, "sine", sr, d); err != nil {
		t.Fatal(err)
	}
	e.SetMachine(0, mDpWave)
	e.SetParam(0, HOLD, 127)
	e.NoteOn(0, 48, 1)
	l, r := renderN(e, 4000)
	if rms(l, r) < 1e-3 {
		t.Fatal("DPRO-WAVE silent")
	}
	if u, _ := e.WaveMemory(); u != 2000 {
		t.Fatalf("memory %d", u)
	}
}
