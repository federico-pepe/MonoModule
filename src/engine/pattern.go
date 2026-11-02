package engine

// TicksPerBeat is the engine's clock resolution (pulses per quarter note).
// A 16th-note step at 1x is 24 ticks: fine enough for swing, slides and
// external clock (24 ppqn x 4).
const TicksPerBeat = 96

// ScaleNames and scaleTicks are the pattern scale options: how long one step
// lasts. The hardware has 1x and 2x; 1/2x is an addition.
var (
	ScaleNames = [...]string{"1/2x", "1x", "2x"}
	scaleTicks = [...]int{48, 24, 12}
	// DefaultScale is the index of "1x".
	DefaultScale = 1
)

// StepTicks is the length of one step of a pattern at scale index s.
func StepTicks(s int) int {
	if s < 0 || s >= len(scaleTicks) {
		s = DefaultScale
	}
	return scaleTicks[s]
}

// LenUnit: a note's length is stored in eighths of a step.
const (
	LenUnits    = 8
	DefaultLen  = 6
	DefaultVel  = 100
	DefaultNote = 60 // C-4
)

// Step is one position of one track. On says there is a trig. A trig carries
// a note (and, in poly mode, up to MaxChord-1 more), a velocity, a length in
// eighths of a step, and Trig: which envelopes it restarts (0 = trigless).
// Off marks a note-off step instead of a note.
type Step struct {
	On    bool
	Off   bool
	Trig  uint8
	Note  uint8
	Vel   uint8
	Len   uint8
	N     uint8 // extra notes in Extra
	Extra [MaxChord - 1]uint8
}

// NewNoteStep is a normal note trig.
func NewNoteStep(note, vel int) Step {
	return Step{On: true, Trig: TrigAll, Note: uint8(clampi(note, 0, 127)),
		Vel: uint8(clampi(vel, 1, 127)), Len: DefaultLen}
}

// Notes copies the step's notes into buf and returns how many there are.
func (s *Step) Notes(buf *[MaxChord]uint8) int {
	buf[0] = s.Note
	n := 1
	for i := 0; i < int(s.N) && n < MaxChord; i++ {
		buf[n] = s.Extra[i]
		n++
	}
	return n
}

// AddNote adds a note to the step (a chord); it reports false when full or
// the note is already there.
func (s *Step) AddNote(note int) bool {
	var b [MaxChord]uint8
	n := s.Notes(&b)
	for i := 0; i < n; i++ {
		if int(b[i]) == note {
			return false
		}
	}
	if n >= MaxChord {
		return false
	}
	s.Extra[n-1] = uint8(note)
	s.N++
	return true
}

// Lock is a set of parameter locks on one step: the parameters in Mask take
// the value in Val for that trig instead of the kit's value.
type Lock struct {
	Mask uint64
	Val  [NParams]uint8
}

// Has reports whether parameter p is locked.
func (l *Lock) Has(p int) bool { return l != nil && p >= 0 && p < NParams && l.Mask&(1<<uint(p)) != 0 }

// TrackPat is one track's row in a pattern.
type TrackPat struct {
	Step [MaxSteps]Step
	Lock [MaxSteps]*Lock
}

// Pattern is six tracks of up to 64 steps, plus the swing and slide layers
// shared by all tracks. In POLY mode the PolyTrack plays all six voices and
// the other tracks are silent.
type Pattern struct {
	Name      string
	Len       int // 2..64 steps
	Scale     int // index into ScaleNames
	SwingAmt  int // 50..80 (percent)
	Kit       int // kit this pattern loads when it starts; -1 keeps the current kit
	Poly      bool
	PolyTrack int
	Trans     int // pattern transpose in semitones, -24..24
	Swing     [MaxSteps]bool
	Slide     [MaxSteps]bool
	Tracks    [NumTracks]TrackPat
}

// NewPattern is an empty pattern: 16 steps, 1x, no swing.
func NewPattern() *Pattern {
	return &Pattern{Len: 16, Scale: DefaultScale, SwingAmt: 50, Kit: -1}
}

