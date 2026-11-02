package engine

import (
	"encoding/json"
	"errors"
	"fmt"
)

// TrackNames are the six track labels.
var TrackNames = [NumTracks]string{"T1", "T2", "T3", "T4", "T5", "T6"}

// PatternName is the bank letter and number of a pattern: A01..H16.
func PatternName(n int) string {
	n = clampi(n, 0, NumPatterns-1)
	return fmt.Sprintf("%c%02d", 'A'+n/16, n%16+1)
}

// Project is everything the host saves: kits, patterns, songs and the user
// waveforms. Slots that were never touched are nil and read as factory.
type Project struct {
	Name     string
	Tempo    float64
	Kits     [NumKits]*Kit
	Patterns [NumPatterns]*Pattern
	Songs    [NumSongs]*Song
	Waves    [NumUserWave]*UserWave
	Working  *Kit // the working kit as last saved (edits not yet stored in a slot)
	WorkingN int
}

// Kit returns kit n, making the factory kit on first use.
func (p *Project) Kit(n int) *Kit {
	n = clampi(n, 0, NumKits-1)
	if p.Kits[n] == nil {
		p.Kits[n] = DefaultKit(fmt.Sprintf("KIT%03d", n+1))
	}
	return p.Kits[n]
}

// Pattern returns pattern n, making an empty one on first use.
func (p *Project) Pattern(n int) *Pattern {
	n = clampi(n, 0, NumPatterns-1)
	if p.Patterns[n] == nil {
		p.Patterns[n] = NewPattern()
	}
	return p.Patterns[n]
}

// Song returns song n, making an empty one on first use.
func (p *Project) Song(n int) *Song {
	n = clampi(n, 0, NumSongs-1)
	if p.Songs[n] == nil {
		p.Songs[n] = NewSong()
	}
	return p.Songs[n]
}

// DefaultKit is the factory kit: saw bass, pulse lead, ensemble pad, FM bell,
// a beat box and a user-wave track.
func DefaultKit(name string) *Kit {
	return NewKit(name, []string{"SWAVE-SAW", "SWAVE-PULSE", "SWAVE-ENS", "FM+STAT", "DPRO-BBOX", "VO-6"})
}

// NewProject is the factory project: every pattern and kit ready (so the
// audio thread never has to allocate) and a short demo in pattern A01.
func NewProject() *Project {
	p := &Project{Name: "MONOMODULE", Tempo: 120}
	for i := range p.Kits {
		p.Kit(i)
	}
	for i := range p.Patterns {
		p.Pattern(i)
	}
	for i := range p.Songs {
		p.Song(i)
	}
	p.Kits[0].Name = "INIT"
	pat := p.Patterns[0]
	pat.Name = "DEMO"
	bass := []int{0, 3, 6, 10, 12}
	notes := []int{36, 36, 39, 34, 41}
	for i, s := range bass {
		pat.Tracks[0].Step[s] = NewNoteStep(notes[i], 110)
	}
	for _, s := range []int{0, 8} {
		st := NewNoteStep(60, 90)
		st.Len = 56
		st.AddNote(63)
		st.AddNote(67)
		pat.Tracks[2].Step[s] = st
	}
	for _, s := range []int{0, 4, 8, 12} {
		pat.Tracks[4].Step[s] = NewNoteStep(36, 120)
	}
	return p
}

// ---------------------------------------------------------------- JSON

// The file format is sparse: only used patterns, changed kits and non-empty
// songs are written, and steps are listed by position.

type jsonStep struct {
	T int       `json:"t"`           // track
	S int       `json:"s"`           // step
	N []uint8   `json:"n,omitempty"` // notes
	V uint8     `json:"v,omitempty"`
	L uint8     `json:"l,omitempty"`
	X uint8     `json:"x"`           // trig bits
	O bool      `json:"o,omitempty"` // note off
	K *jsonLock `json:"k,omitempty"`
}

type jsonLock struct {
	M uint64  `json:"m"`
	V []uint8 `json:"v"` // values of the locked parameters in index order
}

type jsonPattern struct {
	N     int        `json:"n"`
	Name  string     `json:"name,omitempty"`
	Len   int        `json:"len"`
	Scale int        `json:"scale"`
	SwAmt int        `json:"swing_amt"`
	Kit   int        `json:"kit"`
	Poly  bool       `json:"poly,omitempty"`
	PTrk  int        `json:"poly_track,omitempty"`
	Trans int        `json:"trans,omitempty"`
	Swing []int      `json:"swing,omitempty"`
	Slide []int      `json:"slide,omitempty"`
	Steps []jsonStep `json:"steps,omitempty"`
}

type jsonKit struct {
	N      int              `json:"n"`
	Name   string           `json:"name"`
	Master []uint8          `json:"master"`
	Tracks []jsonTrackSetup `json:"tracks"`
}

