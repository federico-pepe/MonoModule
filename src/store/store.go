package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"monomodule/engine"
)

// Store keeps the project and its waves in a folder:
//
//	project.json        patterns, kits, songs, tempo, the working kit
//	waves/NN.wav        UserWave slots (16 bit mono) and waves/index.json (names)
//	imports/            WAV files waiting to be loaded into a slot
type Store struct {
	dir string
	eng *engine.Engine

	mu    sync.Mutex
	saved [engine.NumUserWave]*engine.UserWave // the wave each file holds
}

// MaxImportBytes caps one WAV file read from the imports folder or an upload.
const MaxImportBytes = 64 << 20

// Open makes the folders.
func Open(dir string, e *engine.Engine) (*Store, error) {
	for _, d := range []string{dir, filepath.Join(dir, "waves"), filepath.Join(dir, "imports")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir, eng: e}, nil
}

func (s *Store) projectPath() string { return filepath.Join(s.dir, "project.json") }
func (s *Store) wavePath(n int) string {
	return filepath.Join(s.dir, "waves", fmt.Sprintf("%02d.wav", n+1))
}
func (s *Store) indexPath() string { return filepath.Join(s.dir, "waves", "index.json") }

// Load reads the project and waves into the engine. A missing project is not
// an error: the engine keeps its factory project. A damaged file is moved
// aside (project.json.bad) so it is not overwritten.
func (s *Store) Load() error {
	b, err := os.ReadFile(s.projectPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := engine.UnmarshalProject(b)
	if err != nil {
		_ = os.Rename(s.projectPath(), s.projectPath()+".bad")
		return fmt.Errorf("project.json is damaged (kept as project.json.bad): %w", err)
	}
	names := map[string]string{}
	if ib, err := os.ReadFile(s.indexPath()); err == nil {
		_ = json.Unmarshal(ib, &names)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < engine.NumUserWave; i++ {
		f, err := os.Open(s.wavePath(i))
		if err != nil {
			continue
		}
		data, rate, err := engine.DecodeWAV(io.LimitReader(f, MaxImportBytes))
		f.Close()
		if err != nil || len(data) < 32 {
			log.Printf("wave %d: %v (skipped)", i+1, err)
			continue
		}
		name := names[fmt.Sprintf("%02d", i+1)]
		if name == "" {
			name = fmt.Sprintf("UW%02d", i+1)
		}
		w := &engine.UserWave{Name: engine.CleanWaveName(name), Rate: rate, Data: data}
		p.Waves[i] = w
		s.saved[i] = w
	}
	s.eng.SetProject(p)
	s.eng.TakeDirty()
	return nil
}

// Save writes the project and any waves that changed.
func (s *Store) Save() error {
	b, waves, err := s.eng.MarshalProject()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	names := map[string]string{}
	for i, w := range waves {
		if w != s.saved[i] {
			if w == nil {
				_ = os.Remove(s.wavePath(i))
			} else if err := writeAtomic(s.wavePath(i), engine.EncodeWAV16(w.Data, w.Rate, 1)); err != nil {
				return err
			}
			s.saved[i] = w
		}
		if w != nil {
			names[fmt.Sprintf("%02d", i+1)] = w.Name
		}
	}
	ib, _ := json.MarshalIndent(names, "", " ")
	if err := writeAtomic(s.indexPath(), ib); err != nil {
		return err
	}
	return writeAtomic(s.projectPath(), b)
}

// Autosave saves whenever the engine says the project changed, at most every
// two seconds, until stop closes. It saves once more on the way out.
func (s *Store) Autosave(stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			if s.eng.TakeDirty() {
				if err := s.Save(); err != nil {
					log.Printf("final save: %v", err)
				}
			}
			return
		case <-t.C:
			if s.eng.TakeDirty() {
				if err := s.Save(); err != nil {
					log.Printf("autosave: %v", err)
				}
			}
		}
	}
}

// ---------------------------------------------------------------- imports

// SafeName reduces a file name to a plain WAV name inside the imports folder.
func SafeName(name string) (string, error) {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if base == "." || base == "/" || strings.HasPrefix(base, ".") {
		return "", errors.New("bad file name")
	}
	if !strings.EqualFold(filepath.Ext(base), ".wav") {
		return "", errors.New("only .wav files can be imported")
	}
	return base, nil
}

// ImportFiles lists the WAV files in the imports folder, sorted.
func (s *Store) ImportFiles() []string {
	ents, err := os.ReadDir(filepath.Join(s.dir, "imports"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			if n, err := SafeName(e.Name()); err == nil {
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ImportWave loads a file from the imports folder into a UserWave slot.
func (s *Store) ImportWave(slot int, file string) error {
	n, err := SafeName(file)
	if err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(s.dir, "imports", n))
	if err != nil {
		return err
	}
	defer f.Close()
	return s.eng.LoadWAV(slot, strings.TrimSuffix(n, filepath.Ext(n)), io.LimitReader(f, MaxImportBytes))
}

// UploadWave loads an uploaded WAV into a slot and keeps a copy in imports.
func (s *Store) UploadWave(slot int, file string, r io.Reader) error {
	n, err := SafeName(file)
	if err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(r, MaxImportBytes+1))
	if err != nil {
		return err
	}
	if len(b) > MaxImportBytes {
		return errors.New("file is too large")
	}
	if err := s.eng.LoadWAV(slot, strings.TrimSuffix(n, filepath.Ext(n)), bytes.NewReader(b)); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, "imports", n), b)
}

// ProjectJSON is the project as a download.
func (s *Store) ProjectJSON() ([]byte, error) {
	b, _, err := s.eng.MarshalProject()
	return b, err
}

// ReplaceProject loads an uploaded project.json (the waves stay as they are).
func (s *Store) ReplaceProject(b []byte) error {
	p, err := engine.UnmarshalProject(b)
	if err != nil {
		return err
	}
	for i := 0; i < engine.NumUserWave; i++ {
		p.Waves[i] = s.eng.Wave(i)
	}
	s.eng.SetProject(p)
	return nil
}
