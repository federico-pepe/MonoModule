package main

import (
	"log"
	"net"
	"runtime"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
)

const busName = "MonoModule"

var busSocket = audiobus.SocketPath(audiobus.DefaultDir)

// busUp says whether a mixer answers on the audio bus socket.
func busUp() bool {
	c, err := net.DialTimeout("unix", busSocket, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// busSession is the render goroutine that feeds the mixer.
type busSession struct {
	stop chan struct{}
	done chan struct{}
}

func (s *busSession) close() {
	close(s.stop)
	<-s.done
}

func (a *app) startBus(c *audiobus.Client) *busSession {
	f := c.Format()
	a.eng.SetSampleRate(float64(f.Rate))
	a.channels.Store(int32(f.Channels))
	s := &busSession{stop: make(chan struct{}), done: make(chan struct{})}
	log.Printf("audio bus: %s on channels %d-%d, rate=%d period=%d", busName, c.Pair()+1, c.Pair()+2, f.Rate, f.Period)
	go a.renderBus(c, s, c.Generation(), f)
	return s
}

// renderBus is the real-time loop for the bus: engine -> interleaved stereo ->
// mixer. Write waits for room in the ring, which paces the loop to the card.
// It ends when the connection drops or renegotiates; superviseAudio restarts it.
func (a *app) renderBus(c *audiobus.Client, s *busSession, gen uint64, f audiobus.Format) {
	defer close(s.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !setRealtime(realtimePrio) {
		log.Printf("warning: SCHED_FIFO not available; running at normal priority")
	}
	l, r := make([]float32, f.Period), make([]float32, f.Period)
	buf := make([]float32, f.Period*2)
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		if c.Generation() != gen || !c.Connected() {
			return
		}
		a.eng.Render(l, r)
		for i := range l {
			buf[i*2], buf[i*2+1] = l[i], r[i]
		}
		_ = c.Write(buf) // a dropped block is silence; nothing to handle
	}
}

// busStep runs one supervisor tick in bus mode and returns how long to wait.
func (a *app) busStep(sess **busSession) time.Duration {
	c := a.busClient.Load()
	if c == nil {
		cfg := a.config()
		c = audiobus.Open(busSocket, busName, cfg.BusPair)
		a.busClient.Store(c)
	}
	if *sess != nil {
		select {
		case <-(*sess).done:
			*sess = nil
		default:
		}
	}
	if !c.Connected() {
		if *sess != nil {
			(*sess).close()
			*sess = nil
		}
		a.status.set(false, msgNoLive)
		return pollWaiting
	}
	if *sess == nil {
		*sess = a.startBus(c)
		a.status.set(true, "")
	}
	return pollSteady
}

// effectivePair is the first channel of the pair MonoModule plays on.
func (a *app) effectivePair() int {
	if a.busMode.Load() {
		if c := a.busClient.Load(); c != nil && c.Connected() {
			return c.Pair()
		}
	}
	return a.config().ChannelOffset
}
