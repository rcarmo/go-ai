.PHONY: help install lint format test test-deterministic vet coverage fuzz check clean clean-all build build-all deps generate check-model-regeneration check-model-regeneration-self-test check-v0850-inventory check-v0850-catalog-delta check-v0851-inventory check-v0851-catalog-delta bump-patch push security vuln-check vuln-self-test license-check sbom sbom-check sbom-self-test publisher-self-test ci-artifacts bench toolchain-info test-repro test-repro-fast test-race test-shuffle staticcheck

GO ?= $(shell command -v go 2>/dev/null || echo /workspace/.cache/go-install/go/bin/go)
GOFMT ?= gofumpt
GOLINT ?= golangci-lint
GOSEC ?= gosec
PROJECT := go-ai
# Resolve once BEFORE replacing TMPDIR. Portable helpers are vendored here.
# PROJECT_TMP_BASE selects <base>/go-ai; compatible explicit ROOT must agree.
# CI: RUNNER_TEMP, inherited TMPDIR, /tmp. Local: /workspace/tmp, /tmp.
PROJECT_ORIGINAL_TMPDIR ?= $(TMPDIR)
PROJECT_ORIGINAL_TMPDIR := $(PROJECT_ORIGINAL_TMPDIR)
export PROJECT_ORIGINAL_TMPDIR
ifeq ($(origin PROJECT_TMP_ROOT),undefined)
PROJECT_TMP_RESOLVED := $(shell $(if $(filter undefined,$(origin PROJECT_TMP_BASE)),,PROJECT_TMP_BASE='$(subst ','"'"',$(PROJECT_TMP_BASE))') PROJECT_ORIGINAL_TMPDIR='$(subst ','"'"',$(PROJECT_ORIGINAL_TMPDIR))' PROJECT=$(PROJECT) bash scripts/project-tmp.sh root)
else
PROJECT_TMP_RESOLVED := $(shell $(if $(filter undefined,$(origin PROJECT_TMP_BASE)),,PROJECT_TMP_BASE='$(subst ','"'"',$(PROJECT_TMP_BASE))') PROJECT_TMP_ROOT='$(subst ','"'"',$(PROJECT_TMP_ROOT))' PROJECT_ORIGINAL_TMPDIR='$(subst ','"'"',$(PROJECT_ORIGINAL_TMPDIR))' PROJECT=$(PROJECT) bash scripts/project-tmp.sh root)
endif
override PROJECT_TMP_ROOT := $(PROJECT_TMP_RESOLVED)
ifeq ($(strip $(PROJECT_TMP_ROOT)),)
$(error Cannot resolve a safe project temporary root)
endif
GO_TMPDIR ?= $(PROJECT_TMP_ROOT)/build/tmp
GOCACHE ?= $(PROJECT_TMP_ROOT)/cache/go-build
GOMODCACHE ?= $(PROJECT_TMP_ROOT)/cache/go-mod
GOPATH ?= $(PROJECT_TMP_ROOT)/cache/go-path
XDG_CACHE_HOME ?= $(PROJECT_TMP_ROOT)/cache/xdg
npm_config_cache ?= $(PROJECT_TMP_ROOT)/cache/npm
BUN_INSTALL_CACHE_DIR ?= $(PROJECT_TMP_ROOT)/cache/bun
PYTHONPYCACHEPREFIX ?= $(PROJECT_TMP_ROOT)/cache/python
GO_AI_MODEL_REGEN_CACHE ?= $(PROJECT_TMP_ROOT)/cache/model-regeneration
PROFILE_ROOT ?= $(PROJECT_TMP_ROOT)/evidence/profiles
TMPDIR := $(GO_TMPDIR)
TMP := $(GO_TMPDIR)
TEMP := $(GO_TMPDIR)
GOTMPDIR := $(GO_TMPDIR)
export PROJECT_TMP_ROOT GOCACHE GOMODCACHE GOPATH XDG_CACHE_HOME npm_config_cache BUN_INSTALL_CACHE_DIR PYTHONPYCACHEPREFIX GO_AI_MODEL_REGEN_CACHE PROFILE_ROOT TMPDIR TMP TEMP GOTMPDIR
PACKAGES ?= ./...
TEST_FLAGS ?=
PROFILE_TEST = GO=$(GO) PROFILE_ROOT=$(PROFILE_ROOT) bash scripts/test-profile.sh

.PHONY: project-tmp-init project-paths project-tmp-self-test
project-tmp-init: ## Validate/init project caches and scratch without deleting evidence
	@bash -c 'source scripts/project-env.sh'

project-tmp-self-test: project-tmp-init ## Profile portable routing and unsafe-root/isolation tests
	GO=$(GO) bash scripts/test-python-profile.sh scripts/test-project-temp.py

project-paths: ## Show canonical cache/build/run/evidence paths
	@printf '%s\n' 'PROJECT_TMP_ROOT=$(PROJECT_TMP_ROOT)' 'GOCACHE=$(GOCACHE)' 'GOMODCACHE=$(GOMODCACHE)' 'GO_TMPDIR=$(GO_TMPDIR)' 'PROFILE_ROOT=$(PROFILE_ROOT)'

# Enforce routing for all cache/temp-producing entrypoints, including recursive Make.
deps install lint format test test-deterministic test-shuffle test-race coverage bench fuzz vet staticcheck build generate check-model-regeneration check-model-regeneration-self-test vuln-check vuln-self-test license-check sbom sbom-self-test publisher-self-test toolchain-info check-v0850-inventory check-v0850-catalog-delta check-v0851-inventory check-v0851-catalog-delta check-v0870-inventory check-v0870-catalog-delta check-v0871-inventory check-v0871-catalog-delta: | project-tmp-init
GOTOOLCHAIN ?= auto
STATICCHECK_VERSION ?= v0.8.1
CYCLONEDX_GOMOD_VERSION ?= v1.12.0
GOVULNCHECK_VERSION ?= v1.7.0
GOVULNCHECK_GOTOOLCHAIN ?= go1.27.1
GO_LICENSES_VERSION ?= v1.6.0
SBOM_DIR ?= artifacts
SBOM_FILE ?= $(SBOM_DIR)/sbom.cdx.json
SBOM_SHA_FILE ?= $(SBOM_FILE).sha256
SBOM_REVISION ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
SBOM_VCS_REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
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

vuln-check: ## Run govulncheck at a pinned version/toolchain and enforce security-vuln-policy.json
	GOTOOLCHAIN=$(GOVULNCHECK_GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) python3 scripts/check-vuln-policy.py security-vuln-policy.json $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) -json ./...

vuln-self-test: ## Run negative vulnerability policy self-tests
	GO=$(GO) bash scripts/test-python-profile.sh scripts/test-check-vuln-policy.py

license-check: ## Review dependency licenses; unknown/forbidden fail unless documented and explicitly allowed
	GOTOOLCHAIN=$(GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) $(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check --include_tests --allowed_licenses=$(ALLOWED_LICENSES) ./...

sbom: ## Generate normalized CycloneDX JSON SBOM and SHA-256 checksum under artifacts/
	@mkdir -p $(SBOM_DIR)
	GOTOOLCHAIN=$(GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) $(GO) run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION) mod -json -licenses -assert-licenses -output-version 1.6 -type library -output $(SBOM_FILE) .
	python3 scripts/normalize-sbom.py $(SBOM_FILE) $(SBOM_REVISION) --vcs-revision $(SBOM_VCS_REVISION)
	@sha256sum $(SBOM_FILE) > $(SBOM_SHA_FILE)

sbom-check: sbom ## Validate SBOM schema/required fields/checksum/dependency output
	python3 scripts/validate-sbom.py $(SBOM_FILE) $(SBOM_SHA_FILE) --expected-revision $(SBOM_REVISION) --expected-vcs-revision $(SBOM_VCS_REVISION)

sbom-self-test: ## Run negative SBOM normalizer and validator self-tests
	GO=$(GO) bash scripts/test-python-profile.sh scripts/test-normalize-sbom.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/test-validate-sbom.py

publisher-self-test: ## Run native publisher tag policy simulations
	GO=$(GO) bash scripts/test-python-profile.sh scripts/test-verify-native-release-tag.py

ci-artifacts: sbom-check sbom-self-test publisher-self-test vuln-check vuln-self-test license-check ## Generate and validate release CI security artifacts

format: ## Format code with gofumpt
	@which $(GOFMT) > /dev/null || (echo "Installing gofumpt..." && $(GO) install mvdan.cc/gofumpt@latest)
	$(GOFMT) -w .

test: ## Run tests with CPU/heap profiles and allocation analysis
	$(PROFILE_TEST) $(PACKAGES) -- $(TEST_FLAGS)

test-deterministic: ## Profile tests three times to catch nondeterminism
	$(PROFILE_TEST) $(PACKAGES) -- -count=3 $(TEST_FLAGS)

test-shuffle: ## Profile tests with shuffled order
	$(PROFILE_TEST) $(PACKAGES) -- -shuffle=on $(TEST_FLAGS)

vet: ## Run go vet
	TMPDIR=$(GO_TMPDIR) $(GO) vet ./...

coverage: ## Run tests with coverage
	COVERAGE_FILE=coverage.out $(PROFILE_TEST) $(PACKAGES) -- -cover $(TEST_FLAGS)
	$(GO) tool cover -func=coverage.out

bench: ## Run benchmarks
	$(PROFILE_TEST) $(PACKAGES) -- -run '^$$' -bench . -benchmem $(TEST_FLAGS)

fuzz: ## Run profiled fuzz targets (default 30s each; focus with FUZZ_TARGET/PACKAGES)
	@if test -n "$(FUZZ_TARGET)"; then \
	  $(PROFILE_TEST) $(PACKAGES) -- -run '^$$' -fuzz '$(FUZZ_TARGET)' -fuzztime $(or $(FUZZTIME),30s) $(TEST_FLAGS); \
	else \
	  $(PROFILE_TEST) ./internal/jsonparse -- -run '^$$' -fuzz FuzzPartialJSON -fuzztime $(or $(FUZZTIME),30s) $(TEST_FLAGS) && \
	  $(PROFILE_TEST) ./transports/sse -- -run '^$$' -fuzz FuzzSSEParse -fuzztime $(or $(FUZZTIME),30s) $(TEST_FLAGS) && \
	  $(PROFILE_TEST) ./tests -- -run '^$$' -fuzz FuzzContextRoundTrip -fuzztime $(or $(FUZZTIME),30s) $(TEST_FLAGS) && \
	  $(PROFILE_TEST) ./tests -- -run '^$$' -fuzz FuzzTransformMessages -fuzztime $(or $(FUZZTIME),30s) $(TEST_FLAGS) && \
	  $(PROFILE_TEST) ./tests -- -run '^$$' -fuzz FuzzOverflowDetection -fuzztime $(or $(FUZZTIME),30s) $(TEST_FLAGS); \
	fi

check: project-tmp-self-test test-deterministic vet staticcheck check-logging check-v0850-inventory check-v0850-catalog-delta check-v0851-inventory check-v0851-catalog-delta check-v0870-inventory check-v0870-catalog-delta check-v0871-inventory check-v0871-catalog-delta check-model-regeneration check-model-regeneration-self-test sbom-check sbom-self-test publisher-self-test vuln-check vuln-self-test license-check ## Run deterministic tests + vet + staticcheck + logging + model/SBOM/publisher/security gates

check-v0850-inventory: ## Validate committed v0.85.0 release inventories and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0850-inventory.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0850-inventory.py --self-test
	python3 scripts/validate-test-manifest.py docs/v0850-142-test-manifest.md

check-v0850-catalog-delta: ## Validate exact full-record v0.84.4->v0.85.0 catalog deltas and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0850-catalog-delta.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0850-catalog-delta.py --self-test

check-model-regeneration-self-test: ## Prove text/image generation comparators fail on non-ID metadata drift
	GO=$(GO) bash scripts/test-python-profile.sh scripts/test-check-model-regeneration.py

check-v0851-inventory: ## Validate committed v0.85.1 release inventories and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0851-inventory.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0851-inventory.py --self-test
	python3 scripts/validate-test-manifest.py docs/v0851-142-test-manifest.md docs/v0851/test-corpus-142.txt

check-v0851-catalog-delta: ## Validate exact full-record v0.85.0->v0.85.1 catalog deltas and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0851-catalog-delta.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0851-catalog-delta.py --self-test

check-v0870-inventory: ## Validate committed v0.87.0 release inventories and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0870-inventory.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0870-inventory.py --self-test
	python3 scripts/validate-test-manifest.py docs/v0870-150-test-manifest.md docs/v0870/test-corpus-150.txt

check-v0870-catalog-delta: ## Validate exact full-record v0.85.1->v0.87.0 catalog deltas and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0870-catalog-delta.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0870-catalog-delta.py --self-test

check-v0871-inventory: ## Validate committed v0.87.1 release inventories and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0871-inventory.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0871-inventory.py --self-test
	python3 scripts/validate-test-manifest.py docs/v0871-150-test-manifest.md docs/v0871/test-corpus-150.txt

check-v0871-catalog-delta: ## Validate exact full-record v0.87.0->v0.87.1 catalog deltas and negative self-test
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0871-catalog-delta.py
	GO=$(GO) bash scripts/test-python-profile.sh scripts/validate-v0871-catalog-delta.py --self-test

# =============================================================================
# Reproducible verification targets
# =============================================================================

toolchain-info: ## Print toolchain and environment used by reproducible targets
	@echo "GO=$(GO)"
	@echo "GO_TMPDIR=$(GO_TMPDIR)"
	@echo "GOTOOLCHAIN=$(GOTOOLCHAIN)"
	@echo "GOVULNCHECK_GOTOOLCHAIN=$(GOVULNCHECK_GOTOOLCHAIN)"
	@$(GO) version
	@$(GO) env GOVERSION GOOS GOARCH CGO_ENABLED

staticcheck: ## Run staticcheck at a pinned version
	GOTOOLCHAIN=$(GOTOOLCHAIN) TMPDIR=$(GO_TMPDIR) $(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

test-race: ## Run race tests (requires gcc/clang + CGO)
	CGO_ENABLED=1 $(PROFILE_TEST) $(PACKAGES) -- -race $(TEST_FLAGS)

test-repro-fast: project-tmp-self-test ## Reproducible local gate (profiled tests, no race)
	$(MAKE) test
	TMPDIR=$(GO_TMPDIR) $(GO) vet ./...
	TMPDIR=$(GO_TMPDIR) $(GO) build ./...
	$(MAKE) staticcheck
	$(MAKE) check-logging
	$(MAKE) check-model-regeneration
	$(MAKE) sbom-check
	$(MAKE) sbom-self-test
	$(MAKE) publisher-self-test
	$(MAKE) vuln-check
	$(MAKE) vuln-self-test
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

clean: project-tmp-init ## Remove local disposable output; retain profiles/receipts/SBOM evidence
	$(GO) clean
	rm -f coverage.out
	@echo 'Project caches and retained evidence are preserved; no recursive root cleanup.'

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