type jsonTrackSetup struct {
	Machine string   `json:"machine"`
	P       []uint8  `json:"p"`
	KeyLP   bool     `json:"key_lp"`
	KeyHP   bool     `json:"key_hp"`
	Src     uint8    `json:"src,omitempty"`
	Trans   int      `json:"trans,omitempty"`
	Fix     bool     `json:"trans_fix,omitempty"`
	Arp     ArpSetup `json:"arp"`
}

type jsonSong struct {
	N      int       `json:"n"`
	Name   string    `json:"name,omitempty"`
	LoopTo int       `json:"loop_to"`
	Rows   []SongRow `json:"rows"`
}

type jsonProject struct {
	Format   string        `json:"format"`
	Name     string        `json:"name"`
	Tempo    float64       `json:"tempo"`
	Kits     []jsonKit     `json:"kits"`
	Working  *jsonKit      `json:"working,omitempty"`
	WorkingN int           `json:"working_n"`
	Patterns []jsonPattern `json:"patterns"`
	Songs    []jsonSong    `json:"songs"`
}

const formatID = "monomodule-1"

func kitToJSON(n int, k *Kit) jsonKit {
	j := jsonKit{N: n, Name: k.Name, Master: append([]uint8(nil), k.Master[:]...)}
	for t := range k.Track {
		ts := &k.Track[t]
		j.Tracks = append(j.Tracks, jsonTrackSetup{Machine: Machines[ts.Machine].Name, P: append([]uint8(nil), ts.P[:]...),
			KeyLP: ts.KeyLP, KeyHP: ts.KeyHP, Src: ts.Src, Trans: ts.Trans, Fix: ts.TransFix, Arp: ts.Arp})
	}
	return j
}

func kitFromJSON(j *jsonKit) *Kit {
	k := &Kit{Name: CleanName(j.Name, 12), Master: DefaultMaster}
	copy(k.Master[:], j.Master)
	for t := 0; t < NumTracks; t++ {
		ts := &k.Track[t]
		ts.Arp = DefaultArp
		ts.KeyLP, ts.KeyHP = true, true
		if t >= len(j.Tracks) {
			ts.Machine = mSwSaw
			copy(ts.P[:], DefaultCommon[:])
			copy(ts.P[:PerPage], Machines[ts.Machine].Defaults[:])
			continue
		}
		jt := &j.Tracks[t]
		ts.Machine = MachineIndex(jt.Machine)
		if ts.Machine < 0 {
			ts.Machine = mSwSaw
		}
		copy(ts.P[:], jt.P)
		ts.KeyLP, ts.KeyHP, ts.Src, ts.Trans, ts.TransFix, ts.Arp = jt.KeyLP, jt.KeyHP, jt.Src, jt.Trans, jt.Fix, jt.Arp
	}
	k.normalise()
	return k
}

func sameKit(a, b *Kit) bool { return *a == *b }

func patToJSON(n int, p *Pattern) jsonPattern {
	j := jsonPattern{N: n, Name: p.Name, Len: p.Len, Scale: p.Scale, SwAmt: p.SwingAmt, Kit: p.Kit,
		Poly: p.Poly, PTrk: p.PolyTrack, Trans: p.Trans}
	for s := 0; s < MaxSteps; s++ {
		if p.Swing[s] {
			j.Swing = append(j.Swing, s)
		}
		if p.Slide[s] {
			j.Slide = append(j.Slide, s)
		}
	}
	for t := range p.Tracks {
		tp := &p.Tracks[t]
		for s := range tp.Step {
			st := &tp.Step[s]
			if !st.On {
				continue
			}
			js := jsonStep{T: t, S: s, V: st.Vel, L: st.Len, X: st.Trig, O: st.Off}
			var b [MaxChord]uint8
			n := st.Notes(&b)
			js.N = append([]uint8(nil), b[:n]...)
			if l := tp.Lock[s]; l != nil {
				jl := &jsonLock{M: l.Mask}
				for i := 0; i < NParams; i++ {
					if l.Has(i) {
						jl.V = append(jl.V, l.Val[i])
					}
				}
				js.K = jl
			}
			j.Steps = append(j.Steps, js)
		}
	}
	return j
}

