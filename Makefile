# vmrc Makefile — build the two binaries (vmrc CLI + vmrc-agent) from one module.
#
# Version stamps mirror .goreleaser.yaml's ldflags so a local `make build` reports a
# meaningful version. They are derived from git: a plain checkout off a tag yields
# something like "v0.1.0-3-gabc1234-dirty", which the self-update gate
# (internal/selfupdate: isReleaseVersion) treats as a non-release, so local builds
# won't self-update without --force. Override any of these on the command line, e.g.
#   make build VERSION=v0.2.0

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GO      ?= go
BIN_DIR ?= bin

# Keep GOLANGCI_VERSION in sync with .github/workflows/ci.yml.
GOLANGCI_VERSION ?= v2.12.2
GOTOOLCHAIN      ?= go1.26.3

LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
BUILD   := $(GO) build -trimpath -ldflags '$(LDFLAGS)'

.DEFAULT_GOAL := help

.PHONY: help build vmrc vmrc-agent test vet lint check snapshot snapshot-host dev-install clean

help: ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: vmrc vmrc-agent ## Build both binaries into $(BIN_DIR)/

vmrc: ## Build the host CLI into $(BIN_DIR)/vmrc
	$(BUILD) -o $(BIN_DIR)/vmrc ./cmd/vmrc

vmrc-agent: ## Build the VM agent into $(BIN_DIR)/vmrc-agent
	$(BUILD) -o $(BIN_DIR)/vmrc-agent ./cmd/vmrc-agent

test: ## Run the test suite
	$(GO) test ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint (pinned, via go run — mirrors CI)
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

check: vet test lint ## Run vet, test, and lint (the CI gate)

snapshot: ## Cross-build all platforms with GoReleaser (-> dist/)
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser not found — see https://goreleaser.com"; exit 1; }
	goreleaser build --snapshot --clean

snapshot-host: ## GoReleaser build for just this host (-> dist/)
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser not found — see https://goreleaser.com"; exit 1; }
	goreleaser build --snapshot --single-target --clean

dev-install: ## Build the CLI and symlink it onto your PATH (live rebuilds)
	./scripts/dev-install.sh

clean: ## Remove build output
	rm -rf $(BIN_DIR) dist
	rm -f ./vmrc ./vmrc.exe ./vmrc-agent ./vmrc-agent.exe
