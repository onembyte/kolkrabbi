# kolkrabbi
#
# Every target that walks the repo enumerates ALL go.mod directories. A bare
# `go test ./...` does not see a nested module: it prints ok and exits 0 while
# that module's tests never run. Use these targets, or scripts/test.sh.

GO      ?= go
BIN     ?= kolk
DIST    ?= dist
MODULE  := github.com/onembyte/kolkrabbi
# Every go.mod in the repo. A bare ./... does not see the ones below the root.
GOMODS  := $(shell find . -name go.mod -not -path './.git/*' -not -path '*/node_modules/*' -exec dirname {} \;)
LDFLAGS := -s -w \
  -X $(MODULE)/internal/buildinfo.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
  -X $(MODULE)/internal/buildinfo.commit=$(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown) \
  -X $(MODULE)/internal/buildinfo.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.DEFAULT_GOAL := check

.PHONY: help
help: ## show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## build ./cmd/kolk into ./kolk
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/kolk

.PHONY: install
install: ## install kolk into $$GOBIN
	CGO_ENABLED=0 $(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/kolk

.PHONY: test
test: ## run the tests of every module in the repo
	./scripts/test.sh

.PHONY: vet
vet: ## go vet, every module
	@for d in $(GOMODS); do echo "── $$d ──"; (cd "$$d" && $(GO) vet ./...) || exit 1; done

.PHONY: fmt
fmt: ## gofmt the tree
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## fail if anything needs gofmt
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "these files need gofmt:"; echo "$$unformatted"; exit 1; fi

.PHONY: arch purity buildtags platforms budgets site surface installer release-check release-workflow-check release-verifier-check
arch: ## layering, dependency and naming rules
	./scripts/check-arch.sh
purity: ## the engine touches no OS
	./scripts/check-purity.sh
buildtags: ## every OS-divergent file declares its constraint
	./scripts/check-buildtags.sh
platforms: ## compile the root module for every CLI release target
	./scripts/check-platforms.sh
budgets: ## binary size, cold start, test floor, dependency count
	./scripts/check-budgets.sh
site: ## static landing-page content, safety and deployment contract
	./scripts/test-site.sh
surface: ## v0.1 exposes chat and code only, with code as the default
	./scripts/test-v01-surface.sh
installer: ## offline installer platform, integrity, extraction and replacement matrix
	./scripts/test-installer.sh
release-check: ## static archive, checksum and signing contract
	./scripts/test-release.sh
release-workflow-check: ## immutable tag-only release workflow and SemVer guard
	./scripts/test-release-workflow.sh
release-verifier-check: ## signed public assets, exact manifest, archives and host identity
	./scripts/test-release-verifier.sh

.PHONY: lint
lint: ## golangci-lint, if it is installed
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run ./...; \
	elif [ -x "$$(go env GOPATH)/bin/golangci-lint" ]; then \
	  "$$(go env GOPATH)/bin/golangci-lint" run ./...; \
	else \
	  echo "golangci-lint not installed — skipping (CI runs it)"; \
	  echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
	fi

.PHONY: check
check: fmt-check vet test arch purity buildtags platforms lint budgets site surface installer release-check release-workflow-check release-verifier-check ## everything CI runs

.PHONY: workspace
workspace: ## write a gitignored go.work so gopls sees every module
	./scripts/dev-workspace.sh

.PHONY: mock
mock: ## run the scripted OpenRouter mock (no network, no key, no cost)
	$(GO) run ./cmd/kolk-mock

.PHONY: clean
clean: ## remove build output
	rm -f $(BIN) kolkd kolk-mock
	rm -rf $(DIST)
