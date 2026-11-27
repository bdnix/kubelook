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

.PHONY: docs-check ci-select-test chart-test it-tools it-up it-down it e2e tools fmt lint lint-backend lint-frontend test test-backend test-frontend test-ui coverage coverage-backend coverage-frontend build build-frontend build-backend image verify clean

# The developer tools: golangci-lint and govulncheck, built from Go source. `make verify` needs nothing else installed.
tools:
	hack/install-tools.sh

# kind, kubectl and Helm: only for the integration environment and the chart tests, never for everyday development.
it-tools:
	hack/install-it-tools.sh

fmt:
	golangci-lint fmt
	cd web && npx prettier --write .

lint-backend:
	golangci-lint run

lint-frontend:
	cd web && npm run lint && npm run format:check && npm run typecheck

docs-check:
	hack/docs-check.sh

ci-select-test:
	hack/ci-select-postgres-test.sh

lint: lint-backend lint-frontend docs-check ci-select-test
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

# Integration environment: two kind clusters under test, a host cluster running Kubelook from its Helm chart, Vault,
# PostgreSQL and an identity stand-in. Needs Docker and a cgroup v2 host. KUBELOOK_IMAGE_ARCHIVE names an image
# archive to test instead of building kubelook:dev.
it-up: it-tools
	hack/it-up.sh

it-down:
	hack/it-down.sh

it:
	go test -tags integration -count=1 -p 1 -timeout 30m -v ./test/integration/...
	@echo "INTEGRATION OK"

e2e:
	cd test/e2e && npm ci && npx playwright test
	@echo "E2E OK"

# Lint, render and assert the Helm chart (Step 8.2); needs no cluster.
chart-test: it-tools
	hack/chart-test.sh

clean:
	rm -rf $(BIN)/kubelook coverage.out web/dist web/coverage
	mkdir -p internal/webui/dist
	find internal/webui/dist -mindepth 1 ! -name placeholder.txt -delete
