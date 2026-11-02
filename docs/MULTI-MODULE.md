# Sharing the virtual audio card

- Give every module its own channel or pair: for example MachineModule stereo 1/2, MonoModule mono 3. Two modules on the same channel sum in Live.
- Module names are unique: audio track `MonoModule`, MIDI track `MonoModule MIDI`, MIDI port `MonoModule MIDI In`.
- Enable each input in Live (Push audio preferences > Devices > Push Hack Virtual Audio); only enabled inputs are offered. In Live a stereo pair is named "3/4", a mono input "3" (not seen on a Push by this project).
- MonoModule opens the PCM with the channel count, rate and period Live negotiated and writes only its own channel(s).

## Untested risk

Every module opens `hw:PHVAudio,1,0`. The Loopback driver pairs playback sub-stream N with capture sub-stream N and Live reads sub-stream 0, so a second module may land on a silent sub-stream or be refused. **This has not been tested with two modules.** Check with both running:

    cat /proc/asound/PHVAudio/pcm1p/sub*/status
    cat /proc/asound/PHVAudio/pcm0c/sub*/hw_params

If only one reaches Live, the likely fix is ALSA dmix on the single stream (all modules open a dmix PCM whose slave is `hw:PHVAudio,1,0,0`, same ipc_key, slave channels/rate/period matching Live's); the `pcm_device` setting already accepts another device string. Record the result here.
