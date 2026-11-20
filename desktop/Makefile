# Makefile for the Legacy Router Opener desktop module.
#
# Thin wrapper over the canonical build scripts so there is one source of
# truth per platform:
#   - build.sh        builds Linux binaries (validates the version, strips "v")
#   - build.ps1       builds the Windows .exe (adds go-winres icon/version resources)
#   - build-macos.sh  builds macOS binaries (arm64 by default, or amd64)
#
# Usage:
#   make                              # build for Linux via build.sh (amd64 by default)
#   make GOARCH=arm64                 # build for linux/arm64
#   make build VERSION=1.0.0          # build for Linux with a version stamp
#   make build-windows VERSION=1.0.0  # build the Windows .exe via build.ps1 (requires pwsh)
#   make build-macos VERSION=1.0.0    # build for macOS/arm64 via build-macos.sh
#   make build-macos VERSION=1.0.0 MACOS_ARCH=amd64  # build for macOS/amd64
#   make build-macos-all VERSION=1.0.0 # build both macOS architectures
#   make test-macos                   # run the macOS feature test suite (TDD seams)
#   make test                         # run the Go test suite
#   make run                          # run without auto-opening a browser
#   make clean                        # remove build artifacts

VERSION ?= 0.0.0-dev
GO ?= go
GOTOOLCHAIN ?= auto
GOARCH ?= amd64
MACOS_ARCH ?= arm64

.PHONY: all build build-linux build-windows build-macos build-macos-arm64 build-macos-amd64 build-macos-all test-macos test run clean

all: build

build: build-linux

build-linux:
	GOARCH=$(GOARCH) GOTOOLCHAIN=$(GOTOOLCHAIN) ./build.sh '$(VERSION)'

build-windows:
	GOTOOLCHAIN=$(GOTOOLCHAIN) pwsh -NoProfile -ExecutionPolicy Bypass -File ./build.ps1 -Version '$(VERSION)'

build-macos:
	GOTOOLCHAIN=$(GOTOOLCHAIN) ./build-macos.sh '$(VERSION)' '$(MACOS_ARCH)'

build-macos-arm64:
	GOTOOLCHAIN=$(GOTOOLCHAIN) ./build-macos.sh '$(VERSION)' arm64

build-macos-amd64:
	GOTOOLCHAIN=$(GOTOOLCHAIN) ./build-macos.sh '$(VERSION)' amd64

build-macos-all: build-macos-arm64 build-macos-amd64

test-macos:
	GOTOOLCHAIN=$(GOTOOLCHAIN) bash ../tests/macos-feature.test.sh

test:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test ./...

run:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) run . --no-browser

clean:
	rm -f legacy-router-opener legacy-router-opener.exe legacy-router-opener-macos-arm64 legacy-router-opener-macos-amd64
