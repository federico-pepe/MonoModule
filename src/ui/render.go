package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"monomodule/engine"
)

const (
	cellW   = W / 8
	tabsY   = 0
	headY   = 18
	cellsY  = 38
	ribbonY = 134
	softY   = 144
)

var (
	lfoTrigNames = []string{"FREE", "TRIG", "HOLD", "ONE", "HALF"}
	lfoWaveNames = []string{"TRI", "SIN", "SQR", "SAW", "EXP", "RMP", "TRI-", "SIN-", "SQR-", "SAW-", "RND"}
	lfoPageNames = []string{"SYN", "AMP", "FLT", "FX", "LFO1", "LFO2", "LFO3", "PTCH"}
)

// Render draws the whole screen.
func (c *Controller) Render() *image.NRGBA {
	c.mu.Lock()
	defer c.mu.Unlock()
	cv := newCanvas()
	c.drawTabs(cv)
	c.drawHeader(cv)
	switch {
	case c.ov == ovMachine:
		c.drawMachineList(cv)
	default:
		switch c.page {
		case PgSyn, PgAmp, PgFlt, PgFx, PgLfo:
			c.drawParamCells(cv)
		case PgSeq:
			if c.sub == 0 {
				c.drawSeq(cv)
			} else {
				c.drawArp(cv)
			}
		case PgKit:
			if c.sub == 0 {
				c.drawKit(cv)
			} else {
				c.drawMaster(cv)
			}
		case PgSong:
			c.drawSong(cv)
		case PgSys:
			c.drawSystem(cv)
		}
	}
	c.drawRibbon(cv)
	c.drawSoftKeys(cv)
	return cv.NRGBA
}

func (c *Controller) tabName(i int) string {
	n := pageNames[i]
	if i == PgLfo {
		n = fmt.Sprintf("LFO%d", c.sub+1)
		if c.page != PgLfo {
			n = "LFO"
		}
	}
	return n
}

func (c *Controller) drawTabs(cv canvas) {
	for i := 0; i < numPages-1; i++ {
		x := i * cellW
		if i == c.page {
			cv.fill(x+2, tabsY, cellW-4, 16, colFG)
			cv.textC(x+cellW/2, tabsY+1, 2, c.tabName(i), colBG)
		} else {
			cv.textC(x+cellW/2, tabsY+1, 2, c.tabName(i), colFG)
		}
	}
}

func (c *Controller) drawHeader(cv canvas) {
	pos := c.eng.Position()
	m := c.eng.Machine(c.track)
	pat := c.pattern()
	sync := "INT"
	if c.eng.Sync() == engine.SyncMIDI {
		sync = "MIDI"
	}
	cv.fill(0, headY-2, W, 1, colDim)
	cv.label(6, headY, 2, fmt.Sprintf("T%d", c.track+1))
	cv.text(44, headY, 2, engine.Machines[m].Name, colFG)
	cv.text(176, headY, 2, fmt.Sprintf("K%03d", c.eng.KitNumber()+1), colFG)
	cv.text(262, headY, 2, fmt.Sprintf("%s %02d/%02d", engine.PatternName(pos.Pattern), pos.Step+1, pat.Len), colFG)
	if pos.Next >= 0 {
		cv.text(262+12*9, headY, 2, ">"+engine.PatternName(pos.Next), colWarn)
	}
	if pat.Poly {
		cv.text(452, headY, 2, fmt.Sprintf("POLY T%d", pat.PolyTrack+1), colFG)
	}
	cv.text(560, headY, 2, fmt.Sprintf("%5.1f", c.eng.Tempo()), colFG)
	cv.text(630, headY, 2, sync, colFG)
	flags := ""
	if pos.Playing {
		flags += "PLAY "
	}
	if c.eng.Recording() {
		flags += "REC "
	}
	if on, _ := c.eng.SongMode(); on {
		flags += "SONG "
	}
	cv.text(700, headY, 2, flags, colFG)
	if ok, _ := c.host.AudioStatus(); !ok {
		cv.text(700, headY, 2, "NO AUDIO", colWarn)
	}
	if c.now().Before(c.toastUntil) {
		w := textW(c.toast, 2)
		cv.fill(W-w-14, headY-3, w+12, 20, colFG)
		cv.text(W-w-8, headY, 2, c.toast, colWarn)
	}
	if c.layer != layerNone || c.muteMode {
		name := map[layerKind]string{layerSwing: "SWING", layerSlide: "SLIDE"}[c.layer]
		if c.muteMode {
			name = "MUTE"
		}
		cv.text(860, headY, 2, name, colWarn)
	}
}

