// Package livesync keeps the sequencer in step with Live's transport and tempo. MIDI
// Start and Stop only reach MonoModule while it listens; if Live was
// already playing when MonoModule came up (or the sync mode was switched
// to MIDI mid-song), or a Stop got lost, the sequencer would sit still while
// the clock pulses keep coming. The PushHack Remote Script says whether Live
// plays and where; the Follower joins or stops on a change. It only acts when
// Live's state changes, so it never fights a start the user made by hand.
package livesync

import (
	"log"
	"time"

	"monomodule/engine"
	"monomodule/livelink"
)

const (
	pollEvery  = 250 * time.Millisecond
	retryEvery = 5 * time.Second // the Remote Script is not there (Live closed)
)

// Clock is the part of the engine the Follower drives.
type Clock interface {
	Sync() engine.SyncMode
	Playing() bool
	ExtJoin(beat float64)
	LiveTempo(bpm float64, now time.Time)
	ExtStop()
}

// Follower turns changes of Live's transport into engine calls.
type Follower struct {
	Eng   Clock
	known bool // seen Live's state since sync was last switched to MIDI
	live  bool // Live's state at the last poll
}

// Step handles one answer. rtt is how long the question took: Live's beat is
// from about half of that ago.
func (f *Follower) Step(t livelink.Transport, rtt time.Duration) {
	if f.Eng.Sync() != engine.SyncMIDI {
		f.known = false // look again when MIDI sync is switched on
		return
	}
	// Live's tempo follows at once, playing or not: the clock pulses are
	// only there while Live plays, and only if Live sends them to this port.
	f.Eng.LiveTempo(t.Tempo, time.Now())
	first := !f.known
	rising := t.Playing && (first || !f.live)
	falling := !t.Playing && !first && f.live
	f.known, f.live = true, t.Playing
	switch {
	case rising && !f.Eng.Playing():
		beat := t.Beat
		if t.Tempo > 0 {
			beat += rtt.Seconds() / 2 * t.Tempo / 60
		}
		f.Eng.ExtJoin(beat)
	case falling && f.Eng.Playing():
		f.Eng.ExtStop()
	}
}

// Run polls Live until shutdown closes. While the Remote Script does not
// answer it asks every few seconds and says so once.
func Run(shutdown <-chan struct{}, call livelink.Caller, eng Clock) {
	f := &Follower{Eng: eng}
	wait, down := pollEvery, false
	for {
		start := time.Now()
		t, err := livelink.ReadTransport(call)
		switch {
		case err != nil && !down:
			log.Printf("live sync: %v; asking again every %v", err, retryEvery)
			down, wait, f.known = true, retryEvery, false
		case err == nil:
			if down {
				log.Print("live sync: Live's transport is back")
				down, wait = false, pollEvery
			}
			f.Step(t, time.Since(start))
		}
		select {
		case <-shutdown:
			return
		case <-time.After(wait):
		}
	}
}
