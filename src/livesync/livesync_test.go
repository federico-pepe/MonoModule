package livesync

import (
	"encoding/json"
	"testing"
	"time"

	"monomodule/engine"
	"monomodule/livelink"
)

type fakeClock struct {
	sync    engine.SyncMode
	playing bool
	joined  []float64
	tempo   float64
	stops   int
}

func (c *fakeClock) Sync() engine.SyncMode            { return c.sync }
func (c *fakeClock) Playing() bool                    { return c.playing }
func (c *fakeClock) ExtJoin(b float64)                { c.joined = append(c.joined, b); c.playing = true }
func (c *fakeClock) LiveTempo(b float64, _ time.Time) { c.tempo = b }
func (c *fakeClock) ExtStop()                         { c.stops++; c.playing = false }

func live(playing bool, beat float64) livelink.Transport {
	return livelink.Transport{Playing: playing, Tempo: 120, Beat: beat}
}

func TestJoinsWhenLiveIsAlreadyPlaying(t *testing.T) {
	c := &fakeClock{sync: engine.SyncMIDI}
	f := &Follower{Eng: c}
	f.Step(live(true, 10), 100*time.Millisecond)
	if len(c.joined) != 1 {
		t.Fatalf("joins = %v", c.joined)
	}
	// 120 BPM, half of 100 ms is 0.05 s = 0.1 beat
	if got := c.joined[0]; got < 10.09 || got > 10.11 {
		t.Fatalf("joined at %g, want about 10.1", got)
	}
	f.Step(live(true, 11), 0)
	if len(c.joined) != 1 {
		t.Fatal("joined twice")
	}
}

func TestStopsWhenLiveStops(t *testing.T) {
	c := &fakeClock{sync: engine.SyncMIDI}
	f := &Follower{Eng: c}
	f.Step(live(true, 0), 0)
	f.Step(live(false, 4), 0)
	if c.stops != 1 || c.playing {
		t.Fatalf("stops=%d playing=%v", c.stops, c.playing)
	}
}

func TestLeavesAHandStartedSequencerAlone(t *testing.T) {
	c := &fakeClock{sync: engine.SyncMIDI, playing: true}
	f := &Follower{Eng: c}
	f.Step(live(false, 0), 0) // first look: Live stopped, no change seen
	f.Step(live(false, 0), 0)
	if c.stops != 0 || !c.playing {
		t.Fatalf("stopped a hand-started sequencer: stops=%d", c.stops)
	}
}

func TestIgnoresLiveWithInternalSync(t *testing.T) {
	c := &fakeClock{sync: engine.SyncInternal}
	f := &Follower{Eng: c}
	f.Step(live(true, 3), 0)
	if len(c.joined) != 0 {
		t.Fatal("joined with internal sync")
	}
	// switching to MIDI while Live plays joins at once
	c.sync = engine.SyncMIDI
	f.Step(live(true, 3), 0)
	if len(c.joined) != 1 {
		t.Fatalf("joins after switching to MIDI = %v", c.joined)
	}
}

func TestReadTransport(t *testing.T) {
	call := func(ns, cmd string, args map[string]any) (json.RawMessage, error) {
		if ns != "transport" || cmd != "get" {
			t.Fatalf("asked %s.%s", ns, cmd)
		}
		return json.RawMessage(`{"playing":true,"tempo":98.5,"beat":12.25,"signature":[4,4]}`), nil
	}
	tr, err := livelink.ReadTransport(call)
	if err != nil || !tr.Playing || tr.Tempo != 98.5 || tr.Beat != 12.25 {
		t.Fatalf("%+v %v", tr, err)
	}
}

func TestFollowsLivesTempo(t *testing.T) {
	c := &fakeClock{sync: engine.SyncMIDI}
	f := &Follower{Eng: c}
	f.Step(livelink.Transport{Tempo: 98.5}, 0)
	if c.tempo != 98.5 {
		t.Fatalf("tempo = %g", c.tempo)
	}
	f.Step(livelink.Transport{Tempo: 140}, 0)
	if c.tempo != 140 {
		t.Fatalf("tempo after a change = %g", c.tempo)
	}
	// with internal sync the tempo is left alone
	c.sync, c.tempo = engine.SyncInternal, 0
	f.Step(livelink.Transport{Tempo: 90}, 0)
	if c.tempo != 0 {
		t.Fatal("followed the tempo with internal sync")
	}
}