// Clone is a deep copy.
func (p *Pattern) Clone() *Pattern {
	c := *p
	for t := range p.Tracks {
		for s, l := range p.Tracks[t].Lock {
			if l != nil {
				lc := *l
				c.Tracks[t].Lock[s] = &lc
			}
		}
	}
	return &c
}

// Empty reports whether the pattern is indistinguishable from a new one, so
// the project file can leave it out.
func (p *Pattern) Empty() bool {
	n := NewPattern()
	if p.Len != n.Len || p.Scale != n.Scale || p.SwingAmt != n.SwingAmt ||
		p.Kit != n.Kit || p.Name != "" || p.Poly || p.Trans != 0 {
		return false
	}
	if p.Swing != n.Swing || p.Slide != n.Slide {
		return false
	}
	for t := range p.Tracks {
		for s := range p.Tracks[t].Step {
			if p.Tracks[t].Step[s].On || p.Tracks[t].Lock[s] != nil {
				return false
			}
		}
	}
	return true
}

// normalise puts every field in range (a project file may be hand edited).
func (p *Pattern) normalise() {
	p.Len = clampi(p.Len, 2, MaxSteps)
	if p.Scale < 0 || p.Scale >= len(ScaleNames) {
		p.Scale = DefaultScale
	}
	p.SwingAmt = clampi(p.SwingAmt, 50, 80)
	if p.Kit < -1 || p.Kit >= NumKits {
		p.Kit = -1
	}
	p.PolyTrack = clampi(p.PolyTrack, 0, NumTracks-1)
	p.Trans = clampi(p.Trans, -24, 24)
	for t := range p.Tracks {
		tp := &p.Tracks[t]
		for s := range tp.Step {
			st := &tp.Step[s]
			st.Note = uint8(clampi(int(st.Note), 0, 127))
			st.Vel = uint8(clampi(int(st.Vel), 1, 127))
			if st.Len == 0 {
				st.Len = DefaultLen
			}
			if st.Len > 127 {
				st.Len = 127
			}
			if st.N > MaxChord-1 {
				st.N = MaxChord - 1
			}
			for i := range st.Extra {
				st.Extra[i] = uint8(clampi(int(st.Extra[i]), 0, 127))
			}
			if l := tp.Lock[s]; l != nil {
				l.Mask &= (1 << NParams) - 1
				for i := range l.Val {
					if l.Val[i] > 127 {
						l.Val[i] = 127
					}
				}
				if l.Mask == 0 || !st.On {
					tp.Lock[s] = nil
				}
			}
		}
	}
}

// ---------------------------------------------------------------- kits

// Arpeggiator modes, speeds and play modes.
const (
	ArpOff = iota
	ArpUp
	ArpDown
	ArpUpDown
	ArpDownUp
	ArpRandom
	numArpModes
)

// ArpModeNames, ArpSpeedNames and ArpPlayNames are the display names.
var (
	ArpModeNames  = [numArpModes]string{"OFF", "UP", "DOWN", "UP/DN", "DN/UP", "RND"}
	ArpSpeedNames = [...]string{"1/32", "1/16T", "1/16", "1/8T", "1/8", "1/4T", "1/4"}
	arpSpeedTicks = [...]int{12, 16, 24, 32, 48, 64, 96}
	ArpPlayNames  = [...]string{"NORM", "LATCH", "SID"}
)

// ArpSetup is a track's arpeggiator: it is part of the kit.
type ArpSetup struct {
	Mode  int // ArpOff..ArpRandom
	Speed int // index into ArpSpeedNames
	Range int // octaves, 1..4
	Len   int // gate length 0..127 (127 = full step)
	Play  int // 0 normal, 1 latch, 2 SID (arpeggiates only with two or more keys)
}

// DefaultArp is off, 1/16, one octave, 3/4 gate.
var DefaultArp = ArpSetup{Mode: ArpOff, Speed: 2, Range: 1, Len: 96, Play: 0}