func (c *Controller) cell(cv canvas, i int, label, big, small string, frac float64, centre bool, locked bool) {
	x := i * cellW
	if i > 0 {
		cv.fill(x, cellsY+2, 1, 90, colDim)
	}
	touched := c.lastTouch == i && c.now().Sub(c.lastTouchAt) < 600*time.Millisecond
	if touched {
		cv.label(x+cellW/2-textW(label, 2)/2, cellsY+4, 2, label)
	} else {
		cv.textC(x+cellW/2, cellsY+4, 2, label, colFG)
	}
	cv.textC(x+cellW/2, cellsY+24, 3, big, colFG)
	if frac >= 0 {
		if centre {
			cv.centreBar(x+10, cellsY+52, cellW-20, 12, frac, colFG)
		} else {
			cv.bar(x+10, cellsY+52, cellW-20, 12, frac, colFG)
		}
	}
	if small != "" {
		cv.textC(x+cellW/2, cellsY+72, 2, small, colFG)
	}
	if locked {
		cv.label(x+cellW-40, cellsY+84, 1, "LOCK")
	}
}

func (c *Controller) drawParamCells(cv canvas) {
	m := c.eng.Machine(c.track)
	var lock engine.Lock
	haveLock := false
	for s := range c.held {
		if l, ok := c.eng.StepLock(c.track, s); ok {
			lock, haveLock = l, true
		}
	}
	for i := 0; i < 8; i++ {
		p := c.paramIndex(i)
		v := c.eng.Param(c.track, p)
		locked := false
		if haveLock && lock.Has(p) {
			v, locked = int(lock.Val[p]), true
		}
		c.cell(cv, i, engine.ParamName(m, p), num3(v), c.paramNote(m, p, v), float64(v)/127, engine.IsBipolar(p), locked)
	}
}

// paramNote is the small line under a parameter: what the number means.
func (c *Controller) paramNote(m, p, v int) string {
	switch p {
	case engine.PAN:
		return panText(v)
	case engine.EQG:
		return fmt.Sprintf("%+.1fDB", float64(v-64)*36/64)
	case engine.DSND:
		if v < 62 {
			return "STEREO"
		} else if v > 66 {
			return "PINGPONG"
		}
		return "OFF"
	case engine.DTIM:
		return fmt.Sprintf("%d/256", v+1)
	case engine.BASE, engine.WDTH:
		return ""
	case engine.HOLD:
		if v >= 127 {
			return "INFINITE"
		}
	case 0:
		if engine.Machines[m].Name == "DPRO-WAVE" {
			slot := clampi(v*engine.NumUserWave/128, 0, engine.NumUserWave-1)
			if w := c.eng.Wave(slot); w != nil {
				return fmt.Sprintf("%02d %s", slot+1, strings.ToUpper(w.Name))
			}
			return fmt.Sprintf("%02d EMPTY", slot+1)
		}
	}
	if p >= engine.LFOBase(0) {
		switch p % engine.PerPage {
		case engine.LPAGE:
			return lfoPageNames[clampi(v*8/128, 0, 7)]
		case engine.LTRIG:
			return lfoTrigNames[clampi(v*5/128, 0, 4)]
		case engine.LWAVE:
			return lfoWaveNames[clampi(v*11/128, 0, 10)]
		}
	}
	return ""
}

func panText(v int) string {
	switch {
	case v >= 62 && v <= 66:
		return "CENTER"
	case v < 64:
		return fmt.Sprintf("L%d", (64-v)*100/64)
	}
	return fmt.Sprintf("R%d", (v-64)*100/63)
}

func (c *Controller) drawMaster(cv canvas) {
	for i := 0; i < 8; i++ {
		v := c.eng.Master(i)
		small := ""
		switch i {
		case engine.MELG, engine.MEHG:
			small = fmt.Sprintf("%+.1fDB", (float64(v)/127-0.5)*24)
		case engine.MCTHR:
			small = fmt.Sprintf("%.0fDB", -(1-float64(v)/127)*40)
		}
		c.cell(cv, i, engine.MasterNames[i], num3(v), small, float64(v)/127, i == engine.MELG || i == engine.MEHG, false)
	}
}

