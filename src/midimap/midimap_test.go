package midimap

import "testing"

func TestNoteTrack(t *testing.T) {
	if tr, ok := NoteTrack(0, 0); !ok || tr != 0 {
		t.Fatal("ch1")
	}
	if tr, ok := NoteTrack(7, 2); !ok || tr != 5 {
		t.Fatal("ch8 base3")
	}
	if _, ok := NoteTrack(1, 2); ok {
		t.Fatal("below base")
	}
	if _, ok := NoteTrack(8, 2); ok {
		t.Fatal("above")
	}
}

func TestTrackParam(t *testing.T) {
	if tr, p, ok := TrackParam(1, 16, 0); !ok || tr != 1 || p != 0 {
		t.Fatal("first")
	}
	if _, p, ok := TrackParam(0, 71, 0); !ok || p != 55 {
		t.Fatal("last")
	}
	if _, _, ok := TrackParam(0, 72, 0); ok {
		t.Fatal("72")
	}
	if _, _, ok := TrackParam(0, 15, 0); ok {
		t.Fatal("15")
	}
}

func TestBend(t *testing.T) {
	if Bend(8192) != 0 || Bend(16383) < 1.99 || Bend(0) != -2 {
		t.Fatal("bend")
	}
	if Tempo(90) != 120 {
		t.Fatal("tempo")
	}
}
