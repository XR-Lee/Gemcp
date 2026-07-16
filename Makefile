GO ?= go
NPM ?= npm
VERSION := $(shell tr -d '\n' < VERSION)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILT_AT := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)

.PHONY: fmt test vet vuln frontend-install frontend-test frontend-build stage-frontend build check

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

test:
	$(GO) test ./...

frontend-install:
	cd frontend && $(NPM) ci --include=dev

frontend-test:
	cd frontend && $(NPM) run test:run

frontend-build:
	cd frontend && $(NPM) run build

stage-frontend: frontend-build
	./scripts/stage-frontend.sh

build: stage-frontend
	mkdir -p bin
	$(GO) build -tags webembed -trimpath -ldflags "$(LDFLAGS)" -o bin/gemcp ./cmd/gemcp

check: fmt vet test vuln frontend-test frontend-build
