// MonoModule: a six-voice synth module for Ableton Push 3 in the style of
// the Elektron Monomachine. Six tracks of machines with filters, LFOs and
// per-track effects, a pitched step sequencer with parameter locks, an
// arpeggiator and a POLY mode, rendered into the Push Hack virtual audio card
// and played from Push's own screen and pads. Not affiliated with Elektron;
// the sound engine is original.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/hackcfg"

	"monomodule/engine"
	"monomodule/store"
	"monomodule/ui"
)

const (
	defaultPushManagerURL = "http://localhost:7701"
	defaultWebPort        = 7712
)

func main() {
	// A crash in the audio or ALSA code would otherwise leave the service
	// dead until someone restarts it; run as a supervised child.
	if os.Getenv("MONO_SUPERVISED") != "1" {
		runSupervisor()
		return
	}
	runModule()
}

func runSupervisor() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	backoff := time.Second
	for {
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), "MONO_SUPERVISED=1")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			log.Printf("starting child: %v; retrying in %v", err, backoff)
			time.Sleep(backoff)
			continue
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		start := time.Now()
		select {
		case s := <-sig:
			_ = cmd.Process.Signal(s)
			<-done
			return
		case err := <-done:
			log.Printf("child exited after %v: %v", time.Since(start), err)
		}
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		time.Sleep(backoff)
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func runModule() {
	configPath := flag.String("config", "hack.json", "path to hack.json")
	flag.Parse()
	hackDir, err := filepath.Abs(filepath.Dir(*configPath))
	if err != nil {
		log.Fatalf("resolving hack dir: %v", err)
	}
	alsaseq.WaitForBootSettle()

	cfg, err := store.LoadConfig(hackDir)
	if err != nil {
		log.Printf("config: %v (using defaults)", err)
	}
	a := &app{hackDir: hackDir, dataDir: filepath.Join(hackDir, "data"), cfg: cfg, pmURL: cfg.PushManagerURL}
	if a.pmURL == "" {
		a.pmURL = defaultPushManagerURL
	}
	a.storeMirror(cfg)
	a.status.set(false, msgNoCard)
	a.eng = engine.New(44100)
	if a.st, err = store.Open(a.dataDir, a.eng); err != nil {
		log.Fatalf("data folder: %v", err)
	}
	if err := a.st.Load(); err != nil {
		log.Printf("project: %v (starting from the factory project)", err)
	}
	a.eng.TakeDirty()
	a.ui = ui.New(a.eng, a)

	shutdown := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; log.Print("stopping"); close(shutdown) }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-shutdown; cancel() }()

	hcfg, err := hackcfg.Load(*configPath, defaultWebPort)
	if err != nil {
		log.Printf("%v (web port %d)", err, defaultWebPort)
		hcfg.Port = defaultWebPort
	}

	saved := make(chan struct{})
	go func() { a.st.Autosave(shutdown); close(saved) }()
	go a.watchDependencies(shutdown)
	go a.runSurface(ctx)
	go a.runWeb(hcfg.Port, shutdown)
	go a.superviseMIDI(shutdown)
	a.superviseAudio(shutdown)

	<-saved
	log.Print("stopped")
}