func (c *Controller) drawSeq(cv canvas) {
	p := c.pattern()
	kit := "--"
	if p.Kit >= 0 {
		kit = fmt.Sprintf("%03d", p.Kit+1)
	}
	poly := "OFF"
	if p.Poly {
		poly = "ON"
	}
	c.cell(cv, 0, "LENGTH", num3(p.Len), "STEPS", float64(p.Len)/64, false, false)
	c.cell(cv, 1, "SCALE", engine.ScaleNames[p.Scale], "", -1, false, false)
	c.cell(cv, 2, "SWING", fmt.Sprintf("%d%%", p.SwingAmt), "", float64(p.SwingAmt-50)/30, false, false)
	c.cell(cv, 3, "PAT KIT", kit, "AT START", -1, false, false)
	c.cell(cv, 4, "TRANS", fmt.Sprintf("%+d", p.Trans), "SEMITONES", float64(p.Trans+24)/48, true, false)
	c.cell(cv, 5, "POLY", poly, "ALL VOICES", -1, false, false)
	c.cell(cv, 6, "POLY TRK", fmt.Sprintf("T%d", p.PolyTrack+1), "", -1, false, false)
	c.cell(cv, 7, "TEMPO", fmt.Sprintf("%.1f", c.eng.Tempo()), "BPM", (c.eng.Tempo()-30)/270, false, false)
	c.drawStepEdit(cv, p)
}

// drawStepEdit shows the held step instead of the pattern cells.
func (c *Controller) drawStepEdit(cv canvas, p engine.Pattern) {
	for s := range c.held {
		st := p.Tracks[c.track].Step[s]
		if !st.On {
			return
		}
		cv.fill(0, cellsY, W, 96, colBG)
		trig := []string{"TRIGLESS", "AMP", "FLT", "AMP+FLT", "LFO", "AMP+LFO", "FLT+LFO", "ALL"}[st.Trig&7]
		var b [engine.MaxChord]uint8
		n := st.Notes(&b)
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = NoteName(int(b[i]))
		}
		c.cell(cv, 0, "NOTE", strings.Join(names, " "), fmt.Sprintf("STEP %d", s+1), -1, false, false)
		c.cell(cv, 1, "VEL", num3(int(st.Vel)), "", float64(st.Vel)/127, false, false)
		c.cell(cv, 2, "LEN", fmt.Sprintf("%.2f", float64(st.Len)/engine.LenUnits), "STEPS", float64(st.Len)/127, false, false)
		c.cell(cv, 3, "TRIG", trig, "", -1, false, false)
		cv.text(cellW*4+10, cellsY+30, 2, "HOLD PAD + PLAY KEY: NOTE", colFG)
		cv.text(cellW*4+10, cellsY+50, 2, "TURN PAGE KNOB: LOCK", colFG)
		return
	}
}

func (c *Controller) drawArp(cv canvas) {
	ts := c.eng.TrackSetup(c.track)
	a := ts.Arp
	fix := "OFF"
	if ts.TransFix {
		fix = "ON"
	}
	c.cell(cv, 0, "ARP", engine.ArpModeNames[a.Mode], fmt.Sprintf("TRACK %d", c.track+1), -1, false, false)
	c.cell(cv, 1, "SPEED", engine.ArpSpeedNames[a.Speed], "", float64(a.Speed)/6, false, false)
	c.cell(cv, 2, "RANGE", fmt.Sprintf("%d", a.Range), "OCTAVES", float64(a.Range-1)/3, false, false)
	c.cell(cv, 3, "LEN", num3(a.Len), "GATE", float64(a.Len)/127, false, false)
	c.cell(cv, 4, "PLAY", engine.ArpPlayNames[a.Play], "", -1, false, false)
	c.cell(cv, 5, "TRK TRANS", fmt.Sprintf("%+d", ts.Trans), "SEMITONES", float64(ts.Trans+48)/96, true, false)
	c.cell(cv, 6, "TRANS FIX", fix, "", -1, false, false)
	c.cell(cv, 7, "TEMPO", fmt.Sprintf("%.1f", c.eng.Tempo()), "BPM", (c.eng.Tempo()-30)/270, false, false)
}

