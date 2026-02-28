.PHONY: build test generate clean web:build web:dev install lint

BINARY := aviary
MODULE := github.com/lsegal/aviary
CMD_DIR := ./cmd/aviary

build:
	go build -o $(BINARY) $(CMD_DIR)

install:
	go install $(CMD_DIR)

test:
	go test ./...

lint:
	golangci-lint run ./...

generate:
	go generate ./...

clean:
	rm -f $(BINARY)
	rm -rf web/dist

web:build:
	cd web && npm install && npm run build

web:dev:
	cd web && npm run dev

.DEFAULT_GOAL := build
