// Package midimap says what incoming MIDI means to MonoModule. Plain Go.
package midimap

import "monomodule/engine"

// Each track listens on its own MIDI channel: track 1 on the base channel,
// track 2 on the next, up to track 6. Notes are played at their pitch.

// NoteTrack maps a channel to a track. base is the first channel (0-15).
func NoteTrack(channel, base int) (int, bool) {
	t := channel - base
	return t, t >= 0 && t < engine.NumTracks
}

// A track's 56 parameters take CCs 16-71 in order of parameter index, on the
// track's channel.
const (
	FirstCC = 16
	LastCC  = FirstCC + engine.NParams - 1
)

// TrackParam maps a CC on a channel to a track parameter.
func TrackParam(channel, cc, base int) (track, param int, ok bool) {
	t, okT := NoteTrack(channel, base)
	if !okT || cc < FirstCC || cc > LastCC {
		return
	}
	return t, cc - FirstCC, true
}

// Global CCs on the base channel.
const (
	CCMasterVol = 112
	CCTempo     = 113 // 30-157 BPM
	CCMuteAll   = 114 // 127 mutes every track, 0 unmutes
	CCAllOff    = 123 // all notes off (any channel)
)

// Tempo turns a CC value into BPM.
func Tempo(v int) float64 { return 30 + float64(v) }

// Bend turns a 14 bit pitch wheel value (0-16383, 8192 centre) into
// semitones over +-2.
func Bend(v int) float64 { return float64(v-8192) / 8192 * 2 }
