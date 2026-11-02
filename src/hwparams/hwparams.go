// Package hwparams reads the ALSA hw_params that Live negotiated on the
// virtual loopback card, so MonoModule can open its side with the same
// channels, rate and period. Plain Go (no cgo) so it tests anywhere.
package hwparams

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Params is one snapshot of an open PCM stream.
type Params struct {
	Channels, Rate, Period, Buffer int
}

// Parse reads a hw_params file. ok is false (and err nil) when the stream is
// closed, which is the normal state before Live opens its side.
func Parse(r io.Reader) (p Params, ok bool, err error) {
	sc := bufio.NewScanner(r)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err = sc.Err(); err != nil {
		return Params{}, false, err
	}
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "closed" {
		return Params{}, false, nil
	}
	for _, ln := range lines {
		k, v, found := strings.Cut(ln, ":")
		if !found {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.IndexByte(v, ' '); i >= 0 {
			v = v[:i]
		}
		n, e := strconv.Atoi(v)
		if e != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "channels":
			p.Channels = n
		case "rate":
			p.Rate = n
		case "period_size":
			p.Period = n
		case "buffer_size":
			p.Buffer = n
		}
	}
	if p.Channels == 0 || p.Rate == 0 || p.Period == 0 {
		return Params{}, false, nil
	}
	return p, true, nil
}

// Path is the proc file for the capture side of device 0 of a card, the side
// Live opens.
func Path(card string) string {
	return filepath.Join("/proc/asound", card, "pcm0c", "sub0", "hw_params")
}

// Read reads the live params of a card. A missing card or a closed stream is
// ok=false with no error.
func Read(card string) (Params, bool, error) {
	f, err := os.Open(Path(card))
	if os.IsNotExist(err) {
		return Params{}, false, nil
	}
	if err != nil {
		return Params{}, false, err
	}
	defer f.Close()
	return Parse(f)
}
