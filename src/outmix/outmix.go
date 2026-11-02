// Package outmix puts MonoModule's stereo output on the channels of the
// Push Hack Virtual Audio card: a stereo pair or a single mono channel, at
// any offset, so several modules can share the card. Plain Go, no
// allocations.
package outmix

// Output modes.
const (
	Stereo = "stereo"
	Mono   = "mono"
)

// Valid reports whether mode is a known mode.
func Valid(mode string) bool { return mode == Stereo || mode == Mono }

// Width is how many channels the mode uses.
func Width(mode string) int {
	if mode == Mono {
		return 1
	}
	return 2
}

// Fit keeps the choice inside the card Live opened: unknown mode becomes
// stereo, stereo on a 1-channel card becomes mono, an offset that does not
// fit becomes 0.
func Fit(mode string, offset, channels int) (string, int) {
	if !Valid(mode) {
		mode = Stereo
	}
	if mode == Stereo && channels < 2 {
		mode = Mono
	}
	if offset < 0 || offset+Width(mode) > channels {
		offset = 0
	}
	return mode, offset
}

// Mix fills interleaved frames: L and R on two neighbouring channels, or
// (L+R)/2 on one, and silence everywhere else. len(wide) must be
// len(l)*channels; offset must come from Fit.
func Mix(wide []int16, l, r []float32, channels, offset int, mode string) {
	for i := range wide {
		wide[i] = 0
	}
	mono := mode == Mono
	for f := range l {
		base := f*channels + offset
		if mono {
			wide[base] = s16((l[f] + r[f]) * 0.5)
		} else {
			wide[base] = s16(l[f])
			wide[base+1] = s16(r[f])
		}
	}
}

func s16(v float32) int16 {
	v *= 32767
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	}
	return int16(v)
}
