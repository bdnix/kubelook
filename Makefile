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

.PHONY: docs-check version test-release-scripts chart-test load it-up it-down it e2e tools fmt lint lint-backend lint-frontend test test-backend test-frontend test-ui coverage coverage-backend coverage-frontend build build-frontend build-backend image verify clean

tools:
	hack/install-tools.sh

fmt:
	golangci-lint fmt
	cd web && npx prettier --write .

lint-backend:
	golangci-lint run

lint-frontend:
	cd web && npm run lint && npm run format:check && npm run typecheck

docs-check:
	hack/docs-check.sh

lint: lint-backend lint-frontend docs-check
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

# Integration environment (Phase 7): two kind clusters, Vault, PostgreSQL, an identity stand-in and
# Kubelook containers. Needs Docker and a cgroup v2 host.
it-up: tools image
	hack/it-up.sh

it-down:
	hack/it-down.sh

it:
	go test -tags integration -count=1 -p 1 -timeout 30m ./test/integration/...
	@echo "INTEGRATION OK"

e2e:
	cd test/e2e && npm ci && npx playwright test
	@echo "E2E OK"

# The version of the next publication (the VERSION file, validated); tests of the release scripts.
version:
	@hack/version.sh

test-release-scripts:
	hack/test-release-scripts.sh

# Lint, render and assert the Helm chart (Step 8.2); needs no cluster.
chart-test:
	hack/chart-test.sh

clean:
	rm -rf $(BIN)/kubelook coverage.out web/dist web/coverage
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete

# Watch-hub load test (Step 6.4): measures memory per watch, event latency and time to the first snapshot with real time,
# so it is never part of `verify` or the ci workflow. Parameters: LOAD_PODS, LOAD_SESSIONS, LOAD_EVENTS_PER_SECOND,
# LOAD_SECONDS; LOAD_DOCS names a file the result table is appended to (hack/load.sh sets it).
load:
	go test -tags load -count=1 -timeout 15m -run TestLoad ./test/load/... -v
