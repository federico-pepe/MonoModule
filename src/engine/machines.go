package engine

// Machine is one sound engine: a name, the names and defaults of its eight
// SYN-page parameters, and the generator behind it.
type Machine struct {
	ID       int
	Name     string // e.g. "SWAVE-SAW"
	Group    string // GND SWAVE SID VO FM+ DPRO FX
	Params   [PerPage]string
	Defaults [PerPage]uint8
	FX       bool // an FX machine: processes other tracks instead of making sound
}

// Machines is every machine, in menu order. A machine's ID is its index.
var Machines []Machine

// Generator kinds equal the machine index; these name the ones the voice
// switches on.
const (
	mGndEmpty = iota
	mGndSin
	mGndNois
	mSwSaw
	mSwPulse
	mSwEns
	mSid
	mVo
	mFmStat
	mFmPar
	mFmDyn
	mDpWave
	mDpBbox
	mDpDdrw
	mDpDens
	mFxThru
	mFxReverb
	mFxChorus
	mFxDynamix
	mFxRing
	mFxPhaser
	mFxFlanger
	numMachines
)

func init() { Machines = buildMachines() }

// MachineIndex finds a machine by name, or returns -1.
func MachineIndex(name string) int {
	for i := range Machines {
		if Machines[i].Name == name {
			return i
		}
	}
	return -1
}

func buildMachines() []Machine {
	var ms []Machine
	add := func(name, group string, fx bool, params [PerPage]string, defs [PerPage]uint8) {
		ms = append(ms, Machine{ID: len(ms), Name: name, Group: group, FX: fx, Params: params, Defaults: defs})
	}
	none := [PerPage]string{"----", "----", "----", "----", "----", "----", "----", "----"}
	add("GND-EMPTY", "GND", false, none, [PerPage]uint8{})
	add("GND-SIN", "GND", false, [PerPage]string{"TUNE", "HARM", "FOLD", "SUB", "FDBK", "DRFT", "PHAS", "BITS"},
		[PerPage]uint8{64, 0, 0, 0, 0, 0, 0, 0})
	add("GND-NOIS", "GND", false, [PerPage]string{"HPF", "LPF", "RESO", "COLR", "CRCK", "SHLD", "LEVL", "TILT"},
		[PerPage]uint8{0, 127, 0, 0, 0, 0, 100, 0})
	add("SWAVE-SAW", "SWAVE", false, [PerPage]string{"UNIW", "UNIL", "EXTL", "SQ-1", "SI-1", "SI-2", "TUNE", "DRFT"},
		[PerPage]uint8{40, 90, 50, 40, 0, 0, 64, 0})
	add("SWAVE-PULSE", "SWAVE", false, [PerPage]string{"PW", "PWMD", "PWMS", "DETU", "MIX", "SUB", "TUNE", "DRFT"},
		[PerPage]uint8{50, 60, 30, 30, 60, 40, 64, 0})
	add("SWAVE-ENS", "SWAVE", false, [PerPage]string{"DETU", "SPRD", "CHRD", "INV", "CHRL", "CHRW", "TUNE", "DRFT"},
		[PerPage]uint8{40, 20, 0, 0, 70, 50, 64, 0})
	add("SID-6581", "SID", false, [PerPage]string{"WAVE", "PW", "PWMD", "MODE", "MOFS", "MDET", "TUNE", "DRFT"},
		[PerPage]uint8{38, 64, 0, 0, 64, 64, 64, 0})
	add("VO-6", "VO", false, [PerPage]string{"VOW1", "VOW2", "MORF", "CONS", "CLEN", "CVOL", "SIBI", "BRTH"},
		[PerPage]uint8{0, 40, 0, 0, 64, 64, 30, 10})
	add("FM+STAT", "FM+", false, [PerPage]string{"M1FR", "M1LV", "M2FR", "M2LV", "FDBK", "TONE", "TUNE", "DRFT"},
		[PerPage]uint8{40, 50, 60, 30, 20, 127, 64, 0})
	add("FM+PAR", "FM+", false, [PerPage]string{"M1FR", "M2FR", "M3FR", "M1LV", "M2LV", "M3LV", "MENV", "TUNE"},
		[PerPage]uint8{40, 80, 110, 50, 30, 20, 50, 64})
	add("FM+DYN", "FM+", false, [PerPage]string{"M1FR", "M1LV", "M2FR", "M2LV", "FDBK", "MENV", "MDEC", "XPON"},
		[PerPage]uint8{40, 60, 64, 40, 20, 20, 60, 0})
	add("DPRO-WAVE", "DPRO", false, [PerPage]string{"WAVE", "MORF", "SYNC", "PHAS", "PRAT", "TUNE", "BITS", "DRFT"},
		[PerPage]uint8{16, 0, 0, 0, 40, 64, 0, 0})
	add("DPRO-BBOX", "DPRO", false, [PerPage]string{"SAMP", "PTCH", "STRT", "RTRG", "RTIM", "BITS", "REV", "DECY"},
		[PerPage]uint8{0, 64, 0, 0, 40, 0, 0, 127})
	add("DPRO-DDRW", "DPRO", false, [PerPage]string{"WVA", "WVB", "MIX", "TIME", "PHAS", "SYNC", "DETU", "BITS"},
		[PerPage]uint8{0, 20, 64, 0, 0, 0, 64, 0})
	add("DPRO-DENS", "DPRO", false, [PerPage]string{"WAVE", "PCH2", "PCH3", "PCH4", "CHRL", "CHRW", "TUNE", "DRFT"},
		[PerPage]uint8{20, 76, 96, 64, 60, 50, 64, 0})
	add("FX-THRU", "FX", true, [PerPage]string{"GAIN", "BAL", "SAT", "WID", "----", "----", "----", "----"},
		[PerPage]uint8{64, 64, 0, 64, 0, 0, 0, 0})
	add("FX-REVERB", "FX", true, [PerPage]string{"DEC", "DAMP", "GATE", "HPF", "LPF", "PRED", "WET", "SIZE"},
		[PerPage]uint8{80, 60, 0, 0, 127, 20, 80, 64})
	add("FX-CHORUS", "FX", true, [PerPage]string{"DPTH", "SPD", "DLAY", "WID", "FDBK", "MIX", "HPF", "LPF"},
		[PerPage]uint8{60, 40, 50, 100, 0, 64, 0, 127})
	add("FX-DYNAMIX", "FX", true, [PerPage]string{"THR", "RAT", "ATK", "REL", "MKUP", "MIX", "LIM", "SCHP"},
		[PerPage]uint8{70, 60, 30, 50, 30, 127, 0, 0})
	add("FX-RINGMOD", "FX", true, [PerPage]string{"FREQ", "FINE", "SHPE", "MIX", "FMDP", "FMSP", "HPF", "LPF"},
		[PerPage]uint8{60, 64, 0, 100, 0, 40, 0, 127})
	add("FX-PHASER", "FX", true, [PerPage]string{"DPTH", "SPD", "FDBK", "STGS", "CNTR", "SPRD", "MIX", "WID"},
		[PerPage]uint8{80, 30, 60, 50, 64, 30, 64, 80})
	add("FX-FLANGER", "FX", true, [PerPage]string{"DPTH", "SPD", "DLAY", "FDBK", "MIX", "WID", "POL", "HPF"},
		[PerPage]uint8{70, 30, 40, 70, 64, 80, 0, 0})
	if len(ms) != numMachines {
		panic("machines table out of step with the constants")
	}
	return ms
}
