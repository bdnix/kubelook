SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := verify

# The web build rewrites internal/webui/dist, which the Go build embeds: never run targets in parallel.
.NOTPARALLEL:

BIN := $(CURDIR)/bin
export PATH := $(BIN):$(PATH)
PKG ?= ./...
FILE ?=
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X github.com/bdnix/kubelook/internal/version.Version=$(VERSION) -X github.com/bdnix/kubelook/internal/version.Commit=$(COMMIT)
COVER_TOOL := github.com/vladopajic/go-test-coverage/v2@v2.23.0

.PHONY: tools fmt lint lint-be lint-fe test test-be test-fe test-ui cover cover-be cover-fe build build-fe build-be image verify clean

tools:
	hack/install-tools.sh

fmt:
	golangci-lint fmt
	cd web && npx prettier --write .

lint-be:
	golangci-lint run

lint-fe:
	cd web && npm run lint && npm run format:check && npm run typecheck

lint: lint-be lint-fe
	@echo "LINT OK"

test-be:
	go test -race -count=1 -shuffle=on $(PKG)

test-fe:
	cd web && npx vitest run $(FILE)

test: test-be test-fe

# Browser UI tests (Playwright, Chromium): the built web app with the API answered by the tests.
# Needs a browser: `cd web && npx playwright install --with-deps chromium`, or set
# PLAYWRIGHT_CHROMIUM_EXECUTABLE to an existing Chromium. Not part of `verify` (it needs a browser).
test-ui:
	cd web && npx playwright test

cover-be:
	go test -race -count=1 -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...
	go run $(COVER_TOOL) --config=.testcoverage.yml

cover-fe:
	cd web && npx vitest run --coverage

cover: cover-be cover-fe
	@echo "COVERAGE OK"

build-fe:
	cd web && npm run build
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
	cp -R web/dist/. internal/webui/dist/

build-be:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kubelook ./cmd/kubelook

build: build-fe build-be
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
