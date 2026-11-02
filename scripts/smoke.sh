#!/usr/bin/env bash
# Off-device smoke test: builds a stand-in for libasound that writes the PCM
# stream to a file, then runs the main package's tests against it (the real
# cgo bridge, render loop and web API; no Push, Live or loopback card needed).
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/include/alsa" "$tmp/lib"
cp "$here/scripts/fakealsa/asoundlib.h" "$tmp/include/alsa/"
gcc -shared -fPIC -I"$tmp/include" -o "$tmp/lib/libasound.so" "$here/scripts/fakealsa/fake.c"
cd "$here/src"
FAKE_PCM_OUT="$tmp/out.pcm" CGO_CFLAGS="-I$tmp/include" CGO_LDFLAGS="-L$tmp/lib" \
  LD_LIBRARY_PATH="$tmp/lib" go test -count=1 -v -run 'TestWebAPI|TestAudioPath|TestMonoOutputSwitchesLive' .
