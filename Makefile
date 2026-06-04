# Personal Context — Development Makefile
# Run `make` or `make help` to see all available targets.

.DEFAULT_GOAL := help
SHELL := /bin/bash

# ---------------------------------------------------------------------------
# Directories
# ---------------------------------------------------------------------------
ROOT    := $(shell pwd)
CLI_DIR := $(ROOT)/cli
WEB_DIR := $(ROOT)/web
WEB_CLOUD_ENV_CHECK := { [ -n "$$DATABASE_URL" ] && [ -n "$$S3_BUCKET" ]; } || { [ -f $(WEB_DIR)/.env.local ] && grep -Eq '^DATABASE_URL=.+' $(WEB_DIR)/.env.local 2>/dev/null && grep -Eq '^S3_BUCKET=.+' $(WEB_DIR)/.env.local 2>/dev/null; }
PC_VERSION ?= dev
DIST_DIR ?= dist

# ---------------------------------------------------------------------------
# Help (auto-generated from ## comments)
# ---------------------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@printf '\n\033[1mUsage:\033[0m make \033[36m<target>\033[0m\n\n'
	@printf '\033[1mCommon workflows:\033[0m\n'
	@printf '  \033[36mmake check\033[0m      Run everything needed before committing\n'
	@printf '  \033[36mmake test\033[0m       Run all tests (CLI + Web)\n'
	@printf '  \033[36mmake dev\033[0m        Start the web UI (auto-detect local vs cloud)\n'
	@printf '\n\033[1mAll targets:\033[0m\n'
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo

# ---------------------------------------------------------------------------
# Top-level targets
# ---------------------------------------------------------------------------

.PHONY: install
install: ## Install all dependencies (pnpm + Go modules)
	@echo "→ Installing web dependencies…"
	@cd $(WEB_DIR) && pnpm install
	@echo "→ Tidying Go modules…"
	@cd $(CLI_DIR) && go mod tidy
	@echo "✓ All dependencies installed"

.PHONY: check
check: schema lint typecheck cli-coverage web-coverage build ## Pre-commit gate: schema + lint + typecheck + coverage + build
	@echo "✓ All checks passed"

.PHONY: test
test: cli-test web-test ## Run all tests (CLI unit + Web unit)

.PHONY: lint
lint: cli-lint web-lint ## Lint everything (Go + ESLint)

.PHONY: build
build: cli-build web-build ## Build everything (Go binary + Next.js)

.PHONY: dev
dev: ## Start web UI (auto-detects local vs cloud mode)
	@if $(WEB_CLOUD_ENV_CHECK); then \
		echo "→ Cloud credentials found, starting Next.js directly…"; \
		cd $(WEB_DIR) && pnpm dev; \
	else \
		echo "→ No cloud credentials, starting local mode (pc serve + Next.js)…"; \
		$(MAKE) dev-local; \
	fi

.PHONY: seed
seed: cli-build ## Seed tutorial records (idempotent)
	@$(CLI_DIR)/pc seed

.PHONY: dev-local
dev-local: cli-build ## Start local dev mode (pc serve + proxied Next.js)
	@$(CLI_DIR)/pc seed
	@echo "→ Starting pc serve on port 9876…"
	@set -eu; \
	serve_pid=""; \
	trap 'if [ -n "$$serve_pid" ]; then kill "$$serve_pid" 2>/dev/null || true; wait "$$serve_pid" 2>/dev/null || true; fi' EXIT INT TERM; \
	$(CLI_DIR)/pc serve --port 9876 & \
	serve_pid="$$!"; \
	sleep 1; \
	if ! kill -0 "$$serve_pid" 2>/dev/null; then \
		echo "pc serve failed to start" >&2; \
		exit 1; \
	fi; \
	echo "→ Starting Next.js with LOCAL_BACKEND_URL…"; \
	cd $(WEB_DIR) && LOCAL_BACKEND_URL=http://127.0.0.1:9876 pnpm dev

.PHONY: dev-cloud
dev-cloud: ## Start cloud dev mode (Next.js with Neon/S3 — requires env vars)
	@if $(WEB_CLOUD_ENV_CHECK); then \
		cd $(WEB_DIR) && pnpm dev; \
	else \
		echo "DATABASE_URL and S3_BUCKET must be set (environment or web/.env.local) before running make dev-cloud" >&2; \
		exit 1; \
	fi

.PHONY: serve
serve: cli-build ## Start pc serve only (Go API server)
	@$(CLI_DIR)/pc serve

.PHONY: clean
clean: ## Remove build artifacts
	@rm -f $(CLI_DIR)/pc
	@rm -rf $(WEB_DIR)/.next
	@echo "✓ Clean"

.PHONY: test-release
test-release: ## Run release artifact tests
	@./scripts/test-release.sh

