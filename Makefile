SHELL := /bin/sh
BIN := bin/send-to-me

-include .env
export

.PHONY: all deps frontend build run test test-web vet fmt clean

all: build

deps: web/node_modules

web/node_modules: web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml
	cd web && pnpm install --frozen-lockfile

frontend: web/node_modules
	cd web && pnpm run build

build:
	$(MAKE) frontend
	mkdir -p bin
	go build -trimpath -ldflags="-s -w" -o $(BIN) .

run:
	$(MAKE) build
	./$(BIN)

test:
	go test ./...

test-web:
	cd web && pnpm run typecheck

vet:
	go vet ./...

fmt:
	gofmt -w main.go internal

clean:
	rm -rf bin
