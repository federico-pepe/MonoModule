# Changelog

## Unreleased

- Release workflow (`.github/workflows/release.yml`): a `v*` tag tests, builds (Docker, cgo, linux/amd64), packages `monomodule.tar.gz` and publishes a GitHub Release, then updates `release.json`. Every push and pull request is also tested and built, and the package is kept as a workflow artifact.
## 0.1.0-alpha

First version. Engine, sequencer, Push panel, web page, MIDI map and Push Hack integration. Tested off device only; not verified on a Push 3, Live or the loopback card.
