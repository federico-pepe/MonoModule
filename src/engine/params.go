package engine

// Sizes of the model. They follow the Monomachine: six tracks (six voices),
// up to 64 steps, 128 patterns in 8 banks, 128 kits, songs of up to 200 rows.
const (
	NumTracks   = 6
	MaxSteps    = 64
	NumPatterns = 128 // 8 banks (A-H) of 16
	NumKits     = 128
	NumSongs    = 24
	MaxSongRows = 200
	NumUserWave = 32 // DigiPro user waveforms
	MaxPoly     = NumTracks
	MaxChord    = 6 // notes in one step (poly mode)

	PerPage = 8
	NPages  = 7 // SYN AMP FLT FX LFO1 LFO2 LFO3
	NParams = NPages * PerPage
	NMaster = 8
)

// Pages of eight parameters, as on the hardware's DATA ENTRY knobs.
const (
	PageSyn = iota
	PageAmp
	PageFlt
	PageFx
	PageLFO1
	PageLFO2
	PageLFO3
)

// PageNames are the page labels.
var PageNames = [NPages]string{"SYN", "AMP", "FLT", "FX", "LFO1", "LFO2", "LFO3"}

// Parameter indices. The SYN page (0..7) is named by the machine.
const (
	ATK  = PerPage*PageAmp + iota // amp attack
	HOLD                          // amp hold (127 = until note off)
	DEC                           // amp decay after the hold
	REL                           // amp release after note off
	DIST                          // overload distortion
	VOL                           // volume
	PAN                           // pan
	PORT                          // portamento
)

const (
	BASE = PerPage*PageFlt + iota // filter base (high-pass) position
	WDTH                          // filter width (low-pass sits this far above)
	HPQ                           // high-pass resonance
	LPQ                           // low-pass resonance
	FATK                          // filter envelope attack
	FDEC                          // filter envelope decay
	BOFS                          // envelope offset added to BASE (64 = none)
	WOFS                          // envelope offset added to WDTH (64 = none)
)

const (
	EQF  = PerPage*PageFx + iota // EQ centre frequency
	EQG                          // EQ gain, +-36 dB
	SRR                          // sample rate reduction
	DSND                         // delay send (negative: stereo, positive: ping-pong)
	DTIM                         // delay time in 1/256 notes
	DFB                          // delay feedback
	DBAS                         // delay high-pass in the loop
	DWID                         // delay low-pass in the loop, relative to DBAS
)

// LFO parameter offsets inside an LFO page.
const (
	LPAGE = iota // destination page (SYN..LFO3, PTCH)
	LDEST        // destination parameter within the page
	LTRIG        // FREE TRIG HOLD ONE HALF
	LWAVE        // 11 shapes
	LMULT        // speed multiplier, 1..128 in octaves
	LSPD         // base speed, tempo synced
	LINTL        // interlace
	LDPTH        // depth (64 = none, bipolar)
)

// LFOBase is the first parameter index of LFO n (0..2).
func LFOBase(n int) int { return PerPage * (PageLFO1 + n) }

// Names of the pages that do not depend on the machine.
var commonNames = map[int][PerPage]string{
	PageAmp:  {"ATK", "HOLD", "DEC", "REL", "DIST", "VOL", "PAN", "PORT"},
	PageFlt:  {"BASE", "WDTH", "HPQ", "LPQ", "ATK", "DEC", "BOFS", "WOFS"},
	PageFx:   {"EQF", "EQG", "SRR", "DSND", "DTIM", "DFB", "DBAS", "DWID"},
	PageLFO1: {"PAGE", "DEST", "TRIG", "WAVE", "MULT", "SPD", "INTL", "DPTH"},
	PageLFO2: {"PAGE", "DEST", "TRIG", "WAVE", "MULT", "SPD", "INTL", "DPTH"},
	PageLFO3: {"PAGE", "DEST", "TRIG", "WAVE", "MULT", "SPD", "INTL", "DPTH"},
}

// DefaultCommon is the neutral value of every parameter on pages AMP..LFO3:
// the amp holds until note off, the filter is open, the effects are off,
// centred, and the LFOs have no depth.
func defaultCommon() (d [NParams]uint8) {
	set := func(page int, v [PerPage]uint8) { copy(d[page*PerPage:], v[:]) }
	set(PageAmp, [PerPage]uint8{0, 127, 64, 40, 0, 100, 64, 0})
	set(PageFlt, [PerPage]uint8{0, 127, 0, 0, 0, 64, 64, 64})
	set(PageFx, [PerPage]uint8{64, 64, 0, 64, 32, 50, 0, 127})
	for n := 0; n < 3; n++ {
		set(PageLFO1+n, [PerPage]uint8{0, 0, 0, 0, 0, 64, 0, 64})
	}
	return
}

// DefaultCommon holds the neutral values of parameters 8..55.
var DefaultCommon = defaultCommon()

// Master bus parameters (stored in each kit). The Monomachine has no master
// effects; this strip (volume, compressor, tilt EQ) is an addition.
const (
	MVOL = iota
	MCTHR
	MCRAT
	MCREL
	MCMKP
	MCMIX
	MELG
	MEHG
)

// MasterNames are the display names of the master parameters.
var MasterNames = [NMaster]string{"MVOL", "CTHR", "CRAT", "CREL", "CMKP", "CMIX", "ELG", "EHG"}

// DefaultMaster: unity-ish volume, compressor off, flat EQ.
var DefaultMaster = [NMaster]uint8{100, 90, 40, 50, 40, 0, 64, 64}

// ParamName is the display name of parameter idx (0..55) on a machine.
func ParamName(machine, idx int) string {
	page := idx / PerPage
	if idx < 0 || idx >= NParams {
		return "?"
	}
	if page == PageSyn {
		if machine >= 0 && machine < len(Machines) {
			return Machines[machine].Params[idx]
		}
		return "P" + string(rune('1'+idx))
	}
	return commonNames[page][idx%PerPage]
}

// IsBipolar says whether a parameter has its zero in the middle (64).
func IsBipolar(idx int) bool {
	switch idx {
	case PAN, BOFS, WOFS, EQG, DSND:
		return true
	}
	if idx >= LFOBase(0) {
		return idx%PerPage == LDPTH
	}
	return false
}

// Trig bits: which envelopes a note trig restarts. A normal note has all
// three; a "trigless" trig (0) changes pitch and locks without restarting.
const (
	TrigAmp = 1 << iota
	TrigFlt
	TrigLFO
	TrigAll = TrigAmp | TrigFlt | TrigLFO
)
