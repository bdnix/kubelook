SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := verify

BIN := $(CURDIR)/bin
export PATH := $(BIN):$(PATH)
PKG ?= ./...
FILE ?=
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/bdnix/kubelook/internal/version.Version=$(VERSION) -X github.com/bdnix/kubelook/internal/version.Commit=$(COMMIT)
COVER_TOOL := github.com/vladopajic/go-test-coverage/v2@v2.23.0

.PHONY: tools fmt lint lint-go lint-web test test-go test-web cover cover-go cover-web build build-web build-go image verify clean

tools:
	hack/install-tools.sh

fmt:
	golangci-lint fmt
	cd web && npx prettier --write .

lint-go:
	golangci-lint run

lint-web:
	cd web && npm run lint && npm run format:check && npm run typecheck

lint: lint-go lint-web
	@echo "LINT OK"

test-go:
	go test -race -count=1 -shuffle=on $(PKG)

test-web:
	cd web && npx vitest run $(FILE)

test: test-go test-web

cover-go:
	go test -race -count=1 -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...
	go run $(COVER_TOOL) --config=.testcoverage.yml

cover-web:
	cd web && npx vitest run --coverage

cover: cover-go cover-web
	@echo "COVERAGE OK"

build-web:
	cd web && npm run build
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
	cp -R web/dist/. internal/webui/dist/

build-go:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kubelook ./cmd/kubelook

build: build-web build-go
	@echo "BUILD OK"

image:
	@test -f Dockerfile || { echo "no Dockerfile yet (added in Step 0.6)" >&2; exit 1; }
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t kubelook:$(VERSION) .

verify: lint cover build
	@echo "VERIFY OK"

clean:
	rm -rf $(BIN)/kubelook coverage.out web/dist web/coverage
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
