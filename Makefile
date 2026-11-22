GO ?= go
PREFIX ?= $(HOME)/.local

.PHONY: build install test check terminal-test
build:
	$(GO) build -buildvcs=false -trimpath -o bin/bt ./cmd/bt

install: build
	install -Dm755 bin/bt $(DESTDIR)$(PREFIX)/bin/bt

test:
	$(GO) test -race ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...

terminal-test: build
	BT_TEST_BINARY=$(CURDIR)/bin/bt $(GO) test -count=1 -run TestTerminalRestoration -v ./internal/tui
