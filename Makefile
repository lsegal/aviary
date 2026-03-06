.PHONY: build test generate clean web-build web-dev install lint

BINARY := aviary
MODULE := github.com/lsegal/aviary
CMD_DIR := ./cmd/aviary
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X $(MODULE)/internal/server.Version=$(VERSION)"

build: web-copy
	go build $(LDFLAGS) -o $(BINARY) $(CMD_DIR)

install: web-copy
	go install $(LDFLAGS) $(CMD_DIR)

test:
	go test ./...

lint:
	golangci-lint run ./...

generate:
	go generate ./...

clean:
	rm -f $(BINARY)
	rm -rf web/dist internal/server/webdist

web-build:
	pnpm install && pnpm build
	$(MAKE) web-copy

web-copy:
	@mkdir -p internal/server/webdist
	@cp -r web/dist/. internal/server/webdist/

web-dev:
	pnpm dev

.DEFAULT_GOAL := build