func patFromJSON(j *jsonPattern) (*Pattern, error) {
	p := NewPattern()
	p.Name, p.Len, p.Scale, p.SwingAmt, p.Kit = CleanName(j.Name, 12), j.Len, j.Scale, j.SwAmt, j.Kit
	p.Poly, p.PolyTrack, p.Trans = j.Poly, j.PTrk, j.Trans
	for _, s := range j.Swing {
		if s >= 0 && s < MaxSteps {
			p.Swing[s] = true
		}
	}
	for _, s := range j.Slide {
		if s >= 0 && s < MaxSteps {
			p.Slide[s] = true
		}
	}
	for i := range j.Steps {
		js := &j.Steps[i]
		if js.T < 0 || js.T >= NumTracks || js.S < 0 || js.S >= MaxSteps {
			return nil, fmt.Errorf("step %d/%d out of range", js.T, js.S)
		}
		st := Step{On: true, Off: js.O, Trig: js.X & TrigAll, Vel: js.V, Len: js.L}
		if len(js.N) > 0 {
			st.Note = js.N[0]
			for _, x := range js.N[1:] {
				if int(st.N) < MaxChord-1 {
					st.Extra[st.N] = x
					st.N++
				}
			}
		}
		tp := &p.Tracks[js.T]
		tp.Step[js.S] = st
		if js.K != nil && js.K.M != 0 {
			l := &Lock{Mask: js.K.M & ((1 << NParams) - 1)}
			vi := 0
			for q := 0; q < NParams && vi < len(js.K.V); q++ {
				if l.Has(q) {
					l.Val[q] = js.K.V[vi]
					vi++
				}
			}
			tp.Lock[js.S] = l
		}
	}
	p.normalise()
	return p, nil
}

// Marshal encodes the project as indented JSON (user waves are saved by the
// host as WAV files next to it).
func (p *Project) Marshal() ([]byte, error) {
	j := jsonProject{Format: formatID, Name: p.Name, Tempo: p.Tempo, WorkingN: p.WorkingN}
	for i, k := range p.Kits {
		if k != nil && !sameKit(k, DefaultKit(fmt.Sprintf("KIT%03d", i+1))) {
			j.Kits = append(j.Kits, kitToJSON(i, k))
		}
	}
	if p.Working != nil {
		w := kitToJSON(p.WorkingN, p.Working)
		j.Working = &w
	}
	for i, pt := range p.Patterns {
		if pt != nil && !pt.Empty() {
			j.Patterns = append(j.Patterns, patToJSON(i, pt))
		}
	}
	for i, s := range p.Songs {
		if s != nil && (len(s.Rows) > 0 || s.Name != "") {
			j.Songs = append(j.Songs, jsonSong{N: i, Name: s.Name, LoopTo: s.LoopTo, Rows: s.Rows})
		}
	}
	return json.MarshalIndent(&j, "", " ")
}

// UnmarshalProject decodes a project file. Unknown fields are ignored and
// every value is brought into range.
func UnmarshalProject(b []byte) (*Project, error) {
	var j jsonProject
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	if j.Format != formatID {
		return nil, errors.New("not a MonoModule project (format " + j.Format + ")")
	}
	p := &Project{Name: CleanName(j.Name, 16), Tempo: j.Tempo, WorkingN: j.WorkingN}
	if p.Tempo < 30 || p.Tempo > 300 {
		p.Tempo = 120
	}
	for i := range p.Kits {
		p.Kit(i)
	}
	for i := range p.Patterns {
		p.Pattern(i)
	}
	for i := range p.Songs {
		p.Song(i)
	}
	for i := range j.Kits {
		jk := &j.Kits[i]
		if jk.N < 0 || jk.N >= NumKits {
			return nil, fmt.Errorf("kit %d out of range", jk.N)
		}
		p.Kits[jk.N] = kitFromJSON(jk)
	}
	if j.Working != nil {
		p.Working = kitFromJSON(j.Working)
		p.WorkingN = clampi(j.WorkingN, -1, NumKits-1)
	}
	for i := range j.Patterns {
		jp := &j.Patterns[i]
		if jp.N < 0 || jp.N >= NumPatterns {
			return nil, fmt.Errorf("pattern %d out of range", jp.N)
		}
		pt, err := patFromJSON(jp)
		if err != nil {
			return nil, err
		}
		p.Patterns[jp.N] = pt
	}
	for i := range j.Songs {
		js := &j.Songs[i]
		if js.N < 0 || js.N >= NumSongs {
			return nil, fmt.Errorf("song %d out of range", js.N)
		}
		s := &Song{Name: CleanName(js.Name, 12), LoopTo: js.LoopTo}
		for _, r := range js.Rows {
			if len(s.Rows) >= MaxSongRows {
				break
			}
			s.Rows = append(s.Rows, SongRow{Pattern: clampi(r.Pattern, 0, NumPatterns-1),
				Repeats: clampi(r.Repeats, 1, 99), Trans: clampi(r.Trans, -24, 24)})
		}
		if s.LoopTo >= len(s.Rows) || s.LoopTo < -1 {
			s.LoopTo = -1
		}
		p.Songs[js.N] = s
	}
	return p, nil
}

// MarshalProject encodes the engine's project safely while it runs and
// returns the user waves for the host to write as files.
func (e *Engine) MarshalProject() ([]byte, [NumUserWave]*UserWave, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.proj.Tempo = e.bpm
	w := *e.kit
	e.proj.Working, e.proj.WorkingN = &w, e.kitN
	b, err := e.proj.Marshal()
	e.proj.Working = nil
	return b, e.proj.Waves, err
}
