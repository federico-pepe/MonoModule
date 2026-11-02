package engine

import "math"

// waveLen is the length of one single-cycle wave table (DPRO-WAVE).
const waveLen = 2048

// NumFactoryWaves is the number of built-in DPRO-WAVE waveforms.
const NumFactoryWaves = 32

var (
	factoryWaves [NumFactoryWaves][waveLen]float32
	bboxSamples  [24][]float32 // 12-bit percussion one-shots at bboxRate
)

const bboxRate = 44100

func init() {
	buildFactoryWaves()
	buildBeatBox()
}

// additive builds a table from harmonic amplitudes (index 0 = fundamental).
func additive(t *[waveLen]float32, amps []float64, phases func(k int) float64) {
	var pk float64
	tmp := make([]float64, waveLen)
	for i := range tmp {
		x := 2 * math.Pi * float64(i) / waveLen
		var s float64
		for k, a := range amps {
			if a == 0 {
				continue
			}
			ph := 0.0
			if phases != nil {
				ph = phases(k)
			}
			s += a * math.Sin(float64(k+1)*x+ph)
		}
		tmp[i] = s
		if math.Abs(s) > pk {
			pk = math.Abs(s)
		}
	}
	if pk == 0 {
		pk = 1
	}
	for i := range tmp {
		t[i] = float32(tmp[i] / pk)
	}
}

func quant12(v float32) float32 { return float32(math.Round(float64(v)*2047)) / 2047 }

func buildFactoryWaves() {
	harm := func(n int, f func(k int) float64) []float64 {
		a := make([]float64, n)
		for k := range a {
			a[k] = f(k + 1)
		}
		return a
	}
	fill := func(i int, f func(x float64) float64) {
		var pk float64
		for j := 0; j < waveLen; j++ {
			v := f(float64(j) / waveLen)
			factoryWaves[i][j] = float32(v)
			if math.Abs(v) > pk {
				pk = math.Abs(v)
			}
		}
		if pk > 0 {
			for j := range factoryWaves[i] {
				factoryWaves[i][j] /= float32(pk)
			}
		}
	}
	two := 2 * math.Pi
	fill(0, func(x float64) float64 { return math.Sin(two * x) })
	fill(1, func(x float64) float64 { return 1 - 4*math.Abs(x-0.5) })
	fill(2, func(x float64) float64 { return 2*x - 1 })
	fill(3, func(x float64) float64 {
		if x < 0.5 {
			return 1
		}
		return -1
	})
	fill(4, func(x float64) float64 {
		if x < 0.25 {
			return 1
		}
		return -1
	})
	fill(5, func(x float64) float64 {
		if x < 0.125 {
			return 1
		}
		return -1
	})
	fill(6, func(x float64) float64 { return math.Abs(math.Sin(two*x))*2 - 1 })
	fill(7, func(x float64) float64 { return math.Sin(two*x) + 0.7*math.Sin(2*two*x) })
	fill(8, func(x float64) float64 { return math.Sin(two*x + 2*math.Sin(two*x)) })
	fill(9, func(x float64) float64 { return math.Sin(two*x + 4*math.Sin(2*two*x)) })
	fill(10, func(x float64) float64 { return math.Floor(x*6)/2.5 - 1 })
	fill(11, func(x float64) float64 { return (2*x - 1) * math.Abs(math.Sin(two*x*0.5+0.3)) })
	fill(12, func(x float64) float64 { return math.Tanh(3 * math.Sin(two*x)) })
	fill(13, func(x float64) float64 { return math.Sin(math.Sin(two*x) * 3) })
	fill(14, func(x float64) float64 {
		y := 2*x - 1
		return y * y * y
	})
	fill(15, func(x float64) float64 { return math.Sin(two*x) * math.Sin(3*two*x+0.4) })
	// additive spectra
	additive(&factoryWaves[16], harm(16, func(k int) float64 {
		if k%2 == 1 {
			return 1 / float64(k)
		}
		return 0
	}), nil)
	additive(&factoryWaves[17], harm(24, func(k int) float64 { return 1 / math.Sqrt(float64(k)) }), nil)
	additive(&factoryWaves[18], []float64{1, 0.8, 0.6, 0.5, 0.2, 0.1}, nil)
	additive(&factoryWaves[19], []float64{1, 0, 0.5, 0, 0.25, 0, 0.12}, nil)
	additive(&factoryWaves[20], []float64{0.4, 1, 0.3, 0.9, 0.1, 0.4, 0, 0.2}, nil)
	additive(&factoryWaves[21], []float64{1, 0.1, 0.9, 0.05, 0.8, 0.05, 0.7}, nil)
	additive(&factoryWaves[22], harm(20, func(k int) float64 {
		d := float64(k) - 5
		return math.Exp(-d*d/6) + 0.2/float64(k)
	}), nil) // formant around the 5th harmonic
	additive(&factoryWaves[23], harm(20, func(k int) float64 {
		d := float64(k) - 9
		return math.Exp(-d*d/10) + 0.5/float64(k)
	}), nil)
	additive(&factoryWaves[24], []float64{1, 0, 0, 0.7, 0, 0, 0.5, 0, 0, 0.3}, nil)
	additive(&factoryWaves[25], []float64{1, 1, 1, 1, 1, 1, 1, 1}, func(k int) float64 { return float64(k*k) * 0.37 })
	additive(&factoryWaves[26], harm(12, func(k int) float64 { return 1 / float64(k*k) }), nil)
	additive(&factoryWaves[27], []float64{0.2, 0.2, 1, 0.2, 0.2, 0.8, 0.1, 0.1, 0.5}, nil)
	additive(&factoryWaves[28], harm(30, func(k int) float64 { return 1 / float64(k) }), func(k int) float64 { return float64(k) * 0.9 })
	additive(&factoryWaves[29], []float64{1, 0.6, 0.9, 0.3, 0.7, 0.2, 0.5, 0.1}, func(k int) float64 { return float64(k) * 1.3 })
	// a fixed pseudo-random table, smoothed
	var s uint32 = 12345
	var prev float32
	for j := 0; j < waveLen; j++ {
		s ^= s << 13
		s ^= s >> 17
		s ^= s << 5
		v := float32(int32(s)) / 2147483648
		prev += (v - prev) * 0.35
		factoryWaves[30][j] = prev
	}
	normalise(&factoryWaves[30])
	fill(31, func(x float64) float64 { return math.Sin(two*x) * (1 - x) * 2 })
	for i := range factoryWaves {
		for j := range factoryWaves[i] {
			factoryWaves[i][j] = quant12(factoryWaves[i][j]) // raw 12-bit
		}
	}
}

