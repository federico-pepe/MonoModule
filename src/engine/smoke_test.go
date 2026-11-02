package engine

import (
	"math"
	"testing"
)

func rms(l, r []float32) float64 {
	var s float64
	for i := range l {
		s += float64(l[i]*l[i] + r[i]*r[i])
	}
	return math.Sqrt(s / float64(2*len(l)))
}

func TestAllMachinesAudible(t *testing.T) {
	for m := range Machines {
		if Machines[m].FX || m == mGndEmpty {
			continue
		}
		e := New(44100)
		e.SetMachine(0, m)
		e.NoteOn(0, 48, 1)
		l, r := make([]float32, 4410), make([]float32, 4410)
		e.Render(l, r)
		for i := range l {
			if math.IsNaN(float64(l[i])) || math.IsInf(float64(l[i]), 0) || math.IsNaN(float64(r[i])) {
				t.Fatalf("%s: non-finite at %d", Machines[m].Name, i)
			}
		}
		if rms(l, r) < 1e-4 {
			t.Errorf("%s: silent (rms %g)", Machines[m].Name, rms(l, r))
		}
	}
}

func TestDemoPlays(t *testing.T) {
	e := New(44100)
	e.Play()
	l, r := make([]float32, 44100*2), make([]float32, 44100*2)
	for i := 0; i < 4; i++ {
		e.Render(l, r)
	}
	if rms(l, r) < 1e-3 {
		t.Fatal("demo pattern is silent")
	}
}
