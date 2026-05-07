.PHONY: help install lint format test coverage fuzz check clean clean-all build build-all deps generate bump-patch push security bench toolchain-info test-repro test-repro-fast test-race staticcheck

GO ?= $(shell command -v go 2>/dev/null || echo /workspace/.cache/go-install/go/bin/go)
GOFMT ?= gofumpt
GOLINT ?= golangci-lint
GOSEC ?= gosec
GO_TMPDIR ?= /workspace/tmp
GOTOOLCHAIN ?= auto
STATICCHECK_VERSION ?= v0.7.0

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# =============================================================================
# Full reproducible build
# =============================================================================

build-all: clean deps lint test build ## Full reproducible build (clean + deps + lint + test + build)
	@echo "Build complete!"

# =============================================================================
# Go targets
# =============================================================================

deps: ## Download and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

install: ## Install the library
	$(GO) install ./...

lint: ## Run golangci-lint
	@which $(GOLINT) > /dev/null || (echo "Installing golangci-lint..." && brew install golangci-lint)
	$(GOLINT) run ./...

security: ## Run gosec security scan
	@which $(GOSEC) > /dev/null || (echo "Installing gosec..." && $(GO) install github.com/securego/gosec/v2/cmd/gosec@latest)
	$$(go env GOPATH)/bin/$(GOSEC) ./...

format: ## Format code with gofumpt
	@which $(GOFMT) > /dev/null || (echo "Installing gofumpt..." && $(GO) install mvdan.cc/gofumpt@latest)
	$(GOFMT) -w .

test: ## Run tests
	TMPDIR=$(GO_TMPDIR) $(GO) test ./... -count=1

coverage: ## Run tests with coverage
	TMPDIR=$(GO_TMPDIR) $(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

bench: ## Run benchmarks
	TMPDIR=$(GO_TMPDIR) $(GO) test -run '^$$' -bench . ./...

fuzz: ## Run fuzz tests (30s each by default, override with FUZZTIME=60s)
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzPartialJSON -fuzztime $(or $(FUZZTIME),30s) ./internal/jsonparse/
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzSSEParse -fuzztime $(or $(FUZZTIME),30s) ./internal/eventstream/
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzContextRoundTrip -fuzztime $(or $(FUZZTIME),30s) .
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzTransformMessages -fuzztime $(or $(FUZZTIME),30s) .
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzOverflowDetection -fuzztime $(or $(FUZZTIME),30s) .

check: lint test check-logging ## Run lint + tests + logging gate

# =============================================================================
# Reproducible verification targets
# =============================================================================

toolchain-info: ## Print toolchain and environment used by reproducible targets
	@echo "GO=$(GO)"
	@echo "GO_TMPDIR=$(GO_TMPDIR)"
	@echo "GOTOOLCHAIN=$(GOTOOLCHAIN)"
	@$(GO) version
	@$(GO) env GOVERSION GOOS GOARCH CGO_ENABLED

staticcheck: ## Run staticcheck at a pinned version
	GOTOOLCHAIN=$(GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) $(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

test-race: ## Run race tests (requires gcc/clang + CGO)
	CGO_ENABLED=1 TMPDIR=$(GO_TMPDIR) $(GO) test -race ./... -count=1

test-repro-fast: ## Reproducible local gate (no race)
	TMPDIR=$(GO_TMPDIR) $(GO) test ./... -count=1
	TMPDIR=$(GO_TMPDIR) $(GO) vet ./...
	TMPDIR=$(GO_TMPDIR) $(GO) build ./...
	$(MAKE) staticcheck
	$(MAKE) check-logging

test-repro: ## Full reproducible gate (includes race detector)
	$(MAKE) toolchain-info
	$(MAKE) test-repro-fast
	$(MAKE) test-race

check-logging: ## Verify logging quality gate
	./scripts/check-logging.sh

build: ## Build the library (verify compilation)
	$(GO) build ./...

# =============================================================================
# Code generation
# =============================================================================

generate: ## Regenerate models_generated.go from pi-coding-agent (with legacy fallback support)
	$(GO) run scripts/generate-models.go

# =============================================================================
# Clean targets
# =============================================================================

clean: ## Remove build artifacts and cache
	$(GO) clean
	rm -rf coverage.out

clean-all: clean ## Remove everything including vendor
	rm -rf vendor

# =============================================================================
# Version management
# =============================================================================

bump-patch: ## Bump patch version and create git tag
	@CURRENT=$$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"); \
	MAJOR=$$(echo $$CURRENT | sed 's/v//' | cut -d. -f1); \
	MINOR=$$(echo $$CURRENT | sed 's/v//' | cut -d. -f2); \
	PATCH=$$(echo $$CURRENT | sed 's/v//' | cut -d. -f3); \
	NEW="v$$MAJOR.$$MINOR.$$((PATCH + 1))"; \
	git tag "$$NEW"; \
	echo "Created tag: $$NEW"

push: ## Push commits and current tag to origin
	@TAG=$$(git describe --tags --exact-match 2>/dev/null); \
	git push origin main; \
	if [ -n "$$TAG" ]; then \
		echo "Pushing tag $$TAG..."; \
		git push origin "$$TAG"; \
	else \
		echo "No tag on current commit"; \
	fi
