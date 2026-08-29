.PHONY: help install lint format test test-deterministic vet coverage fuzz check clean clean-all build build-all deps generate check-model-regeneration bump-patch push security vuln-check license-check sbom sbom-check ci-artifacts bench toolchain-info test-repro test-repro-fast test-race staticcheck

GO ?= $(shell command -v go 2>/dev/null || echo /workspace/.cache/go-install/go/bin/go)
GOFMT ?= gofumpt
GOLINT ?= golangci-lint
GOSEC ?= gosec
GO_TMPDIR ?= /workspace/tmp
GOTOOLCHAIN ?= auto
STATICCHECK_VERSION ?= v0.7.0
CYCLONEDX_GOMOD_VERSION ?= v1.12.0
GOVULNCHECK_VERSION ?= v1.7.0
GO_LICENSES_VERSION ?= v1.6.0
SBOM_DIR ?= artifacts
SBOM_FILE ?= $(SBOM_DIR)/sbom.cdx.json
SBOM_SHA_FILE ?= $(SBOM_FILE).sha256
SBOM_REVISION ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
ALLOWED_LICENSES ?= Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT,MPL-2.0

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

security: vuln-check ## Run pinned vulnerability scan

vuln-check: ## Run govulncheck at a pinned version and enforce security-vuln-policy.json
	python3 scripts/check-vuln-policy.py security-vuln-policy.json $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) -json ./...

license-check: ## Review dependency licenses; unknown/forbidden fail unless documented and explicitly allowed
	GOTOOLCHAIN=$(GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) $(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check --include_tests --allowed_licenses=$(ALLOWED_LICENSES) ./...

sbom: ## Generate normalized CycloneDX JSON SBOM and SHA-256 checksum under artifacts/
	@mkdir -p $(SBOM_DIR)
	GOTOOLCHAIN=$(GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) $(GO) run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION) mod -json -licenses -assert-licenses -output-version 1.6 -type library -output $(SBOM_FILE) .
	python3 scripts/normalize-sbom.py $(SBOM_FILE) $(SBOM_REVISION)
	@sha256sum $(SBOM_FILE) > $(SBOM_SHA_FILE)

sbom-check: sbom ## Validate SBOM schema/required fields/checksum/dependency output
	python3 scripts/validate-sbom.py $(SBOM_FILE) $(SBOM_SHA_FILE)

ci-artifacts: sbom-check vuln-check license-check ## Generate and validate release CI security artifacts

format: ## Format code with gofumpt
	@which $(GOFMT) > /dev/null || (echo "Installing gofumpt..." && $(GO) install mvdan.cc/gofumpt@latest)
	$(GOFMT) -w .

test: ## Run tests
	TMPDIR=$(GO_TMPDIR) $(GO) test ./... -count=1

test-deterministic: ## Run tests three times to catch nondeterminism
	TMPDIR=$(GO_TMPDIR) $(GO) test ./... -count=3

vet: ## Run go vet
	TMPDIR=$(GO_TMPDIR) $(GO) vet ./...

coverage: ## Run tests with coverage
	TMPDIR=$(GO_TMPDIR) $(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

bench: ## Run benchmarks
	TMPDIR=$(GO_TMPDIR) $(GO) test -run '^$$' -bench . ./...

fuzz: ## Run fuzz tests (30s each by default, override with FUZZTIME=60s)
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzPartialJSON -fuzztime $(or $(FUZZTIME),30s) ./internal/jsonparse/
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzSSEParse -fuzztime $(or $(FUZZTIME),30s) ./transports/sse/
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzContextRoundTrip -fuzztime $(or $(FUZZTIME),30s) .
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzTransformMessages -fuzztime $(or $(FUZZTIME),30s) .
	TMPDIR=$(GO_TMPDIR) $(GO) test -fuzz FuzzOverflowDetection -fuzztime $(or $(FUZZTIME),30s) .

check: test-deterministic vet staticcheck check-logging check-model-regeneration sbom-check vuln-check license-check ## Run deterministic tests + vet + staticcheck + logging + model/SBOM/security gates

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
	$(MAKE) check-model-regeneration
	$(MAKE) sbom-check
	$(MAKE) vuln-check
	$(MAKE) license-check

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

generate: ## Regenerate models_generated.go from pi-ai (with legacy fallback support)
	$(GO) run scripts/generate-models.go

check-model-regeneration: ## Verify models_generated.go matches exact normalized regeneration
	GO=$(GO) GO_TMPDIR=$(GO_TMPDIR) TMPDIR=$(GO_TMPDIR) ./scripts/check-model-regeneration.sh

# =============================================================================
# Clean targets
# =============================================================================

clean: ## Remove build artifacts and cache
	$(GO) clean
	rm -rf coverage.out $(SBOM_DIR)

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
