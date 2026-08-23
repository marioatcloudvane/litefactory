GO      ?= go
BINARY  := litefactory
DIST    := dist
PKGS    := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# `check` runs its steps in order and stops at the first failure; running them
# concurrently would interleave their output for no gain.
.NOTPARALLEL:

.PHONY: all build test check fmt vet lint clean

all: build

## build: compile a static, cgo-free binary into dist/
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) ./cmd/litefactory

## test: run the test suite with the race detector
test:
	$(GO) test -race $(PKGS)

## check: everything CI enforces — formatting, vet, lint, race tests
check: fmt vet lint test

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

## lint: run golangci-lint; a missing linter is a failure, not a skip
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed, so 'make check' cannot verify this repo."; \
		echo "install it: brew install golangci-lint"; \
		echo "(to run the remaining checks only: make fmt vet test)"; \
		exit 1; \
	}
	golangci-lint run

## clean: remove build output
clean:
	rm -rf $(DIST)
