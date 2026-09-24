GO      ?= go
BINARY  := litefactory
DIST    := dist
PKGS    := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# `check` runs its steps in order and stops at the first failure; running them
# concurrently would interleave their output for no gain.
.NOTPARALLEL:

.PHONY: all build test test-fast check check-fast fmt vet lint clean

all: build

## build: compile a static, cgo-free binary into dist/
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) ./cmd/litefactory

## test: run the test suite with the race detector
# No package deadline: the real git/SQLite race tests keep growing, and any
# fixed limit (Go's default is ten minutes) eventually fails a healthy run.
# Override for a bounded run with `make test TEST_TIMEOUT=30m`.
TEST_TIMEOUT ?= 0
test:
	$(GO) test -race -timeout=$(TEST_TIMEOUT) $(PKGS)

## test-fast: run the test suite without the race detector
# The same tests in a fraction of the time: race instrumentation of the
# transpiled SQLite dominates `test`. Use it to iterate; it does not replace
# `check` as the gate.
test-fast:
	$(GO) test -timeout=$(TEST_TIMEOUT) $(PKGS)

## check: everything CI enforces — formatting, vet, lint, race tests
check: fmt vet lint test

## check-fast: check without the race detector, for iterating before `check`
check-fast: fmt vet lint test-fast

## fmt: fail if any file is not gofmt-clean
fmt:
	@unformatted="$$(gofmt -l . )"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: these files are not formatted:"; \
		echo "$$unformatted"; \
		echo "run: gofmt -w ."; \
		exit 1; \
	fi

## vet: run go vet
vet:
	$(GO) vet $(PKGS)

## lint: run golangci-lint at a pinned version
#
# The version is pinned here and run via `go run` rather than requiring a system
# install, so every machine and CI runner lints with the same rules — an
# unpinned linter turns a green build red on someone else's laptop. This is a
# build-time tool only: it is kept out of the module graph in tools/go.mod, so
# it contributes nothing to the shipped binary's dependencies. A locally
# installed golangci-lint is preferred when present, purely because it is much
# faster; both paths run the same pinned version.
# .golangci.yml is a v2-format config, so the linter must be v2 — note the /v2
# in the module path. Bumping this version without checking the config format
# still matches is the one way this target breaks confusingly.
GOLANGCI_VERSION ?= v2.13.1
GOLANGCI        := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

lint:
	@if command -v golangci-lint >/dev/null 2>&1 && \
	   golangci-lint --version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_VERSION))"; then \
		echo "golangci-lint $(GOLANGCI_VERSION) (installed)"; \
		golangci-lint run; \
	else \
		echo "golangci-lint $(GOLANGCI_VERSION) (go run; slower — 'brew install golangci-lint' to speed this up)"; \
		$(GO) run $(GOLANGCI) run; \
	fi

## clean: remove build output
clean:
	rm -rf $(DIST)
