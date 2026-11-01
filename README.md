# MonoModule

A six-voice synthesizer for **Ableton Push 3** in the style of the Elektron Monomachine SFX-60 MKII, as a module for [Push Hack](https://github.com/federico-pepe/ableton-push-hack).

It follows the Monomachine's *model*: six tracks, 22 machines, a cascaded high-pass/low-pass filter with an envelope, three LFOs per voice, per-track EQ, sample-rate reduction and delay, a pitched step sequencer with parameter locks, slide, swing, a POLY mode, an arpeggiator, kits, patterns and songs. The sound engine is original; it is **not** a reproduction of Elektron's DSP and does not sound identical to the hardware.

**Not affiliated with, endorsed by or derived from Elektron.** "Monomachine" is Elektron's name; it is used here only to say what this imitates.

## Status: alpha, not verified on hardware

Everything that can run without a Push has been tested here (engine, sequencer, panel, project files, MIDI map, web API, the cgo audio path against a stand-in for libasound). **The integration with a real Push 3, Ableton Live and the loopback card has not been run**: Home focus/blur, LED updates, pad and encoder mapping, the audio session on `hw:PHVAudio,1,0` and track creation through the Remote Script are written from the patterns of other Push Hack modules and compile-checked only. See [CLAUDE.md](CLAUDE.md) for what to check first on a device.

## What you get

- **Six tracks, 22 machines**: GND-EMPTY/SIN/NOIS, SWAVE-SAW/PULSE/ENS, SID-6581, VO-6, FM+STAT/PAR/DYN, DPRO-WAVE/BBOX/DDRW/DENS, and the FX machines THRU, REVERB, CHORUS, DYNAMIX, RINGMOD, PHASER, FLANGER (which process lower-numbered tracks).
- **56 parameters per track** on seven pages of eight (SYN, AMP, FLT, FX, LFO1-3), all lockable per step.
- **Sequencer**: patterns A01-H16, 2-64 steps, scales 1/2x-1x-2x, swing 50-80%, slide, chords, note-off steps, trigless trigs, real-time and step recording.
- **POLY**: all six voices on one track.
- **Arpeggiator** per track (up, down, up/down, down/up, random; NORM, LATCH, SID play modes).
- **128 kits, 24 songs**, 32 user waveforms (WAV import).
- **Push front panel** on a green LCD look-alike, and a **web page** with the same functions plus a piano keyboard.
- MIDI in: channel n plays track n, CCs 16-71 map a track's 56 parameters, clock/start/stop sync.

## Requirements

[push-audio-loopback](https://github.com/federico-pepe/ableton-push-hack) (the virtual audio card), push-manager and push-display, and the PushHack Remote Script in Live (to create the tracks).

## Install and build

Add `catalog-entry.json` to the framework's catalog, tag a release (see [CLAUDE.md](CLAUDE.md)), or build locally:

```
make          # builds ./monomodule for the Push (Docker, cgo, linux/amd64)
make test     # go vet + go test -race
make smoke    # off-device test of the audio path and web API
scripts/deploy.sh <push-host>
```

`go.sum` is not committed; `make` runs `go mod tidy` in the container.

## Use

Open it from Push Hack Home (Shift + Help). Then see [MANUAL.md](MANUAL.md). In short: pads rows 0-1 are steps, row 2 selects tracks, rows 3-4 select patterns, rows 5-7 play notes; the eight encoders edit the page's eight parameters; hold a step and turn an encoder to lock that parameter on the step.

## What is not implemented

The Monomachine has features this module leaves out: the MIDI sequencer tracks, multi-trig/multi-envelope modes, audio inputs, the three stereo output buses, FX machines gated by trigs, SysEx dump/receive, the +Drive/RAM memory and the joystick. Machine names and parameter names are this project's own (the manual's appendix could not be read), and several behaviours are approximations. Details are in the MANUAL's "Differences" section.

## Docs

`docs/screens` has the Push screens (rendered by `cmd/mmscreens` from the same code the device runs) and the web page; `docs/demo.wav` is a 32 s demo rendered by `cmd/mmrender` without any hardware.

## License

MIT, see LICENSE.
