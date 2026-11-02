package ui

import (
	"testing"
	"time"

	"monomodule/engine"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

type fakeHost struct{ saved int }

func (h *fakeHost) ImportFiles() []string        { return []string{"a.wav"} }
func (h *fakeHost) ImportWave(int, string) error { return nil }
func (h *fakeHost) SaveProject() error           { h.saved++; return nil }
func (h *fakeHost) AudioStatus() (bool, string)  { return true, "" }
func (h *fakeHost) SyncChanged(engine.SyncMode)  {}

func newC() (*Controller, *engine.Engine, *fakeHost) {
	e := engine.New(44100)
	e.ClearPattern(0)
	h := &fakeHost{}
	c := New(e, h)
	t0 := time.Unix(1000, 0)
	c.SetClock(func() time.Time { return t0 })
	return c, e, h
}

func pp(col, row int) byte { return push3.PadNote(col, row) }

func stepOn(e *engine.Engine, t, s int) (on bool) {
	e.ReadCurrent(func(p *engine.Pattern) { on = p.Tracks[t].Step[s].On })
	return
}

func TestStepToggle(t *testing.T) {
	c, e, _ := newC()
	c.Pad(pp(2, 1), 100) // step 3
	c.Pad(pp(2, 1), 0)
	if !stepOn(e, 0, 2) {
		t.Fatal("step not set")
	}
	c.Pad(pp(2, 1), 100)
	c.Pad(pp(2, 1), 0) // second tap removes it
	if stepOn(e, 0, 2) {
		t.Fatal("step not removed")
	}
	c.Pad(pp(0, 0), 100) // step 9
	c.Pad(pp(0, 0), 0)
	if !stepOn(e, 0, 8) {
		t.Fatal("row 0 is steps 9-16")
	}
}

func TestHeldStepTakesNoteAndLocks(t *testing.T) {
	c, e, _ := newC()
	c.Pad(pp(0, 1), 100)
	c.Pad(pp(3, 5), 100) // keyboard: base 48 + 3
	c.Pad(pp(3, 5), 0)
	c.Button(push3.CCScreenTop2, true) // AMP page
	c.Encoder(push3.CCEncoder1, 7)
	c.Pad(pp(0, 1), 0)
	e.ReadCurrent(func(p *engine.Pattern) {
		if p.Tracks[0].Step[0].Note != 51 {
			t.Errorf("note %d", p.Tracks[0].Step[0].Note)
		}
		if l := p.Tracks[0].Lock[0]; l == nil || !l.Has(engine.ATK) {
			t.Error("no lock")
		}
	})
	if !stepOn(e, 0, 0) {
		t.Fatal("edited step was removed on release")
	}
}

func TestKeyboardPlaysAndReleases(t *testing.T) {
	c, e, _ := newC()
	c.Pad(pp(0, 5), 100)
	l, r := make([]float32, 2048), make([]float32, 2048)
	e.Render(l, r)
	if e.ActiveVoices() != 1 {
		t.Fatal("no voice")
	}
	c.Pad(pp(0, 5), 0)
	if len(c.sounding) != 0 {
		t.Fatal("pad still sounding")
	}
}

func TestPagesAndSubpages(t *testing.T) {
	c, _, _ := newC()
	c.Button(push3.CCScreenTop5, true)
	c.Button(push3.CCScreenTop5, true)
	if c.page != PgLfo || c.sub != 1 {
		t.Fatalf("page %d sub %d", c.page, c.sub)
	}
	if c.paramIndex(0) != engine.LFOBase(1) {
		t.Fatal("LFO2 index")
	}
	c.Button(push3.CCSettings, true)
	if c.page != PgSys {
		t.Fatal("settings")
	}
}

func TestMachineOverlay(t *testing.T) {
	c, e, _ := newC()
	c.Button(push3.CCScreenBot1, true)
	c.Encoder(push3.CCEncoder1, 8)
	c.Button(push3.CCJogPress, true)
	if e.Machine(0) != 11 {
		t.Fatalf("machine %d", e.Machine(0))
	}
}

func TestPolyKnobAndPatternSelect(t *testing.T) {
	c, e, _ := newC()
	c.Button(push3.CCScreenTop6, true)
	c.Encoder(push3.CCEncoder6, 6)
	e.ReadCurrent(func(p *engine.Pattern) {
		if !p.Poly {
			t.Error("poly not set")
		}
	})
	c.Pad(pp(3, 4), 100) // pattern 4 of bank A, row 4 = first 8
	if e.CurrentPattern() != 3 {
		t.Fatalf("pattern %d", e.CurrentPattern())
	}
}

func TestMuteAndTrackSelect(t *testing.T) {
	c, e, _ := newC()
	c.Pad(pp(3, 2), 100)
	if c.Track() != 3 {
		t.Fatal("track")
	}
	c.Button(push3.CCMute, true)
	c.Pad(pp(1, 2), 100)
	if !e.Muted(1) {
		t.Fatal("mute")
	}
}

func TestRenderAndLEDsAreSane(t *testing.T) {
	c, _, _ := newC()
	for p := 0; p < numPages; p++ {
		for s := 0; s < subCount(p); s++ {
			c.SetView(p, s)
			img := c.Render()
			if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
				t.Fatal("size")
			}
			b, pads := c.LEDs()
			if len(b) == 0 || pads[0*8+0] == 0 && pads[7*8+0] == 0 {
				t.Fatalf("page %d empty LEDs", p)
			}
		}
	}
	if NoteName(60) != "C-3" {
		t.Fatal(NoteName(60))
	}
}

func TestOnlyASCII(t *testing.T) {
	for i := range pageNames {
		for _, r := range pageNames[i] {
			if r > 126 {
				t.Fatal("non-ascii")
			}
		}
	}
	for _, m := range engine.Machines {
		for _, r := range m.Name {
			if r > 126 || r < 32 {
				t.Fatal("non-ascii machine name")
			}
		}
	}
}
