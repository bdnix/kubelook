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

.PHONY: tools fmt lint lint-backend lint-frontend test test-backend test-frontend test-ui coverage coverage-backend coverage-frontend build build-frontend build-backend image verify clean

tools:
	hack/install-tools.sh

fmt:
	golangci-lint fmt
	cd web && npx prettier --write .

lint-backend:
	golangci-lint run

lint-frontend:
	cd web && npm run lint && npm run format:check && npm run typecheck

lint: lint-backend lint-frontend
	@echo "LINT OK"

test-backend:
	go test -race -count=1 -shuffle=on $(PKG)

test-frontend:
	cd web && npx vitest run $(FILE)

test: test-backend test-frontend

# Browser UI tests (Playwright, Chromium): the built web app with the API answered by the tests.
# Needs a browser: `cd web && npx playwright install --with-deps chromium`, or set
# PLAYWRIGHT_CHROMIUM_EXECUTABLE to an existing Chromium. Not part of `verify` (it needs a browser).
test-ui:
	cd web && npx playwright test

coverage-backend:
	go test -race -count=1 -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...
	go run $(COVER_TOOL) --config=.testcoverage.yml

coverage-frontend:
	cd web && npx vitest run --coverage

coverage: coverage-backend coverage-frontend
	@echo "COVERAGE OK"

build-frontend:
	cd web && npm run build
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
	cp -R web/dist/. internal/webui/dist/

build-backend:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kubelook ./cmd/kubelook

build: build-frontend build-backend
	@echo "BUILD OK"

image:
	@test -f Dockerfile || { echo "no Dockerfile yet (added in Step 0.6)" >&2; exit 1; }
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t kubelook:$(VERSION) .

verify: lint coverage build
	@echo "VERIFY OK"

clean:
	rm -rf $(BIN)/kubelook coverage.out web/dist web/coverage
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
