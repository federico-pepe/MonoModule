package engine

// Editing the pattern and song data. Every method takes the engine lock, so
// the screens and the web page can call them while the engine plays.

// CurrentPattern is the pattern that plays (or is edited when stopped).
func (e *Engine) CurrentPattern() int { e.mu.Lock(); defer e.mu.Unlock(); return e.curPat }

// ReadPattern calls f with pattern n while the lock is held. f must not keep
// the pointer.
func (e *Engine) ReadPattern(n int, f func(*Pattern)) {
	e.mu.Lock()
	f(e.proj.Pattern(n))
	e.mu.Unlock()
}

// ReadCurrent calls f with the current pattern.
func (e *Engine) ReadCurrent(f func(*Pattern)) {
	e.mu.Lock()
	f(e.proj.Pattern(e.curPat))
	e.mu.Unlock()
}

// EditPattern calls f on pattern n and brings the result into range.
func (e *Engine) EditPattern(n int, f func(*Pattern)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.proj.Pattern(n)
	wasPoly, wasTrack := p.Poly, p.PolyTrack
	f(p)
	p.normalise()
	if p.Poly && Machines[e.kit.Track[p.PolyTrack].Machine].FX {
		p.Poly = false // an FX machine cannot take all voices
	}
	if e.proj.Pattern(e.curPat) == p && (wasPoly != p.Poly || wasTrack != p.PolyTrack) {
		for _, v := range e.voices {
			v.noteOff()
		}
		e.syncVoicesLocked()
	}
	if e.nextStep > p.Len && e.proj.Pattern(e.curPat) == p {
		e.nextStep = p.Len
	}
	e.dirty = true
}

// EditCurrent is EditPattern for the current pattern.
func (e *Engine) EditCurrent(f func(*Pattern)) {
	e.EditPattern(e.CurrentPattern(), f)
}

// EditStep changes one step of the current pattern.
func (e *Engine) EditStep(track, step int, f func(*Step)) {
	if !okTrack(track) || step < 0 || step >= MaxSteps {
		return
	}
	e.EditCurrent(func(p *Pattern) {
		tp := &p.Tracks[track]
		f(&tp.Step[step])
		if !tp.Step[step].On {
			tp.Lock[step] = nil
		}
	})
}

// ToggleStep turns a trig on (with note, or the track's last note) or off.
func (e *Engine) ToggleStep(track, step, note int) {
	e.EditStep(track, step, func(s *Step) {
		if s.On {
			*s = Step{}
			return
		}
		*s = NewNoteStep(note, DefaultVel)
	})
}

// SetStepNote sets the note of a step, turning the trig on if needed. With
// add and a poly pattern the note joins the chord.
func (e *Engine) SetStepNote(track, step, note int, add bool) {
	e.EditCurrent(func(p *Pattern) {
		s := &p.Tracks[track].Step[step]
		note = clampi(note, 0, 127)
		switch {
		case !s.On || s.Off:
			*s = NewNoteStep(note, DefaultVel)
		case add && p.Poly:
			s.AddNote(note)
		default:
			s.Note, s.N = uint8(note), 0
		}
	})
}

// SetStepOff makes a step a note-off step.
func (e *Engine) SetStepOff(track, step int) {
	e.EditStep(track, step, func(s *Step) { *s = Step{On: true, Off: true, Trig: TrigAll} })
}

// LockParam sets a parameter lock on a step; the step must have a trig.
func (e *Engine) LockParam(track, step, param, v int) {
	if !okTrack(track) || step < 0 || step >= MaxSteps || param < 0 || param >= NParams {
		return
	}
	e.EditCurrent(func(p *Pattern) {
		tp := &p.Tracks[track]
		if !tp.Step[step].On {
			return
		}
		l := tp.Lock[step]
		if l == nil {
			l = &Lock{}
			tp.Lock[step] = l
		}
		l.Mask |= 1 << uint(param)
		l.Val[param] = uint8(clampi(v, 0, 127))
	})
}

// ClearLock removes one lock of a step (param >= 0) or all of them (param < 0).
func (e *Engine) ClearLock(track, step, param int) {
	if !okTrack(track) || step < 0 || step >= MaxSteps {
		return
	}
	e.EditCurrent(func(p *Pattern) {
		tp := &p.Tracks[track]
		l := tp.Lock[step]
		if l == nil {
			return
		}
		if param < 0 {
			tp.Lock[step] = nil
			return
		}
		l.Mask &^= 1 << uint(param)
		if l.Mask == 0 {
			tp.Lock[step] = nil
		}
	})
}

