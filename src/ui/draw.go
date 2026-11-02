package ui

import (
	"image"
	"image/color"
	"strings"
)

// Screen size of Push 3 (core/push3 VisW x VisH).
const (
	W = 960
	H = 160
)

// The look: the green LCD of the Monomachine, dark pixels on green.
var (
	colBG    = color.NRGBA{R: 150, G: 186, B: 60, A: 255}
	colFG    = color.NRGBA{R: 14, G: 28, B: 6, A: 255}
	colDim   = color.NRGBA{R: 104, G: 142, B: 40, A: 255}
	colLight = color.NRGBA{R: 196, G: 226, B: 104, A: 255}
	colWarn  = color.NRGBA{R: 255, G: 240, B: 140, A: 255}
)

type canvas struct{ *image.NRGBA }

func newCanvas() canvas {
	c := canvas{image.NewNRGBA(image.Rect(0, 0, W, H))}
	c.fill(0, 0, W, H, colBG)
	return c
}

func (c canvas) fill(x, y, w, h int, col color.NRGBA) {
	r := image.Rect(x, y, x+w, y+h).Intersect(c.Bounds())
	for yy := r.Min.Y; yy < r.Max.Y; yy++ {
		o := c.PixOffset(r.Min.X, yy)
		for xx := r.Min.X; xx < r.Max.X; xx++ {
			c.Pix[o], c.Pix[o+1], c.Pix[o+2], c.Pix[o+3] = col.R, col.G, col.B, col.A
			o += 4
		}
	}
}

// frame draws a 1 pixel outline.
func (c canvas) frame(x, y, w, h int, col color.NRGBA) {
	c.fill(x, y, w, 1, col)
	c.fill(x, y+h-1, w, 1, col)
	c.fill(x, y, 1, h, col)
	c.fill(x+w-1, y, 1, h, col)
}

// textW is the width of s at scale sc (6 columns per glyph at scale 1).
func textW(s string, sc int) int { return len(s) * 6 * sc }

// text draws s with its top-left at (x, y). Anything outside ASCII 32..126
// becomes '?', because the font has nothing else.
func (c canvas) text(x, y, sc int, s string, col color.NRGBA) {
	for _, r := range s {
		if r < 32 || r > 126 {
			r = '?'
		}
		g := &font5x7[r-32]
		for cx := 0; cx < 5; cx++ {
			bits := g[cx]
			for cy := 0; cy < 7; cy++ {
				if bits&(1<<uint(cy)) != 0 {
					c.fill(x+cx*sc, y+cy*sc, sc, sc, col)
				}
			}
		}
		x += 6 * sc
	}
}

// textC draws s centred on cx.
func (c canvas) textC(cx, y, sc int, s string, col color.NRGBA) {
	c.text(cx-textW(s, sc)/2+sc/2, y, sc, s, col)
}

// label draws s inverted (light text on a dark box), for the selected thing.
func (c canvas) label(x, y, sc int, s string) {
	c.fill(x-2, y-2, textW(s, sc)+3, 7*sc+4, colFG)
	c.text(x, y, sc, s, colBG)
}

// bar draws a horizontal value bar of frac 0..1.
func (c canvas) bar(x, y, w, h int, frac float64, col color.NRGBA) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	c.frame(x, y, w, h, colFG)
	c.fill(x+2, y+2, int(float64(w-4)*frac), h-4, col)
}

// centreBar draws a bar that grows from the middle (for pan and EQ gain).
func (c canvas) centreBar(x, y, w, h int, frac float64, col color.NRGBA) {
	c.frame(x, y, w, h, colFG)
	mid := x + w/2
	v := x + 2 + int(float64(w-4)*frac)
	if v >= mid {
		c.fill(mid, y+2, v-mid, h-4, col)
	} else {
		c.fill(v, y+2, mid-v, h-4, col)
	}
	c.fill(mid, y+1, 1, h-2, colFG)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func num3(v int) string {
	if v < 0 {
		v = 0
	}
	return string([]byte{byte('0' + v/100%10), byte('0' + v/10%10), byte('0' + v%10)})
}
