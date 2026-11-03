# CLAUDE.md

Guidance for Claude Code in this repository.

## What this is

MonoModule: a [push-hack](https://github.com/federico-pepe/ableton-push-hack) module for Ableton Push 3 that recreates the *model* of an Elektron Monomachine SFX-60 MKII (six tracks, machines, filter, LFOs, pitched sequencer, POLY, arp, kits, songs). A Go host renders audio (pure-Go engine) into push-audio-loopback's virtual card, draws a front panel on Push's screen, and serves a web page. Not affiliated with Elektron; the DSP is original and not matched to the hardware.

## Layout (`src/`)

| Package | Role | Needs hardware? |
|---|---|---|
| `engine/` | DSP and sequencer. `params`/`machines`/`gens`/`tables`/`fxmachines` (sound), `voice` (one of six voices: gen, AHDR, HP+LP filter, 3 LFOs), `engine` (tracks, FX chain, render), `player` (96 PPQN clock, steps, transport, MIDI clock, live notes, recording), `arp`, `pattern`, `project` (data + JSON), `edit`, `master`. One mutex guards `Engine`; `Render` holds it for one block. No allocations in `Render`. | no |
| `ui/` | Front panel: pads/buttons/encoders in, 960x160 frame and LED map out, through the `Host` interface. | no |
| `store/` | config, project.json, waves as WAV, imports folder, autosave | no |
| `midimap/` | what notes/CCs mean (channel n = track n) | no |
| `hwparams/` | parses Live's negotiated hw_params | no |
| `livelink/` | PushHack Remote Script client (`track.ensure`) | no |
| `audioout/` | what the web page offers for the output: devices, channels or pairs | no |
| `cmd/mmrender/`, `cmd/mmscreens/` | demo/tour WAV; Push screen PNGs | no |
| main package | `main.go` (supervisor re-exec, `MONO_SUPERVISED`), `audio.go` (supervisor, direct card path), `bus.go` (audio bus client), `pcm.go`/`bridge.c` (cgo ALSA playback, direct path only), `midi.go` (port "MonoModule MIDI In"), `surface.go`, `web.go` + `web/index.html`, `app.go` | yes (cgo + libasound) |

Design notes: voices hold their generators in one struct (no interfaces, no allocation); parameters resolve at control rate (32 samples); a track's FX chain is FX machine, filter-page, EQ, SRR, delay; FX machines take lower-numbered tracks by source mask; `Render` splits blocks at step and arp events. Parameter locks use a uint64 mask over 56 params.

## Rules (from the Push Hack framework)

- Never write the display shm directly: frames go through `pmclient.Push` while the app owns the surface; LEDs go through `POST /api/midi/led`.
- On-screen text is ASCII only. Never hardcode ports (`hack.json` "port" 7712 is only a default).
- Push controls count only while focused (`app.focus`), and only from Push 3 client 16 port 0, channel 0 for CCs.
- The render goroutine is pinned (`LockOSThread`, SCHED_FIFO); keep it allocation-free (`TestRenderAllocFree`).
- Audio goes through the push-audio-loopback mixer when one answers on the bus socket (`bus.go`, `core/audiobus`): float stereo blocks to the mixer, which owns the card and the channel pair (`bus_pair` in the config, -1 = the mixer chooses). The mixer plays pairs only, so `output_mode` (stereo/mono) and `pcm_device` apply to the direct path. Without a mixer: open `hw:PHVAudio,1,0` with the params Live negotiated (`hwparams`), S16_LE interleaved, channels at `channel_offset` per `output_mode` (`outmix`). `superviseAudio` switches by itself. Design: `docs/audio-bus.md` in ableton-push-hack.
- `core/audiobus` comes from `core/v0.5.3` (pinned in `src/go.mod`). It needs push-audio-loopback 0.2.0-alpha or later for bus mode.
- Create tracks must never switch the user's channel; `livelink.EnsureTracks` reports what Live offers. MonoModule keeps its MIDI track (notes play the tracks).

## Build and test

- `cd src && go test -race ./...` (the main package needs cgo; with no libasound headers use `make smoke`'s approach). `go vet ./...` too.
- `make` builds the cgo binary in Docker (golang:1.25-bookworm + libasound2-dev). `go mod tidy` runs there to create go.sum (not committed).
- `make smoke` (`scripts/smoke.sh`) builds a stand-in libasound (`scripts/fakealsa`) that writes PCM to a file and runs `TestWebAPI` and `TestAudioPath`: the real cgo bridge and render loop, plus the HTTP API.
- `go run ./cmd/mmrender -o demo.wav`, `go run ./cmd/mmscreens -o docs/screens`.
- `scripts/deploy.sh` copies the binary to a Push (service `push-hack-monomodule`, log `/data/push-hack/logs/monomodule.log`).

## Release

Bump `hack.json` "version", tag `v<version>`, push the tag. The workflow checks they match, tests, builds, packages `monomodule.tar.gz` (top directory `monomodule/`) and commits `release.json`. Add `catalog-entry.json` to the framework's `catalog/catalog.json`.

## Not verified on hardware

The Push/Live/loopback integration has only been compile-checked and smoke-tested off device. The bus client (`bus.go`) has a test against a real `core/audiobus` server (`TestBusPath`), and the same client code ran on a Push 3 in MachineModule and Braids, but MonoModule has not been run with the mixer on a device. Check first on a Push: the audio session opens and sounds; Home focus/blur; LED updates; pad and encoder mapping; Remote Script track creation. Also: the binary links dynamically (`--dynamic-linker=/lib/ld-linux-x86-64.so.2`, as braids verified); the framework's PUBLISHING.md recommends static linking, so confirm which the target needs. The name "Monomodule" is also used by another project; check the catalog for clashes before publishing.
