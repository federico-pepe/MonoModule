package main

import (
	"sync"
	"sync/atomic"

	"monomodule/engine"
	"monomodule/store"
	"monomodule/ui"
)

// app is everything the pieces share. The engine has its own lock; cfg has
// cfgMu; the rest is set once.
type app struct {
	eng     *engine.Engine
	ui      *ui.Controller
	st      *store.Store
	hackDir string
	dataDir string
	pmURL   string

	cfgMu sync.Mutex
	cfg   store.Config

	status audioStatus
	focus  atomic.Bool // MonoModule owns the Push surface
}

func (a *app) config() store.Config {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	return a.cfg
}

func (a *app) setConfig(f func(*store.Config)) error {
	a.cfgMu.Lock()
	f(&a.cfg)
	c := a.cfg
	a.cfgMu.Unlock()
	return store.SaveConfig(a.hackDir, c)
}

// ---- ui.Host ----

func (a *app) ImportFiles() []string                  { return a.st.ImportFiles() }
func (a *app) ImportWave(slot int, file string) error { return a.st.ImportWave(slot, file) }
func (a *app) SaveProject() error                     { return a.st.Save() }
func (a *app) AudioStatus() (bool, string)            { return a.status.get() }
func (a *app) SyncChanged(m engine.SyncMode)          { a.eng.SetSync(m) }

// audioStatus says whether sound is going out.
type audioStatus struct {
	mu  sync.Mutex
	ok  bool
	msg string
}

func (s *audioStatus) set(ok bool, msg string) {
	s.mu.Lock()
	s.ok, s.msg = ok, msg
	s.mu.Unlock()
}

func (s *audioStatus) get() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ok, s.msg
}
