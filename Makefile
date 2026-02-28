.PHONY: build test generate clean web:build web:dev install lint

BINARY := aviary
MODULE := github.com/lsegal/aviary
CMD_DIR := ./cmd/aviary

build: web:copy
	go build -o $(BINARY) $(CMD_DIR)

install: web:copy
	go install $(CMD_DIR)

test:
	go test ./...

lint:
	golangci-lint run ./...

generate:
	go generate ./...

clean:
	rm -f $(BINARY)
	rm -rf web/dist internal/server/webdist

web:build:
	cd web && npm install && npm run build
	$(MAKE) web:copy

web:copy:
	@mkdir -p internal/server/webdist
	@cp -r web/dist/. internal/server/webdist/

web:dev:
	cd web && npm run dev

.DEFAULT_GOAL := build
