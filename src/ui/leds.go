package ui

import (
	"monomodule/engine"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

func pal(name string) byte {
	i, ok := push3.ColorByName(name)
	if !ok {
		panic("ui: unknown palette colour " + name)
	}
	return i
}

var (
	ledOff   = byte(0)
	ledWhite = pal("white")
	ledDim   = pal("dgray")
	ledGreen = pal("green")
	ledAmber = pal("amber")
	ledBlue  = pal("blue")

	trackColors = [engine.NumTracks]byte{pal("lime"), pal("green"), pal("teal"), pal("sky"), pal("amber"), pal("orange")}
	trackDim    = [engine.NumTracks]byte{pal("olive"), pal("forest"), pal("teal"), pal("cobalt"), pal("sienna"), pal("maroon")}
	pageColors  = [numPages]byte{pal("lime"), pal("green"), pal("teal"), pal("sky"), pal("violet"), pal("amber"), pal("orange"), pal("pink"), pal("white")}
)

// LEDs says what every lit control shows. Buttons maps a CC to a palette
// index (RGB buttons) or a brightness (white buttons); pads is indexed
// row*8+col from the bottom left, as the pad notes are.
func (c *Controller) LEDs() (buttons map[byte]byte, pads [64]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := map[byte]byte{}
	pos := c.eng.Position()
	pat := c.pattern()

	for i := 0; i < numPages-1; i++ {
		v := ledDim
		if i == c.page {
			v = pageColors[i]
		}
		b[push3.CCScreenTopN(i)] = v
	}
	if c.page == PgSys {
		b[push3.CCSettings] = 127
	} else {
		b[push3.CCSettings] = 40
	}
	for i, l := range c.softLabels() {
		if l != "" {
			b[push3.CCScreenBotN(i)] = ledWhite
		}
	}
	for i := 0; i < 8; i++ {
		v := ledDim
		if 7-i == c.bank {
			v = ledAmber
		}
		b[byte(push3.CCScene14)+byte(i)] = v
	}
	if pos.Playing {
		b[push3.CCPlay] = 127
	} else {
		b[push3.CCPlay] = 40
	}
	if c.eng.Recording() {
		b[push3.CCRecord] = 127
	} else {
		b[push3.CCRecord] = 30
	}
	on := func(cc byte, v bool) {
		if v {
			b[cc] = 127
		} else {
			b[cc] = 30
		}
	}
	on(push3.CCMute, c.muteMode)
	on(push3.CCQuantize, c.layer == layerSwing)
	on(push3.CCRepeat, c.layer == layerSlide)
	on(push3.CCDuplicate, c.cpy)
	on(push3.CCDelete, c.clear)
	on(push3.CCShift, c.shift)
	b[push3.CCSave] = 40
	b[push3.CCTapTempo] = 40
	b[push3.CCConvert] = 40
	b[push3.CCOctaveUp], b[push3.CCOctaveDown] = 40, 40
	b[push3.CCPageLeft], b[push3.CCPageRight] = 40, 40
	b[push3.CCDPadUp], b[push3.CCDPadDown], b[push3.CCDPadLeft], b[push3.CCDPadRight] = 40, 40, 40, 40

	set := func(col, row int, v byte) { pads[row*8+col] = v }
	// steps: row 1 holds 1-8, row 0 holds 9-16
	for k := 0; k < 16; k++ {
		s := c.stepPage*16 + k
		col, row := k%8, 1-k/8
		if s >= pat.Len {
			set(col, row, ledOff)
			continue
		}
		v := ledDim
		switch c.layer {
		case layerSwing:
			if pat.Swing[s] {
				v = ledBlue
			}
		case layerSlide:
			if pat.Slide[s] {
				v = ledGreen
			}
		default:
			if pat.Tracks[c.track].Step[s].On {
				v = trackColors[c.track]
			}
		}
		if pos.Playing && pos.Step == s {
			v = ledWhite
		}
		set(col, row, v)
	}
	// tracks, row 2
	mask := c.eng.MuteMask()
	for t := 0; t < engine.NumTracks; t++ {
		v := trackDim[t]
		if t == c.track {
			v = trackColors[t]
		}
		if mask&(1<<uint(t)) != 0 {
			v = ledOff
			if t == c.track {
				v = ledDim
			}
		}
		set(t, 2, v)
	}
	// patterns, rows 3-4
	for k := 0; k < 16; k++ {
		p := c.bank*16 + k
		col, row := k%8, 4-k/8
		v := ledOff
		c.eng.ReadPattern(p, func(x *engine.Pattern) {
			if !x.Empty() {
				v = pal("cobalt")
			}
		})
		if p == pos.Next {
			v = ledAmber
		}
		if p == pos.Pattern {
			v = ledGreen
		}
		set(col, row, v)
	}
	// keyboard, rows 5-7
	for k := 0; k < 24; k++ {
		col, row := k%8, 5+k/8
		v := ledDim
		if n := c.baseNote() + k; n%12 == 0 {
			v = trackColors[c.track]
		} else if isWhite(n % 12) {
			v = ledWhite
		}
		set(col, row, v)
	}
	return b, pads
}

func isWhite(pc int) bool {
	switch pc {
	case 1, 3, 6, 8, 10:
		return false
	}
	return true
}
