package main

import (
	"encoding/binary"
	"log"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/push3"

	"monomodule/engine"
	"monomodule/livelink"
	"monomodule/midimap"
)

const midiPortName = livelink.MIDIPort

func cardPresent(id string) bool {
	cards, err := alsapcm.EnumCards()
	if err != nil {
		return false
	}
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

// midiHandler turns ALSA sequencer events into calls on the engine and the
// front panel. It runs on the read-loop goroutine; the engine and the panel
// are safe to call from there.
type midiHandler struct{ a *app }

func (h *midiHandler) Fixed(ev uint8, src alsaseq.Addr, data []byte) {
	a := h.a
	push := a.push3.Is(src)
	ch := int(data[0] & 0x0F)
	switch ev {
	case alsaseq.EvClock:
		a.eng.ExtPulse(time.Now())
		return
	case alsaseq.EvStart:
		a.eng.ExtStart()
		return
	case alsaseq.EvContinue:
		a.eng.ExtContinue()
		return
	case alsaseq.EvStop:
		a.eng.ExtStop()
		return
	}

	if push {
		// Only Push's own control port drives the panel, and only while the
		// app has the surface; everything else it sends belongs to Live.
		if !a.push3.IsLivePort(src) || !a.focus.Load() {
			return
		}
		switch ev {
		case alsaseq.EvNoteOn, alsaseq.EvNoteOff:
			vel := data[2]
			if ev == alsaseq.EvNoteOff {
				vel = 0
			}
			a.ui.Pad(data[1], vel)
		case alsaseq.EvController:
			if ch != 0 { // other channels carry per-pad expression
				return
			}
			cc := byte(binary.LittleEndian.Uint32(data[4:]) & 0x7F)
			val := byte(binary.LittleEndian.Uint32(data[8:]) & 0x7F)
			switch {
			case cc >= push3.CCEncoder1 && cc <= push3.CCEncoder8,
				cc == push3.CCJogWheel, cc == push3.CCVolume, cc == push3.CCTempo:
				a.ui.Encoder(cc, push3.DecodeRel(val))
			default:
				a.ui.Button(cc, val > 0)
			}
		}
		return
	}

	// External gear or a Live MIDI track: channel n plays track n.
	cfg := a.config()
	base := 0
	if cfg.MidiChannel >= 0 {
		base = cfg.MidiChannel
	}
	switch ev {
	case alsaseq.EvNoteOn, alsaseq.EvNoteOff:
		t, ok := midimap.NoteTrack(ch, base)
		if !ok {
			return
		}
		if ev == alsaseq.EvNoteOn && data[2] > 0 {
			a.eng.NoteOn(t, int(data[1]), float32(data[2])/127)
		} else {
			a.eng.NoteOff(t, int(data[1]))
		}
	case alsaseq.EvPitchBend:
		if ch == base {
			v := int(int32(binary.LittleEndian.Uint32(data[8:]))) + 8192
			a.eng.SetBend(midimap.Bend(v))
		}
	case alsaseq.EvController:
		cc := int(binary.LittleEndian.Uint32(data[4:]) & 0x7F)
		val := int(binary.LittleEndian.Uint32(data[8:]) & 0x7F)
		if cc == midimap.CCAllOff {
			a.eng.AllNotesOff()
			return
		}
		if t, p, ok := midimap.TrackParam(ch, cc, base); ok {
			a.eng.SetParam(t, p, val)
			return
		}
		if ch != base {
			return
		}
		switch cc {
		case midimap.CCMasterVol:
			a.eng.SetMaster(engine.MVOL, val)
		case midimap.CCTempo:
			a.eng.SetTempo(midimap.Tempo(val))
		case midimap.CCMuteAll:
			for t := 0; t < engine.NumTracks; t++ {
				a.eng.SetMute(t, val >= 64)
			}
		}
	case alsaseq.EvPgmChange:
		n := int(binary.LittleEndian.Uint32(data[8:]) & 0x7F)
		a.eng.SelectPattern(n, false)
	}
}

func (h *midiHandler) VarLen(uint8, alsaseq.Addr, []byte) {}

// superviseMIDI opens the port and subscribes it to Push 3's control port,
// retrying until it works.
func (a *app) superviseMIDI(shutdown <-chan struct{}) {
	for {
		seq, err := alsaseq.Open()
		if err == nil {
			_, err = seq.CreatePort(midiPortName, alsaseq.CapWrite|alsaseq.CapSubsWrite,
				alsaseq.PortTypeMidi|alsaseq.PortTypeApp)
			if err != nil {
				seq.Close()
			}
		}
		if err != nil {
			log.Printf("opening %s: %v; will retry", midiPortName, err)
			if !sleepOr(time.Second, shutdown) {
				return
			}
			continue
		}
		go func() {
			if err := seq.ReadLoop(&midiHandler{a: a}); err != nil {
				log.Printf("%s read loop ended: %v", midiPortName, err)
			}
		}()
		log.Printf("%s open", midiPortName)
		for {
			// The client number moves with the sound cards (20, not 16, when the
			// loopback card is card 0): look Push 3 up on every attempt.
			pinned := a.push3.Refresh()
			if err := seq.Subscribe(pinned); err == nil {
				log.Printf("subscribed to Push 3 %v", pinned)
				break
			} else {
				log.Printf("subscribing to Push 3: %v; will retry", err)
			}
			if !sleepOr(2*time.Second, shutdown) {
				seq.Close()
				return
			}
		}
		<-shutdown
		seq.Close()
		return
	}
}

func sleepOr(d time.Duration, shutdown <-chan struct{}) bool {
	select {
	case <-time.After(d):
		return true
	case <-shutdown:
		return false
	}
}
