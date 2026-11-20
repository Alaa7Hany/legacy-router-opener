# Makefile for the Legacy Router Opener desktop module.
#
# Usage:
#   make                      # build for the current host (Linux by default)
#   make build VERSION=1.0.0  # build with a version stamp
#   make test                 # run the Go test suite
#   make run                  # run without auto-opening a browser
#   make clean                # remove build artifacts

VERSION ?= 0.0.0-dev
GO ?= go
GOTOOLCHAIN ?= auto

BINARY := legacy-router-opener
ifeq ($(OS),Windows_NT)
BINARY := legacy-router-opener.exe
endif

LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build build-linux build-windows test run clean

all: build

build: build-linux

build-linux:
	GOTOOLCHAIN=$(GOTOOLCHAIN) GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .

build-windows:
	GOTOOLCHAIN=$(GOTOOLCHAIN) GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w -H=windowsgui -X main.version=$(VERSION)" -o legacy-router-opener.exe .

test:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test ./...

run:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) run . --no-browser

clean:
	rm -f legacy-router-opener legacy-router-opener.exe
