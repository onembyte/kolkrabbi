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

.PHONY: arch purity buildtags budgets
arch: ## layering, dependency and naming rules
	./scripts/check-arch.sh
purity: ## the engine touches no OS
	./scripts/check-purity.sh
buildtags: ## every OS-divergent file declares its constraint
	./scripts/check-buildtags.sh
budgets: ## binary size, cold start, test floor, dependency count
	./scripts/check-budgets.sh

.PHONY: check
check: fmt-check vet test arch purity buildtags budgets ## everything CI runs

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