func (a *ArpSetup) normalise() {
	a.Mode = clampi(a.Mode, 0, numArpModes-1)
	a.Speed = clampi(a.Speed, 0, len(arpSpeedTicks)-1)
	a.Range = clampi(a.Range, 1, 4)
	a.Len = clampi(a.Len, 1, 127)
	a.Play = clampi(a.Play, 0, len(ArpPlayNames)-1)
}

// TrackSetup is one track of a kit: the machine, its 56 parameters, filter
// key tracking, the FX machine inputs, transpose and arpeggiator.
type TrackSetup struct {
	Machine  int
	P        [NParams]uint8
	KeyLP    bool
	KeyHP    bool
	Src      uint8 // for FX machines: bit j set = track j (lower numbered) feeds this track
	Trans    int   // track transpose in semitones
	TransFix bool  // ignore pattern/song transpose (drum machines)
	Arp      ArpSetup
}

// Kit is a sound set: six tracks and the master strip.
type Kit struct {
	Name   string
	Track  [NumTracks]TrackSetup
	Master [NMaster]uint8
}

// SetMachine puts a machine on a track and resets its SYN parameters to that
// machine's defaults. The other pages stay.
func (k *Kit) SetMachine(track, machine int) {
	if track < 0 || track >= NumTracks || machine < 0 || machine >= len(Machines) {
		return
	}
	k.Track[track].Machine = machine
	copy(k.Track[track].P[:PerPage], Machines[machine].Defaults[:])
	if Machines[machine].FX && k.Track[track].Src == 0 && track > 0 {
		k.Track[track].Src = 1 << uint(track-1) // neighbour: the track before
	}
}

// NewKit is a kit with the given machine names in track order and neutral
// settings elsewhere. Unknown or missing names fall back to SWAVE-SAW.
func NewKit(name string, machines []string) *Kit {
	k := &Kit{Name: name, Master: DefaultMaster}
	for t := 0; t < NumTracks; t++ {
		m := mSwSaw
		if t < len(machines) {
			if i := MachineIndex(machines[t]); i >= 0 {
				m = i
			}
		}
		ts := &k.Track[t]
		ts.Machine = m
		copy(ts.P[:PerPage], Machines[m].Defaults[:])
		copy(ts.P[PerPage:], DefaultCommon[PerPage:])
		ts.Arp = DefaultArp
		ts.KeyLP, ts.KeyHP = true, true
		if Machines[m].FX && t > 0 {
			ts.Src = 1 << uint(t-1)
		}
	}
	return k
}

// Set sets a parameter, clamped to 0..127.
func (k *Kit) Set(track, param, v int) {
	if track < 0 || track >= NumTracks || param < 0 || param >= NParams {
		return
	}
	k.Track[track].P[param] = uint8(clampi(v, 0, 127))
}

func (k *Kit) normalise() {
	for t := range k.Track {
		ts := &k.Track[t]
		if ts.Machine < 0 || ts.Machine >= len(Machines) {
			ts.Machine = 0
		}
		for q := range ts.P {
			if ts.P[q] > 127 {
				ts.P[q] = 127
			}
		}
		ts.Trans = clampi(ts.Trans, -48, 48)
		ts.Arp.normalise()
		ts.Src &= (1 << uint(t)) - 1 // only lower-numbered tracks can feed
	}
	for g := range k.Master {
		if k.Master[g] > 127 {
			k.Master[g] = 127
		}
	}
}

// ---------------------------------------------------------------- songs

// SongRow plays a pattern for a number of repeats, transposed.
type SongRow struct {
	Pattern int
	Repeats int // 1..99
	Trans   int // semitones, -24..24
}

// Song is a list of rows, with an optional loop back to a row at the end.
type Song struct {
	Name   string
	Rows   []SongRow
	LoopTo int // row to jump to after the last row; -1 stops
}

// NewSong is an empty song that stops at its end.
func NewSong() *Song { return &Song{LoopTo: -1} }
