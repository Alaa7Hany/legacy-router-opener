# Makefile for the Legacy Router Opener desktop module.
#
# Thin wrapper over the canonical build scripts so there is one source of
# truth per platform:
#   - build.sh  builds Linux binaries (validates the version, strips "v")
#   - build.ps1 builds the Windows .exe (adds go-winres icon/version resources)
#
# Usage:
#   make                              # build for Linux via build.sh (amd64 by default)
#   make GOARCH=arm64                 # build for linux/arm64
#   make build VERSION=1.0.0          # build for Linux with a version stamp
#   make build-windows VERSION=1.0.0  # build the Windows .exe via build.ps1 (requires pwsh)
#   make test                         # run the Go test suite
#   make run                          # run without auto-opening a browser
#   make clean                        # remove build artifacts

VERSION ?= 0.0.0-dev
GO ?= go
GOTOOLCHAIN ?= auto
GOARCH ?= amd64

.PHONY: all build build-linux build-windows test run clean

all: build

build: build-linux

build-linux:
	GOARCH=$(GOARCH) GOTOOLCHAIN=$(GOTOOLCHAIN) ./build.sh '$(VERSION)'

build-windows:
	GOTOOLCHAIN=$(GOTOOLCHAIN) pwsh -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 -Version '$(VERSION)'

test:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test ./...

run:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) run . --no-browser

clean:
	rm -f legacy-router-opener legacy-router-opener.exe
