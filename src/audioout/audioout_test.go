package audioout

import (
	"testing"

	"github.com/federico-pepe/ableton-push-hack/core/alsapcm"
)

func TestDevices(t *testing.T) {
	list := []alsapcm.PlaybackDevice{{CardIndex: 1, CardID: "PHVAudio", Device: 1, Name: "Loopback PCM"}}
	got := Devices(list, "hw:PHVAudio,1,0")
	if len(got) != 1 || !got[0].Current {
		t.Fatalf("%+v", got)
	}
	got = Devices(list, "hw:Gone,0,0")
	if len(got) != 2 || !got[1].Current || got[0].Current {
		t.Fatalf("unlisted current device: %+v", got)
	}
	if HasDevice(list, "hw:Gone,0,0") || !HasDevice(list, got[0].Value) {
		t.Fatal("HasDevice")
	}
}

func TestChannels(t *testing.T) {
	p := Channels(6, false, 2)
	if len(p) != 3 || p[1].Label != "Ch 3-4" || !p[1].Current || p[0].Current {
		t.Fatalf("%+v", p)
	}
	m := Channels(4, true, 2)
	if len(m) != 4 || m[2].Label != "Ch 3" || !m[2].Current {
		t.Fatalf("%+v", m)
	}
	if n := len(Channels(0, false, 0)); n != LoopbackChannels/2 {
		t.Fatalf("default has %d pairs", n)
	}
	odd := Channels(6, false, 1)
	if len(odd) != 4 || !odd[3].Current || odd[3].Value != "1" {
		t.Fatalf("odd current offset: %+v", odd)
	}
}

func TestValidChannel(t *testing.T) {
	if !ValidChannel(6, false, 4) || ValidChannel(6, false, 6) || ValidChannel(6, false, 3) || ValidChannel(6, false, -2) {
		t.Fatal("stereo")
	}
	if !ValidChannel(6, true, 5) || ValidChannel(6, true, 6) || ValidChannel(6, true, -1) {
		t.Fatal("mono")
	}
}