func (c *Controller) drawSong(cv canvas) {
	var s engine.Song
	c.eng.ReadSong(c.songNum, func(x *engine.Song) { s = *x; s.Rows = append([]engine.SongRow(nil), x.Rows...) })
	on, _ := c.eng.SongMode()
	row, pat, rep, tr := "NEW", "---", "-", "-"
	if c.songRow < len(s.Rows) {
		r := s.Rows[c.songRow]
		row = fmt.Sprintf("%03d", c.songRow+1)
		pat, rep, tr = engine.PatternName(r.Pattern), fmt.Sprintf("x%d", r.Repeats), fmt.Sprintf("%+d", r.Trans)
	}
	loop := "STOP"
	if s.LoopTo >= 0 {
		loop = fmt.Sprintf("ROW %d", s.LoopTo+1)
	}
	c.cell(cv, 0, "ROW", row, "", -1, false, false)
	c.cell(cv, 1, "PATTERN", pat, "", -1, false, false)
	c.cell(cv, 2, "REPEAT", rep, "", -1, false, false)
	c.cell(cv, 3, "AT END", loop, "", -1, false, false)
	c.cell(cv, 4, "SONG", fmt.Sprintf("%02d", c.songNum+1), onOff(on), -1, false, false)
	c.cell(cv, 5, "TRANS", tr, "", -1, false, false)
	pos := c.eng.Position()
	for k := 0; k < 3; k++ {
		r := c.songRow - 1 + k
		y := cellsY + 40 + k*18
		if r < 0 || r > len(s.Rows) {
			continue
		}
		txt := "-- END --"
		if r < len(s.Rows) {
			txt = fmt.Sprintf("%03d  %s  x%d  %+d", r+1, engine.PatternName(s.Rows[r].Pattern), s.Rows[r].Repeats, s.Rows[r].Trans)
		}
		if r == c.songRow {
			cv.label(cellW*6+30, y, 2, pad(txt, 17))
		} else {
			cv.text(cellW*6+30, y, 2, txt, colFG)
		}
		if on && pos.Row == r {
			cv.text(cellW*6+8, y, 2, ">", colFG)
		}
	}
}

func (c *Controller) drawKit(cv canvas) {
	ts := c.eng.TrackSetup(c.track)
	yn := func(b bool) string {
		if b {
			return "ON"
		}
		return "OFF"
	}
	c.cell(cv, 0, "KIT", fmt.Sprintf("%03d", c.kitCursor+1), "LOADED "+fmt.Sprintf("%03d", c.eng.KitNumber()+1), -1, false, false)
	c.cell(cv, 1, "KEY HP", yn(ts.KeyHP), "TRACK "+fmt.Sprint(c.track+1), -1, false, false)
	c.cell(cv, 2, "KEY LP", yn(ts.KeyLP), "", -1, false, false)
	isFX := engine.Machines[ts.Machine].FX
	src := "N/A"
	small := "NOT AN FX TRACK"
	if isFX {
		src = fmt.Sprintf("T%d", c.srcCursor+1)
		small = yn(ts.Src&(1<<uint(c.srcCursor)) != 0)
	}
	c.cell(cv, 3, "SRC", src, small, -1, false, false)
	cv.text(cellW*4+10, cellsY+8, 2, "KIT: "+strings.ToUpper(c.eng.KitName()), colFG)
	cv.text(cellW*4+10, cellsY+30, 2, "FX TRACKS TAKE INPUT FROM", colFG)
	cv.text(cellW*4+10, cellsY+48, 2, "LOWER TRACKS (SRC).", colFG)
	cv.text(cellW*4+10, cellsY+72, 2, "PRESS KIT AGAIN: MASTER", colFG)
}

func (c *Controller) drawSystem(cv canvas) {
	files := c.host.ImportFiles()
	w := c.eng.Wave(c.waveSlot)
	name, size := "EMPTY", ""
	if w != nil {
		name = strings.ToUpper(w.Name)
		size = fmt.Sprintf("%.2fS", float64(len(w.Data))/float64(w.Rate))
	}
	c.cell(cv, 0, "UW SLOT", fmt.Sprintf("%02d", c.waveSlot+1), name, -1, false, false)
	f := "NONE"
	if len(files) > 0 {
		f = strings.ToUpper(files[clampi(c.fileCur, 0, len(files)-1)])
		if len(f) > 11 {
			f = f[:11]
		}
	}
	c.cell(cv, 1, "FILE", fmt.Sprintf("%d/%d", clampi(c.fileCur+1, 0, len(files)), len(files)), f, -1, false, false)
	sync := "INT"
	if c.eng.Sync() == engine.SyncMIDI {
		sync = "MIDI"
	}
	c.cell(cv, 6, "SYNC", sync, "CLOCK", -1, false, false)
	used, total := c.eng.WaveMemory()
	cv.text(cellW*2+10, cellsY+8, 2, fmt.Sprintf("USERWAVE MEMORY %d%%", used*100/total), colFG)
	cv.bar(cellW*2+10, cellsY+28, 4*cellW, 12, float64(used)/float64(total), colFG)
	cv.text(cellW*2+10, cellsY+50, 2, size, colFG)
	ok, msg := c.host.AudioStatus()
	st := "AUDIO OK"
	if !ok {
		st = "AUDIO: " + engine.CleanName(strings.SplitN(msg, "\n", 2)[0], 30)
	}
	cv.text(cellW*2+10, cellsY+72, 2, st, colFG)
}