.PHONY: release-changelog-check
release-changelog-check: ## Validate CHANGELOG.md has release notes (set RELEASE_TAG=vX.Y.Z)
	@if [[ -z "$${RELEASE_TAG:-}" ]]; then \
		echo "RELEASE_TAG is required (example: make release-changelog-check RELEASE_TAG=v0.1.0)" >&2; \
		exit 1; \
	fi
	@if ! [[ "$${RELEASE_TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$$ ]]; then \
		echo "RELEASE_TAG must be a stable tag in vX.Y.Z format (got: $${RELEASE_TAG})" >&2; \
		exit 1; \
	fi
	@TAG_RE=$$(printf '%s\n' "$${RELEASE_TAG}" | sed 's/\./\\./g'); \
	if ! grep -Eq "^## $${TAG_RE} - [0-9]{4}-[0-9]{2}-[0-9]{2}$$" CHANGELOG.md; then \
		echo "CHANGELOG.md missing release section: ## $${RELEASE_TAG} - YYYY-MM-DD" >&2; \
		exit 1; \
	fi

.PHONY: release-preflight
release-preflight: test-release release-changelog-check ## Validate release readiness (set RELEASE_TAG=vX.Y.Z)

.PHONY: release-dist
release-dist: test-release ## Build release artifacts (cross-compile)
	@PC_VERSION="$(PC_VERSION)" DIST_DIR="$(DIST_DIR)" ./scripts/build-release.sh

# ---------------------------------------------------------------------------
# Schema
# ---------------------------------------------------------------------------

.PHONY: schema
schema: ## Verify schema contract and Postgres/SQLite equivalence
	@echo "→ Schema contract…"
	@./scripts/check_schema_contract.sh
	@echo "→ Schema equivalence…"
	@./scripts/check_schema_equivalence.sh

# ---------------------------------------------------------------------------
# CLI (Go)
# ---------------------------------------------------------------------------

.PHONY: cli-test
cli-test: ## Run Go unit tests
	@cd $(CLI_DIR) && go test ./...

.PHONY: cli-build
cli-build: ## Build the pc binary
	@cd $(CLI_DIR) && go build -o pc ./cmd/pc

.PHONY: cli-lint
cli-lint: ## Run golangci-lint
	@cd $(CLI_DIR) && golangci-lint run ./...

.PHONY: cli-coverage
cli-coverage: ## Enforce Go coverage ≥95% (aggregate + per-package)
	@cd $(CLI_DIR) && ./scripts/check_coverage.sh 95 coverage.out
	@cd $(CLI_DIR) && ./scripts/check_coverage_per_package.sh 95 coverage.out

.PHONY: cli-e2e
cli-e2e: cli-build ## Run CLI e2e tests (builds binary first)
	@cd $(CLI_DIR) && go test ./internal/e2e

.PHONY: cli-integration
cli-integration: ## Run Docker integration tests (Postgres + S3) — requires Docker
	@cd $(CLI_DIR) && go test -tags integration ./internal/repository/postgres/ -v -timeout 180s
	@cd $(CLI_DIR) && go test -tags integration ./internal/s3client/ -v -timeout 60s
	@cd $(CLI_DIR) && go test -tags integration ./internal/cloude2e/ -v -timeout 420s

.PHONY: cli-all
cli-all: cli-lint cli-coverage cli-e2e cli-integration ## Run all CLI checks (lint + coverage + e2e + integration)

# ---------------------------------------------------------------------------
# Web (Next.js)
# ---------------------------------------------------------------------------

.PHONY: typecheck
typecheck: ## Run TypeScript type checking
	@cd $(WEB_DIR) && pnpm typecheck

.PHONY: web-test
web-test: ## Run web unit tests (vitest)
	@cd $(WEB_DIR) && pnpm test

.PHONY: web-build
web-build: ## Build Next.js for production
	@cd $(WEB_DIR) && pnpm build

.PHONY: web-lint
web-lint: ## Run ESLint on web workspace
	@cd $(WEB_DIR) && pnpm lint

.PHONY: web-coverage
web-coverage: ## Run web tests with coverage (≥95% gate)
	@cd $(WEB_DIR) && pnpm test:coverage

.PHONY: web-e2e
web-e2e: ## Run all Playwright e2e tests
	@cd $(WEB_DIR) && pnpm test:e2e

.PHONY: web-e2e-smoke
web-e2e-smoke: ## Run Playwright smoke test only
	@cd $(WEB_DIR) && pnpm test:e2e:smoke

.PHONY: web-e2e-browser
web-e2e-browser: ## Run Record Browser Playwright e2e tests
	@cd $(WEB_DIR) && pnpm test:e2e:record-browser

.PHONY: web-e2e-visual
web-e2e-visual: ## Run visual regression tests (compare against baselines)
	@cd $(WEB_DIR) && pnpm test:e2e:visual

.PHONY: web-e2e-visual-update
web-e2e-visual-update: ## Regenerate visual regression baselines
	@cd $(WEB_DIR) && pnpm test:e2e:visual -- --update-snapshots

.PHONY: web-all
web-all: web-lint typecheck web-coverage web-build web-e2e ## Run all web checks (lint + typecheck + coverage + build + e2e)

.PHONY: web-install-playwright
web-install-playwright: ## Install Playwright browsers
	@cd $(WEB_DIR) && pnpm exec playwright install chromium