// Lock returns a copy of a step's lock, if it has one.
func (e *Engine) StepLock(track, step int) (Lock, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !okTrack(track) || step < 0 || step >= MaxSteps {
		return Lock{}, false
	}
	l := e.proj.Pattern(e.curPat).Tracks[track].Lock[step]
	if l == nil {
		return Lock{}, false
	}
	return *l, true
}

// ToggleSwing and ToggleSlide flip a step of the shared layers.
func (e *Engine) ToggleSwing(step int) {
	if step >= 0 && step < MaxSteps {
		e.EditCurrent(func(p *Pattern) { p.Swing[step] = !p.Swing[step] })
	}
}

func (e *Engine) ToggleSlide(step int) {
	if step >= 0 && step < MaxSteps {
		e.EditCurrent(func(p *Pattern) { p.Slide[step] = !p.Slide[step] })
	}
}

// ClearPattern empties pattern n (keeping nothing).
func (e *Engine) ClearPattern(n int) {
	e.EditPattern(n, func(p *Pattern) { *p = *NewPattern() })
}

// ClearTrack empties one track of the current pattern.
func (e *Engine) ClearTrack(track int) {
	if !okTrack(track) {
		return
	}
	e.EditCurrent(func(p *Pattern) { p.Tracks[track] = TrackPat{} })
}

// CopyPattern copies pattern from over pattern to.
func (e *Engine) CopyPattern(from, to int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if from == to {
		return
	}
	e.proj.Patterns[clampi(to, 0, NumPatterns-1)] = e.proj.Pattern(from).Clone()
	e.dirty = true
}

// ShiftTrack rotates a track's steps by delta positions inside the pattern.
func (e *Engine) ShiftTrack(track, delta int) {
	if !okTrack(track) {
		return
	}
	e.EditCurrent(func(p *Pattern) {
		n := p.Len
		tp := &p.Tracks[track]
		var ns [MaxSteps]Step
		var nl [MaxSteps]*Lock
		for i := 0; i < n; i++ {
			j := ((i+delta)%n + n) % n
			ns[j], nl[j] = tp.Step[i], tp.Lock[i]
		}
		for i := 0; i < n; i++ {
			tp.Step[i], tp.Lock[i] = ns[i], nl[i]
		}
	})
}

// TransposeTrack changes every note of a track of the current pattern.
func (e *Engine) TransposeTrack(track, semis int) {
	if !okTrack(track) {
		return
	}
	e.EditCurrent(func(p *Pattern) {
		tp := &p.Tracks[track]
		for i := range tp.Step {
			s := &tp.Step[i]
			if !s.On || s.Off {
				continue
			}
			s.Note = uint8(clampi(int(s.Note)+semis, 0, 127))
			for k := 0; k < int(s.N); k++ {
				s.Extra[k] = uint8(clampi(int(s.Extra[k])+semis, 0, 127))
			}
		}
	})
}

// ------------------------------------------------------------------ songs

// ReadSong calls f with song n while the lock is held.
func (e *Engine) ReadSong(n int, f func(*Song)) {
	e.mu.Lock()
	f(e.proj.Song(n))
	e.mu.Unlock()
}

// EditSong changes song n and brings it into range.
func (e *Engine) EditSong(n int, f func(*Song)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.proj.Song(n)
	f(s)
	if len(s.Rows) > MaxSongRows {
		s.Rows = s.Rows[:MaxSongRows]
	}
	for i := range s.Rows {
		r := &s.Rows[i]
		r.Pattern = clampi(r.Pattern, 0, NumPatterns-1)
		r.Repeats = clampi(r.Repeats, 1, 99)
		r.Trans = clampi(r.Trans, -24, 24)
	}
	if s.LoopTo >= len(s.Rows) || s.LoopTo < -1 {
		s.LoopTo = -1
	}
	s.Name = CleanName(s.Name, 12)
	e.dirty = true
}

// SetPatternKit sets the kit a pattern loads when it starts (-1 none).
func (e *Engine) SetPatternKit(n, kit int) {
	e.EditPattern(n, func(p *Pattern) { p.Kit = kit })
}
