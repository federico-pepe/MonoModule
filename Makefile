BINARY    := monomodule
REPO_ROOT := $(abspath $(CURDIR))
DOCKER    := docker run --rm --platform linux/amd64 \
               -v "$(REPO_ROOT)":/work -w /work/src \
               golang:1.25-bookworm

.PHONY: all build test smoke clean

all: build

# cgo (libasound) means a plain `go build` cannot cross-compile; build inside a
# linux/amd64 container. AbletonOS keeps its dynamic linker at
# /lib/ld-linux-x86-64.so.2, not /lib64/, which the extldflags fix.
build:
	@echo "Building $(BINARY) (cgo, linux/amd64, via Docker)..."
	$(DOCKER) sh -c "apt-get update -qq && apt-get install -qq -y libasound2-dev >/dev/null && \
	  go mod tidy && \
	  CGO_ENABLED=1 go build -ldflags '-s -w -linkmode external -extldflags -Wl,--dynamic-linker=/lib/ld-linux-x86-64.so.2' -o ../$(BINARY) . && \
	  chmod +x ../$(BINARY)"
	@echo "Done: $(BINARY)"

# The engine, sequencer, panel and storage are plain Go and test anywhere
# (no Push, no Docker needed): `cd src && go test -race ./...`
test:
	$(DOCKER) sh -c "apt-get update -qq && apt-get install -qq -y libasound2-dev >/dev/null && \
	  go mod tidy && go vet ./... && go test -race ./..."

clean:
	rm -f $(BINARY)

# Off-device smoke test of the cgo audio path and the web API, against a
# stand-in for libasound (needs gcc and Go; no Push, Live or Docker).
smoke:
	./scripts/smoke.sh
