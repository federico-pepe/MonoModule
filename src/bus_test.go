package main

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
)

// TestBusPath plays the engine into a real audiobus server (a stand-in for the
// mixer in push-audio-loopback) and checks that the sound lands on the pair
// the mixer gave, and nowhere else.
func TestBusPath(t *testing.T) {
	dir, err := os.MkdirTemp("", "abus")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	srv := audiobus.NewServer(dir)
	f := audiobus.Format{Rate: 48000, Period: 256, Channels: 8}
	srv.SetFormat(f)
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	busSocket = audiobus.SocketPath(dir)
	if !busUp() {
		t.Fatal("busUp: the server does not answer")
	}

	a := newTestApp(t)
	a.eng.Play()
	c := audiobus.Open(busSocket, busName, 4)
	defer c.Close()
	for i := 0; i < 100 && !c.Connected(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !c.Connected() || c.Pair() != 4 {
		t.Fatalf("client: connected=%v pair=%d", c.Connected(), c.Pair())
	}
	s := a.startBus(c)

	var peak [8]float64
	out := make([]float32, f.Period*f.Channels)
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		srv.Mix(out)
		for i, v := range out {
			if m := math.Abs(float64(v)); m > peak[i%f.Channels] {
				peak[i%f.Channels] = m
			}
		}
		time.Sleep(time.Duration(f.Period) * time.Second / time.Duration(f.Rate))
	}
	s.close()

	for ch := range peak {
		if (ch == 4 || ch == 5) != (peak[ch] > 0.001) {
			t.Fatalf("channel %d peak %v: sound must be on the mixer's pair 5/6 only", ch, peak)
		}
	}
	if got := int(a.channels.Load()); got != 8 {
		t.Fatalf("channels = %d, want 8", got)
	}
}