func normalise(t *[waveLen]float32) {
	var pk, mean float32
	for _, v := range t {
		mean += v
	}
	mean /= waveLen
	for i := range t {
		t[i] -= mean
		if a := absf(t[i]); a > pk {
			pk = a
		}
	}
	if pk > 0 {
		for i := range t {
			t[i] /= pk
		}
	}
}

// BeatBoxNames are the 24 built-in percussion hits of DPRO-BBOX.
var BeatBoxNames = [24]string{
	"KICK1", "KICK2", "KICK3", "KICK4", "SNR1", "SNR2", "SNR3", "CLAP",
	"RIM", "HAT1", "HAT2", "HAT3", "OPEN1", "OPEN2", "TOM1", "TOM2",
	"TOM3", "COWB", "CLAV", "SHAK", "CRSH", "CONG", "BLIP", "ZAP",
}

func buildBeatBox() {
	var seed uint32 = 0xC0FFEE
	noise := func() float64 {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return float64(int32(seed)) / 2147483648
	}
	gen := func(i int, secs float64, f func(t float64) float64) {
		n := int(secs * bboxRate)
		b := make([]float32, n)
		var pk float64
		out := make([]float64, n)
		for j := range out {
			out[j] = f(float64(j) / bboxRate)
			if a := math.Abs(out[j]); a > pk {
				pk = a
			}
		}
		for j := range b {
			b[j] = quant12(float32(out[j] / pk * 0.95))
		}
		bboxSamples[i] = b
	}
	kick := func(i int, f0, f1, decay, click float64) {
		var ph float64
		gen(i, 0.5, func(t float64) float64 {
			f := f1 + (f0-f1)*math.Exp(-t*30)
			ph += 2 * math.Pi * f / bboxRate
			return math.Sin(ph)*math.Exp(-t*decay) + click*noise()*math.Exp(-t*400)
		})
	}
	kick(0, 150, 45, 9, 0.2)
	kick(1, 220, 55, 12, 0.35)
	kick(2, 110, 38, 6, 0.1)
	kick(3, 300, 60, 16, 0.5)
	snare := func(i int, f, tone, dec float64) {
		var ph float64
		gen(i, 0.35, func(t float64) float64 {
			ph += 2 * math.Pi * f / bboxRate
			return tone*math.Sin(ph)*math.Exp(-t*25) + noise()*math.Exp(-t*dec)
		})
	}
	snare(4, 190, 0.6, 18)
	snare(5, 230, 0.4, 28)
	snare(6, 160, 0.8, 12)
	gen(7, 0.4, func(t float64) float64 {
		e := math.Exp(-t * 25)
		for _, d := range []float64{0.011, 0.022, 0.033} {
			if t > d {
				e += math.Exp(-(t - d) * 25)
			}
		}
		return noise() * e * math.Exp(-t*6)
	})
	var ph float64
	gen(8, 0.1, func(t float64) float64 {
		ph += 2 * math.Pi * 1700 / bboxRate
		return (math.Sin(ph) + 0.5*noise()) * math.Exp(-t*90)
	})
	hat := func(i int, dec, hp float64, secs float64) {
		var lp, prev float64
		gen(i, secs, func(t float64) float64 {
			n := noise()
			lp += (n - lp) * hp
			x := n - lp
			y := x - prev*0.5
			prev = x
			return y * math.Exp(-t*dec)
		})
	}
	hat(9, 70, 0.1, 0.12)
	hat(10, 45, 0.2, 0.15)
	hat(11, 95, 0.05, 0.1)
	hat(12, 9, 0.1, 0.6)
	hat(13, 6, 0.25, 0.8)
	tom := func(i int, f float64) {
		var p float64
		gen(i, 0.5, func(t float64) float64 {
			p += 2 * math.Pi * (f + f*0.6*math.Exp(-t*20)) / bboxRate
			return math.Sin(p) * math.Exp(-t*8)
		})
	}
	tom(14, 180)
	tom(15, 130)
	tom(16, 90)
	var p1, p2 float64
	gen(17, 0.5, func(t float64) float64 {
		p1 += 2 * math.Pi * 560 / bboxRate
		p2 += 2 * math.Pi * 845 / bboxRate
		return (sgn(math.Sin(p1)) + sgn(math.Sin(p2))) * math.Exp(-t*9)
	})
	var p3 float64
	gen(18, 0.15, func(t float64) float64 {
		p3 += 2 * math.Pi * 2500 / bboxRate
		return math.Sin(p3) * math.Exp(-t*45)
	})
	gen(19, 0.2, func(t float64) float64 {
		return noise() * math.Sin(math.Pi*math.Min(t/0.2, 1)) * math.Exp(-t*10)
	})
	var lp2 float64
	gen(20, 1.0, func(t float64) float64 {
		lp2 += (noise() - lp2) * 0.45
		return (noise() - lp2) * math.Exp(-t*4)
	})
	var p4 float64
	gen(21, 0.4, func(t float64) float64 {
		p4 += 2 * math.Pi * (330 + 80*math.Exp(-t*30)) / bboxRate
		return math.Sin(p4) * math.Exp(-t*12)
	})
	var p5 float64
	gen(22, 0.12, func(t float64) float64 {
		p5 += 2 * math.Pi * 900 / bboxRate
		return math.Sin(p5) * math.Exp(-t*40)
	})
	var p6 float64
	gen(23, 0.3, func(t float64) float64 {
		p6 += 2 * math.Pi * (2400 * math.Exp(-t*12)) / bboxRate
		return math.Sin(p6) * math.Exp(-t*6)
	})
}
