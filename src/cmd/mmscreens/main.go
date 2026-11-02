// Command mmscreens draws Push 3 screens of MonoModule's front panel to PNG
// files, from the same code the device runs.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"monomodule/engine"
	"monomodule/ui"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

type host struct{}

func (host) ImportFiles() []string        { return []string{"kick.wav", "vox.wav"} }
func (host) ImportWave(int, string) error { return nil }
func (host) SaveProject() error           { return nil }
func (host) AudioStatus() (bool, string)  { return true, "" }
func (host) SyncChanged(engine.SyncMode)  {}

func main() {
	dir := flag.String("o", "screens", "output folder")
	flag.Parse()
	check(os.MkdirAll(*dir, 0o755))
	e := engine.New(44100)
	c := ui.New(e, host{})
	e.SetWave(2, "Choir", 44100, sine(8000))
	e.SetTempo(112)
	e.Play()
	l, r := make([]float32, 4096), make([]float32, 4096)
	for i := 0; i < 6; i++ {
		e.Render(l, r)
	}
	e.NoteOn(0, 40, 1)
	e.Render(l, r)
	shot := func(name string, page, sub int) {
		c.SetView(page, sub)
		f, err := os.Create(filepath.Join(*dir, name+".png"))
		check(err)
		check(png.Encode(f, c.Render()))
		f.Close()
		fmt.Println(name)
	}
	shot("push-syn", ui.PgSyn, 0)
	shot("push-amp", ui.PgAmp, 0)
	shot("push-flt", ui.PgFlt, 0)
	shot("push-fx", ui.PgFx, 0)
	shot("push-lfo1", ui.PgLfo, 0)
	shot("push-seq", ui.PgSeq, 0)
	shot("push-arp", ui.PgSeq, 1)
	shot("push-kit", ui.PgKit, 0)
	shot("push-master", ui.PgKit, 1)
	e.EditSong(0, func(s *engine.Song) {
		s.Rows = []engine.SongRow{{Pattern: 0, Repeats: 2}, {Pattern: 1, Repeats: 1, Trans: 3}, {Pattern: 0, Repeats: 4}}
	})
	shot("push-song", ui.PgSong, 0)
	shot("push-system", ui.PgSys, 0)
	// the machine list, and a held step
	c.SetView(ui.PgSyn, 0)
	c.Button(push3.CCScreenBot1, true)
	c.Encoder(push3.CCEncoder1, 8)
	f, _ := os.Create(filepath.Join(*dir, "push-machines.png"))
	check(png.Encode(f, c.Render()))
	f.Close()
	c.Button(push3.CCUndo, true)
	c.SetView(ui.PgSeq, 0)
	c.Pad(push3.PadNote(3, 1), 100)
	f, _ = os.Create(filepath.Join(*dir, "push-step.png"))
	check(png.Encode(f, c.Render()))
	f.Close()
}

func sine(n int) []float32 {
	d := make([]float32, n)
	for i := range d {
		d[i] = float32(i%100) / 50
		d[i] -= 1
	}
	return d
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
