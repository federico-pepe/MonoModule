package store

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"monomodule/engine"
)

func sine(n int) []float32 {
	d := make([]float32, n)
	for i := range d {
		d[i] = float32(math.Sin(float64(i) * 0.05))
	}
	return d
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	e := engine.New(44100)
	s, err := Open(dir, e)
	if err != nil {
		t.Fatal(err)
	}
	e.SetStepNote(3, 4, 50, false)
	if err := e.SetWave(5, "My Hit", 22050, sine(4000)); err != nil {
		t.Fatal(err)
	}
	e.SetParam(1, 2, 77)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	e2 := engine.New(44100)
	s2, _ := Open(dir, e2)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if stepNote(e2) != 50 {
		t.Fatal("trig lost")
	}
	w := e2.Wave(5)
	if w == nil || w.Rate != 22050 || len(w.Data) != 4000 || w.Name == "" {
		t.Fatalf("wave lost: %+v", w)
	}
	if e2.Param(1, 2) != 77 {
		t.Fatal("working kit lost")
	}
	// clearing a slot removes its file
	e2.ClearWave(5)
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "waves", "06.wav")); !os.IsNotExist(err) {
		t.Fatal("wave file should be gone")
	}
}

func TestDamagedProjectKept(t *testing.T) {
	dir := t.TempDir()
	e := engine.New(44100)
	s, _ := Open(dir, e)
	os.WriteFile(filepath.Join(dir, "project.json"), []byte("{not json"), 0o644)
	if err := s.Load(); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "project.json.bad")); err != nil {
		t.Fatal("bad file not kept")
	}
}

func TestSafeName(t *testing.T) {
	for _, bad := range []string{"../x.wav", "a.mp3", ".hidden.wav", ""} {
		n, err := SafeName(bad)
		if bad == "../x.wav" {
			if err != nil || n != "x.wav" {
				t.Fatalf("%q -> %q %v", bad, n, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestUploadAndImport(t *testing.T) {
	dir := t.TempDir()
	e := engine.New(44100)
	s, _ := Open(dir, e)
	wav := engine.EncodeWAV16(sine(3000), 44100, 1)
	if err := s.UploadWave(0, "kick.wav", bytes.NewReader(wav)); err != nil {
		t.Fatal(err)
	}
	if got := s.ImportFiles(); len(got) != 1 || got[0] != "kick.wav" {
		t.Fatalf("imports %v", got)
	}
	if err := s.ImportWave(1, "kick.wav"); err != nil {
		t.Fatal(err)
	}
	if e.Wave(1) == nil {
		t.Fatal("not loaded")
	}
	if err := s.ImportWave(1, "nothere.wav"); err == nil {
		t.Fatal("missing file accepted")
	}
	if err := s.UploadWave(2, "x.wav", bytes.NewReader([]byte("junk"))); err == nil {
		t.Fatal("junk accepted")
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadConfig(dir)
	if err != nil || c.PCMDevice == "" || c.MidiChannel != -1 {
		t.Fatalf("%+v %v", c, err)
	}
	c.ChannelOffset = 2
	if err := SaveConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	c2, _ := LoadConfig(dir)
	if c2.ChannelOffset != 2 {
		t.Fatal("not saved")
	}
}

func stepNote(e *engine.Engine) (n int) {
	e.ReadPattern(0, func(p *engine.Pattern) {
		if st := p.Tracks[3].Step[4]; st.On {
			n = int(st.Note)
		}
	})
	return
}

func TestConfigOldFileIsStereo(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, ConfigFile), []byte(`{"pcm_device":"hw:PHVAudio,1,0","channel_offset":2}`), 0o644)
	c, err := LoadConfig(dir)
	if err != nil || c.OutputMode != "stereo" || c.ChannelOffset != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	_ = os.WriteFile(filepath.Join(dir, ConfigFile), []byte(`{"output_mode":"weird","channel_offset":99}`), 0o644)
	if c, _ = LoadConfig(dir); c.OutputMode != "stereo" || c.ChannelOffset != 0 {
		t.Fatalf("%+v", c)
	}
	_ = os.WriteFile(filepath.Join(dir, ConfigFile), []byte(`{"output_mode":"mono","channel_offset":63}`), 0o644)
	if c, _ = LoadConfig(dir); c.OutputMode != "mono" || c.ChannelOffset != 63 {
		t.Fatalf("%+v", c)
	}
}