func (c *Controller) drawMachineList(cv canvas) {
	n := len(engine.Machines)
	for k := -2; k <= 2; k++ {
		i := c.ovCursor + k
		if i < 0 || i >= n {
			continue
		}
		y := cellsY + 8 + (k+2)*18
		m := engine.Machines[i]
		txt := fmt.Sprintf("%02d %-10s %s", i+1, m.Name, m.Group)
		if k == 0 {
			cv.label(20, y, 2, pad(txt, 24))
		} else {
			cv.text(20, y, 2, txt, colFG)
		}
	}
	m := engine.Machines[c.ovCursor]
	for i := 0; i < 8; i++ {
		x := 400 + (i%4)*140
		y := cellsY + 12 + (i/4)*24
		cv.text(x, y, 2, fmt.Sprintf("%d %s", i+1, m.Params[i]), colFG)
	}
	cv.text(400, cellsY+62, 2, "TURN A: BROWSE", colFG)
	cv.text(400, cellsY+78, 2, "ENTER: ASSIGN  EXIT: CANCEL", colFG)
}

// NoteName is the name of a MIDI note: 60 is C-3 as on the Monomachine.
func NoteName(n int) string {
	names := [...]string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}
	n = clampi(n, 0, 127)
	return fmt.Sprintf("%s%d", names[n%12], n/12-2)
}

// drawRibbon draws the 16 steps of the selected track at the bottom.
func (c *Controller) drawRibbon(cv canvas) {
	pat := c.pattern()
	pos := c.eng.Position()
	for k := 0; k < 16; k++ {
		s := c.stepPage*16 + k
		x := k*60 + 2
		if s >= pat.Len {
			cv.fill(x, ribbonY+3, 56, 2, colDim)
			continue
		}
		on := pat.Tracks[c.track].Step[s].On
		switch c.layer {
		case layerSwing:
			on = pat.Swing[s]
		case layerSlide:
			on = pat.Slide[s]
		}
		if on {
			cv.fill(x, ribbonY, 56, 8, colFG)
		} else {
			cv.frame(x, ribbonY, 56, 8, colDim)
		}
		if pat.Tracks[c.track].Lock[s] != nil && c.layer == layerNone {
			cv.fill(x+24, ribbonY+2, 8, 4, colLight)
		}
		if pos.Playing && pos.Step == s {
			cv.frame(x-1, ribbonY-1, 58, 10, colWarn)
		}
		if s%4 == 0 {
			cv.fill(x, ribbonY-2, 2, 2, colFG)
		}
	}
}

func (c *Controller) softLabels() [8]string {
	var l [8]string
	switch c.page {
	case PgSyn:
		l[0], l[1], l[2] = "MACHINE", "<TRACK", "TRACK>"
		if c.ov == ovMachine {
			l[0] = "ASSIGN"
		}
	case PgAmp, PgFlt, PgFx, PgLfo:
		l[1], l[2], l[7] = "<TRACK", "TRACK>", "CLR LOCK"
	case PgSeq:
		l[0], l[1], l[2], l[3], l[4], l[5], l[6] = "CLR PAT", "COPY", "PASTE", "REC", "<SHIFT", "SHIFT>", "CLR TRK"
	case PgKit:
		l[0], l[1], l[2] = "LOAD", "SAVE", "INIT"
	case PgSong:
		l[0], l[1], l[2] = "ADD ROW", "DEL ROW", "SONG ON"
		if on, _ := c.eng.SongMode(); on {
			l[2] = "SONG OFF"
		}
	case PgSys:
		l[0], l[1], l[2] = "LOAD UW", "CLEAR UW", "SAVE"
	}
	return l
}

func (c *Controller) drawSoftKeys(cv canvas) {
	cv.fill(0, softY-1, W, 1, colDim)
	for i, s := range c.softLabels() {
		if s == "" {
			continue
		}
		cv.textC(i*cellW+cellW/2, softY+1, 2, s, colFG)
	}
}
