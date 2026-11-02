package main

import (
	"log"
	"runtime"
	"time"

	"monomodule/hwparams"
	"monomodule/outmix"
)

const (
	cardID       = "PHVAudio"
	pollSteady   = 2 * time.Second
	pollWaiting  = time.Second
	msgNoCard    = "No loopback card.\nInstall push-audio-loopback."
	msgNoLive    = "1. Push Audio Settings > Devices.\n2. Enable an input of the\n   Push Hack Virtual Audio card.\n3. Set an audio track's input to it\n   and turn Monitor on."
	realtimePrio = 50
)

// session is one open PCM stream and the goroutine that renders into it.
type session struct {
	pcm  *pcmOut
	stop chan struct{}
	done chan struct{}
}

func (s *session) close() {
	close(s.stop)
	<-s.done
}

func (a *app) startSession(hp hwparams.Params, device string) (*session, error) {
	a.eng.SetSampleRate(float64(hp.Rate))
	pcm, err := openPCM(device, hp.Channels, hp.Rate, hp.Period, hp.Buffer)
	if err != nil {
		return nil, err
	}
	s := &session{pcm: pcm, stop: make(chan struct{}), done: make(chan struct{})}
	log.Printf("audio open: %s channels=%d rate=%d period=%d", device, pcm.Channels(), hp.Rate, pcm.Period())
	go a.render(s, hp.Rate)
	return s, nil
}

// render is the real-time loop: engine -> int16 -> the chosen channel pair.
// It allocates nothing after the buffers are made.
func (a *app) render(s *session, rate int) {
	defer close(s.done)
	defer s.pcm.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !setRealtime(realtimePrio) {
		log.Printf("warning: SCHED_FIFO not available; running at normal priority")
	}
	period, ch := s.pcm.Period(), s.pcm.Channels()
	l, r := make([]float32, period), make([]float32, period)
	wide := make([]int16, period*ch)
	var blocks, failed int
	for {
		select {
		case <-s.stop:
			log.Printf("audio stopped after %d blocks (%d write errors)", blocks, failed)
			return
		default:
		}
		a.eng.Render(l, r)
		cfg := a.config()
		mode, off := outmix.Fit(cfg.OutputMode, cfg.ChannelOffset, ch)
		outmix.Mix(wide, l, r, ch, off, mode)
		if s.pcm.Write(wide, period) < 0 {
			failed++
			continue
		}
		blocks++
	}
}

// superviseAudio waits for the card and for Live to open it, then keeps a
// session running that matches what Live negotiated, reopening when that or
// the chosen device changes. It returns when shutdown closes.
func (a *app) superviseAudio(shutdown <-chan struct{}) {
	var sess *session
	var last hwparams.Params
	var lastDev string
	stop := func() {
		if sess != nil {
			sess.close()
			sess = nil
		}
	}
	defer stop()
	lastState := ""
	note := func(s string) {
		if s != lastState {
			lastState = s
			log.Print(s)
		}
	}
	for {
		select {
		case <-shutdown:
			return
		default:
		}
		wait := pollWaiting
		hp, ok, err := hwparams.Read(cardID)
		switch {
		case err != nil:
			log.Printf("hw_params: %v", err)
			a.status.set(false, msgNoLive)
			stop()
		case !ok:
			if cardPresent(cardID) {
				note("waiting for Live to open " + cardID)
				a.status.set(false, msgNoLive)
			} else {
				note("waiting for " + cardID + " (push-audio-loopback)")
				a.status.set(false, msgNoCard)
			}
			stop()
		default:
			dev := a.config().PCMDevice
			if sess == nil || hp != last || dev != lastDev {
				stop()
				s, err := a.startSession(hp, dev)
				if err != nil {
					log.Printf("opening %s: %v; will retry", dev, err)
					a.status.set(false, msgNoLive)
				} else {
					sess, last, lastDev = s, hp, dev
					note("running")
					a.status.set(true, "")
				}
			}
			if sess != nil {
				wait = pollSteady
			}
		}
		select {
		case <-shutdown:
			return
		case <-time.After(wait):
		}
	}
}
