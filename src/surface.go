package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/pmclient"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	appID     = "monomodule"
	frameRate = 100 * time.Millisecond
)

var ledHTTP = &http.Client{Timeout: 2 * time.Second}

func (a *app) pm() *pmclient.Client {
	c := pmclient.New(a.pmURL)
	c.App = appID
	return c
}

func postLED(pm, body string) {
	req, err := http.NewRequest(http.MethodPost, pm+"/api/midi/led", bytes.NewBufferString(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if resp, err := ledHTTP.Do(req); err == nil {
		resp.Body.Close()
	}
}

func ledCC(pm string, cc, v byte) {
	postLED(pm, fmt.Sprintf(`{"type":"cc","channel":0,"cc":%d,"value":%d}`, cc, v))
}

func ledPad(pm string, note, v byte) {
	postLED(pm, fmt.Sprintf(`{"type":"note","channel":0,"note":%d,"velocity":%d}`, note, v))
}

// allCCs is every control-surface light the panel may touch; the encoders are
// not lights. They are all blanked when the app takes the surface, and again
// when it gives it up.
var allCCs = []byte{
	push3.CCScreenTop1, push3.CCScreenTop2, push3.CCScreenTop3, push3.CCScreenTop4,
	push3.CCScreenTop5, push3.CCScreenTop6, push3.CCScreenTop7, push3.CCScreenTop8,
	push3.CCScreenBot1, push3.CCScreenBot2, push3.CCScreenBot3, push3.CCScreenBot4,
	push3.CCScreenBot5, push3.CCScreenBot6, push3.CCScreenBot7, push3.CCScreenBot8,
	push3.CCDPadUp, push3.CCDPadDown, push3.CCDPadLeft, push3.CCDPadRight, push3.CCDPadCenter,
	push3.CCSet, push3.CCSettings, push3.CCUserMode,
	push3.CCDeviceView, push3.CCMixerView, push3.CCClipView, push3.CCSessionView,
	push3.CCShift, push3.CCSelect,
	push3.CCUndo, push3.CCSave, push3.CCAdd, push3.CCSwap,
	push3.CCLock, push3.CCStopClips, push3.CCMute, push3.CCSolo, push3.CCSelectMain,
	push3.CCTapTempo, push3.CCMetronome, push3.CCQuantize, push3.CCFixedLength,
	push3.CCAutomate, push3.CCNew, push3.CCCapture, push3.CCRecord, push3.CCPlay,
	push3.CCScene14, push3.CCScene14t, push3.CCScene18, push3.CCScene18t,
	push3.CCScene116, push3.CCScene116t, push3.CCScene132, push3.CCScene132t,
	push3.CCRepeat, push3.CCAccent, push3.CCScale, push3.CCLayout, push3.CCNote, push3.CCSession,
	push3.CCDoubleLoop, push3.CCDuplicate, push3.CCConvert, push3.CCDelete,
	push3.CCOctaveUp, push3.CCOctaveDown, push3.CCPageLeft, push3.CCPageRight,
}

func allPads() []byte {
	n := make([]byte, 0, 64)
	for r := 0; r < 8; r++ {
		for c := 0; c < 8; c++ {
			n = append(n, push3.PadNote(c, r))
		}
	}
	return n
}

// ledState remembers what was last sent, so only changes cost a request.
type ledState struct {
	cc  map[byte]int
	pad [64]int
}

func newLEDState() *ledState {
	s := &ledState{cc: map[byte]int{}}
	s.forget()
	return s
}

// forget makes the next sync send everything.
func (s *ledState) forget() {
	for _, c := range allCCs {
		s.cc[c] = -1
	}
	for i := range s.pad {
		s.pad[i] = -1
	}
}

func (a *app) syncLEDs(s *ledState) {
	btn, pads := a.ui.LEDs()
	for _, c := range allCCs {
		v := 0
		if x, ok := btn[c]; ok {
			v = int(x)
		}
		if s.cc[c] != v {
			ledCC(a.pmURL, c, byte(v))
			s.cc[c] = v
		}
	}
	for c, x := range btn { // lights outside the blanking list (encoders' touch etc.)
		if _, known := s.cc[c]; !known {
			ledCC(a.pmURL, c, x)
			s.cc[c] = int(x)
		}
	}
	for i, note := range allPads() {
		if s.pad[i] != int(pads[i]) {
			ledPad(a.pmURL, note, pads[i])
			s.pad[i] = int(pads[i])
		}
	}
}

func (a *app) releaseLEDs() {
	for _, c := range allCCs {
		ledCC(a.pmURL, c, 0)
	}
	for _, n := range allPads() {
		ledPad(a.pmURL, n, 0)
	}
}

// runSurface is MonoModule as an app of Push Hack Home: draw and light
// while it owns the surface.
func (a *app) runSurface(ctx context.Context) {
	frames := make(chan bool, 1) // true: focus, false: blur
	go func() {
		cl := a.pm()
		leds := newLEDState()
		tick := time.NewTicker(frameRate)
		defer tick.Stop()
		on := false
		for {
			select {
			case <-ctx.Done():
				if on {
					a.releaseLEDs()
				}
				return
			case f := <-frames:
				on = f
				a.focus.Store(f)
				if f {
					leds.forget()
				} else {
					a.releaseLEDs()
				}
			case <-tick.C:
				if !on {
					continue
				}
				if err := cl.Push(a.ui.Render()); err != nil {
					log.Printf("display: %v", err)
				}
				a.syncLEDs(leds)
			}
		}
	}()
	send := func(f bool) {
		select {
		case <-frames:
		default:
		}
		frames <- f
	}
	err := a.pm().RunSurface(ctx, []int{push3.CCPlay}, pmclient.SurfaceHandler{
		OnFocus: func() { log.Print("focus: MonoModule has the surface"); send(true) },
		OnBlur:  func() { log.Print("blur: surface returned"); send(false) },
	})
	if errors.Is(err, pmclient.ErrNoSwitcher) {
		log.Printf("push-manager has no Push Hack Home: update Push Hack to open MonoModule from Home")
	}
}

// watchDependencies logs once per change whether push-manager and push-display
// are there; without them nothing can be drawn on Push.
func (a *app) watchDependencies(shutdown <-chan struct{}) {
	cl := pmclient.New(a.pmURL)
	cl.HTTP = &http.Client{Timeout: 2 * time.Second}
	last := -1
	for {
		state := 2
		if st, err := cl.DisplayStatus(); err != nil {
			state = 0
		} else if !st.Connected {
			state = 1
		}
		if state != last {
			last = state
			switch state {
			case 0:
				log.Printf("WARNING: push-manager is not reachable at %s; the Push screen will not work", a.pmURL)
			case 1:
				log.Printf("WARNING: push-display is not connected; the Push screen will not work")
			default:
				log.Printf("push-manager and push-display OK")
			}
		}
		if !sleepOr(30*time.Second, shutdown) {
			return
		}
	}
}
