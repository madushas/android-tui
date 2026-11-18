# android-tui — Android emulators & adb connections (Go + Bubble Tea)
NAME    := android-tui
MODULE  := github.com/madushas/android-tui
PREFIX  ?= $(HOME)/.local
BIN     := bin/$(NAME)
# git tag / describe when available, otherwise "dev" (release builds get the tag from GoReleaser)
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --always --dirty 2>/dev/null || echo dev))
LDFLAGS := -s -w -X $(MODULE)/internal/core.Version=$(VERSION)

.PHONY: build install test vet fmt fmt-check lint vendor demo completions clean dist smoke race ci privacy-check size-check qr-check hooks

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

install: build
	install -Dm755 $(BIN) $(PREFIX)/bin/$(NAME)
	install -Dm644 completions/_android-tui $(PREFIX)/share/zsh/site-functions/_android-tui

test:
	go vet ./... && go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w main.go internal

# Fails (and lists the files) when something is not gofmt-clean — used by CI.
fmt-check:
	@out=$$(gofmt -l main.go internal); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Needs golangci-lint v2 (https://golangci-lint.run/welcome/install/).
lint:
	golangci-lint run ./...

# Unit tests under the race detector (needs cgo/gcc).
race:
	CGO_ENABLED=1 go test -race -count=1 ./...

# Re-resolve dependencies from the network and refresh vendor/ (needs access to the Go module proxy).
vendor:
	go mod tidy && go mod vendor

# Run against a fake adb/emulator/android CLI — no phone or SDK needed.
demo: build
	ATUI=$(CURDIR)/$(BIN) sh scripts/demo.sh

# pty smoke test of the TUI against the fake phone (needs python3 + pyte: pip install pyte).
smoke: build
	sh scripts/dev/smoke.sh

# Static binaries + checksums for the platforms the code supports (Linux and macOS; it uses Unix-only syscalls).
dist:
	mkdir -p dist
	for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do \
	  GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(NAME)-$${t%/*}-$${t#*/} . || exit 1; \
	done
	cd dist && (sha256sum $(NAME)-* 2>/dev/null || shasum -a 256 $(NAME)-*) > SHA256SUMS

# Fails when private, device-specific or working-note material is in what git would commit (see scripts/dev/privacy_check.py).
# Run it before every commit and before exporting a zip.
privacy-check:
	python3 scripts/dev/privacy_check.py --self-test
	python3 scripts/dev/privacy_check.py

# Fails when a Go file (600 lines) or function (100 lines) outgrows its limit — split by responsibility instead.
size-check:
	python3 scripts/dev/size_check.py --self-test
	python3 scripts/dev/size_check.py

# Optional: run the mechanical checks (privacy, size, gofmt, vet) before every commit.
hooks:
	git config core.hooksPath .githooks

# Validates the QR symbol the pairing screen draws, against the ISO 18004 rules (independent decoder + self-test).
qr-check:
	QR_DUMP=$(CURDIR)/.qr-symbol.txt go test -count=1 -run TestDumpSyntheticPairingSymbol ./internal/core/
	python3 scripts/dev/qr_check.py .qr-symbol.txt
	python3 scripts/dev/qr_check.py --self-test .qr-symbol.txt
	rm -f .qr-symbol.txt

# Everything CI runs, in the same order (lint is separate because it needs golangci-lint installed).
ci: privacy-check size-check qr-check fmt-check vet test race smoke

clean:
	rm -rf bin dist
