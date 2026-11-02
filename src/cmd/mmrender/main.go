// Command mmrender renders MonoModule's engine to a WAV file without any
// hardware: a demo pattern, a tour of every machine, or a project file.
//
//	mmrender -o demo.wav            # the demo song
//	mmrender -tour -o tour.wav      # every machine plays one note
//	mmrender -project p.json -secs 20 -o p.wav
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"monomodule/engine"
)

const rate = 44100

func main() {
	out := flag.String("o", "demo.wav", "output WAV")
	secs := flag.Float64("secs", 32, "seconds to render")
	tour := flag.Bool("tour", false, "play one note on every machine")
	proj := flag.String("project", "", "render this project.json instead of the demo")
	flag.Parse()
	e := engine.New(rate)
	var pcm []float32
	switch {
	case *tour:
		pcm = renderTour(e)
	default:
		if *proj != "" {
			b, err := os.ReadFile(*proj)
			check(err)
			p, err := engine.UnmarshalProject(b)
			check(err)
			e.SetProject(p)
		} else {
			buildDemo(e)
		}
		e.Play()
		pcm = render(e, int(*secs*rate))
	}
	check(os.WriteFile(*out, engine.EncodeWAV16(pcm, rate, 2), 0o644))
	var peak, sum float64
	clipped := 0
	for _, v := range pcm {
		a := math.Abs(float64(v))
		if a > peak {
			peak = a
		}
		if a >= 0.999 {
			clipped++
		}
		sum += a * a
	}
	fmt.Printf("%s: %.1f s, peak %.3f, rms %.3f, clipped samples %d\n", *out, float64(len(pcm))/2/rate, peak, math.Sqrt(sum/float64(len(pcm))), clipped)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// render returns interleaved stereo.
func render(e *engine.Engine, n int) []float32 {
	pcm := make([]float32, 0, n*2)
	l, r := make([]float32, 256), make([]float32, 256)
	for done := 0; done < n; done += 256 {
		e.Render(l, r)
		for i := range l {
			pcm = append(pcm, l[i], r[i])
		}
	}
	return pcm
}

func renderTour(e *engine.Engine) []float32 {
	var pcm []float32
	for m, mc := range engine.Machines {
		if mc.FX || m == 0 {
			continue
		}
		e.SetMachine(0, m)
		e.SetParam(0, engine.HOLD, 40)
		e.SetParam(0, engine.DEC, 70)
		e.SetParam(0, engine.REL, 50)
		e.NoteOn(0, 48, 0.9)
		pcm = append(pcm, render(e, rate)...)
		e.NoteOff(0, 48)
		pcm = append(pcm, render(e, rate/4)...)
		fmt.Printf("%-10s ok\n", mc.Name)
	}
	return pcm
}

// buildDemo makes a four-pattern groove that touches most of the instrument:
// a filtered saw bass with slides and locks, an arpeggiated pulse lead, an
// ensemble pad chord, FM bells and a beat box kit, with reverb on the pad
// and bells, played as a song.
func buildDemo(e *engine.Engine) {
	e.SetTempo(112)
	e.SetMachine(5, engine.MachineIndex("FX-REVERB"))
	e.SetTrackOptions(5, func(ts engine.TrackSetup) engine.TrackSetup { ts.Src = 1<<2 | 1<<3; return ts })
	e.SetParam(5, 0, 90)
	// bass
	e.SetParam(0, engine.WDTH, 60)
	e.SetParam(0, engine.BOFS, 100)
	e.SetParam(0, engine.FDEC, 50)
	e.SetParam(0, engine.HOLD, 22)
	e.SetParam(0, engine.DEC, 60)
	// lead
	e.SetParam(1, engine.HOLD, 30)
	e.SetParam(1, engine.DEC, 70)
	e.SetParam(1, engine.WDTH, 90)
	e.SetTrackOptions(1, func(ts engine.TrackSetup) engine.TrackSetup {
		ts.Arp = engine.ArpSetup{Mode: engine.ArpUpDown, Speed: 2, Range: 2, Len: 80}
		return ts
	})
	e.SetParam(1, engine.PAN, 80)
	// pad
	e.SetParam(2, engine.ATK, 50)
	e.SetParam(2, engine.HOLD, 127)
	e.SetParam(2, engine.REL, 70)
	e.SetParam(2, engine.WDTH, 80)
	e.SetParam(2, engine.VOL, 70)
	// bells
	e.SetParam(3, engine.HOLD, 20)
	e.SetParam(3, engine.DEC, 80)
	e.SetParam(3, engine.VOL, 80)
	// drums
	e.SetParam(4, engine.VOL, 105)
	e.SetTrackOptions(4, func(ts engine.TrackSetup) engine.TrackSetup { ts.TransFix = true; return ts })
	// the last track is the reverb; give track 6 no notes.

	type pt struct{ s, n int }
	bass := [][]pt{
		{{0, 36}, {3, 36}, {6, 39}, {8, 36}, {11, 43}, {14, 41}},
		{{0, 34}, {3, 34}, {6, 37}, {8, 34}, {11, 41}, {14, 39}},
		{{0, 32}, {3, 32}, {6, 35}, {8, 32}, {11, 39}, {14, 37}},
		{{0, 34}, {3, 34}, {6, 37}, {8, 41}, {11, 39}, {14, 37}},
	}
	chords := [][]int{{60, 63, 67, 70}, {58, 62, 65, 69}, {56, 60, 63, 67}, {58, 62, 65, 69}}
	bells := [][]pt{{{2, 79}, {7, 82}, {12, 75}}, {{2, 77}, {7, 81}, {12, 74}}, {{2, 75}, {7, 79}, {12, 72}}, {{2, 77}, {7, 81}, {12, 86}}}
	for p := 0; p < 4; p++ {
		e.EditPattern(p, func(x *engine.Pattern) { *x = *engine.NewPattern() })
		e.SelectPattern(p, true)
		for i, b := range bass[p] {
			e.SetStepNote(0, b.s, b.n, false)
			if i%2 == 1 {
				e.LockParam(0, b.s, engine.WDTH, 85)
			}
		}
		e.ToggleSlide(6)
		e.ToggleSlide(11)
		c := chords[p]
		e.SetStepNote(1, 0, c[0], false)
		e.SetStepNote(1, 8, c[2], false)
		e.SetStepNote(1, 0, c[1], true)
		e.EditStep(1, 0, func(s *engine.Step) { s.Len = 60 })
		e.EditStep(1, 8, func(s *engine.Step) { s.Len = 60 })
		e.EditCurrent(func(x *engine.Pattern) { x.Poly = false })
		e.SetStepNote(2, 0, c[0]-12, false)
		for _, n := range c[1:] {
			e.SetStepNote(2, 0, n-12, true)
		}
		e.EditStep(2, 0, func(s *engine.Step) { s.Len = 127 })
		for _, b := range bells[p] {
			e.SetStepNote(3, b.s, b.n, false)
		}
		for _, s := range []int{0, 4, 8, 12} {
			e.SetStepNote(4, s, 36, false)
			e.LockParam(4, s, 0, 0)
		}
		for _, s := range []int{2, 6, 10, 14} {
			e.SetStepNote(4, s, 36, false)
			e.LockParam(4, s, 0, 40)
		}
		for _, s := range []int{4, 12} {
			e.SetStepNote(4, s, 36, false)
			e.LockParam(4, s, 0, 20)
		}
		if p == 3 {
			e.ToggleSwing(5)
			e.EditCurrent(func(x *engine.Pattern) { x.SwingAmt = 62 })
		}
	}
	e.EditSong(0, func(s *engine.Song) {
		for p := 0; p < 4; p++ {
			s.Rows = append(s.Rows, engine.SongRow{Pattern: p, Repeats: 2})
		}
		s.LoopTo = 0
	})
	e.SetSong(true, 0)
}
