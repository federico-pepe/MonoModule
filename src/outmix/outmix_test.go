package outmix

import "testing"

func TestFit(t *testing.T) {
	for _, c := range []struct {
		mode    string
		off, ch int
		wantM   string
		wantO   int
	}{
		{Stereo, 2, 8, Stereo, 2},
		{Stereo, 7, 8, Stereo, 0}, // a pair running off the card
		{Mono, 7, 8, Mono, 7},     // mono on the last channel fits
		{Mono, 8, 8, Mono, 0},
		{"bogus", 1, 8, Stereo, 1},
		{Stereo, 0, 1, Mono, 0}, // 1-channel card
		{Stereo, -3, 8, Stereo, 0},
	} {
		if m, o := Fit(c.mode, c.off, c.ch); m != c.wantM || o != c.wantO {
			t.Errorf("Fit(%q,%d,%d) = %q,%d want %q,%d", c.mode, c.off, c.ch, m, o, c.wantM, c.wantO)
		}
	}
}

func filled(n int) []int16 {
	w := make([]int16, n)
	for i := range w {
		w[i] = 9
	}
	return w
}

func TestStereoLandsOnTwoChannels(t *testing.T) {
	l, r := []float32{0.5, -0.5}, []float32{0.25, 1}
	w := filled(2 * 6)
	Mix(w, l, r, 6, 2, Stereo)
	for f := 0; f < 2; f++ {
		for c := 0; c < 6; c++ {
			v := w[f*6+c]
			switch c {
			case 2:
				if v != s16(l[f]) {
					t.Fatalf("L frame %d = %d", f, v)
				}
			case 3:
				if v != s16(r[f]) {
					t.Fatalf("R frame %d = %d", f, v)
				}
			default:
				if v != 0 {
					t.Fatalf("channel %d not silent: %d", c, v)
				}
			}
		}
	}
}

func TestMonoSumsAndClears(t *testing.T) {
	l, r := []float32{0.5, 0.2}, []float32{0.1, -0.2}
	w := filled(2 * 4)
	Mix(w, l, r, 4, 2, Mono)
	if w[2] != s16(0.3) || w[6] != 0 {
		t.Fatalf("mono: %v", w)
	}
	for f := 0; f < 2; f++ {
		for _, c := range []int{0, 1, 3} {
			if w[f*4+c] != 0 {
				t.Fatalf("channel %d not cleared: %v", c, w)
			}
		}
	}
}

func TestClipping(t *testing.T) {
	w := make([]int16, 2)
	Mix(w, []float32{3}, []float32{-3}, 2, 0, Stereo)
	if w[0] != 32767 || w[1] != -32768 {
		t.Fatalf("%v", w)
	}
}

func TestWidthValid(t *testing.T) {
	if Width(Mono) != 1 || Width(Stereo) != 2 || Width("x") != 2 || Valid("x") || !Valid(Mono) {
		t.Fatal()
	}
}
