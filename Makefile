.DEFAULT_GOAL := help
BINARY := glove

# Single source of truth for the version (no external tooling required).
VERSION_CURRENT := $(shell cat VERSION 2>/dev/null || echo 0.0.0)
# Module path of the version package; matches go.mod's module.
VERSION_PKG ?= github.com/teochenglim/glove/internal/version
# `var Version = "x.y.z"`; created with the v0.1.0 code (no trailing spaces: Make keeps them in the value)
VERSION_FILE := internal/version/version.go
IMAGE ?= ghcr.io/teochenglim/glove

.PHONY: help
help: ## Show this menu
	@echo "glove $(VERSION_CURRENT) - available targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Release cycle:"
	@echo "  make release VERSION=x.y.z   # bump VERSION, push HEAD, tag, push tag -> CI"

## --- develop -----------------------------------------------------------

.PHONY: build
build: ## Build the static binary into ./bin (CGO_ENABLED=0, no libc surface; no JS toolchain, no UI)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X $(VERSION_PKG).Version=v$(VERSION_CURRENT)" -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: run
run: ## Run the CLI locally (hook/check/install land with v0.1.0, serve with v0.4.0 — PRD §6/§7)
	go run ./cmd/$(BINARY)

.PHONY: test
test: ## Run the Go test suite (offline; zero external services on the decision path)
	go test ./...

.PHONY: stress
stress: ## Load- and timing-heavy tests (GLOVE_STRESS=1): §8 budgets, soak, 10k query, cross-compile. Run before calling a release ready — skipped by make check/CI (shared runners turn wall-clock budgets into flakes). Packages run serially (-p 1) so benches never compete with parallel builds
	GLOVE_STRESS=1 go test -count=1 -p 1 ./...

.PHONY: race
race: ## Run the test suite under the race detector
	go test -race ./...

.PHONY: ac-map
ac-map: ## Traceability gate: every PRD §7 acceptance bullet -> AC -> >=2 tests
	./scripts/ac-map.sh

.PHONY: fmt
fmt: ## Format Go source
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Check formatting
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }

.PHONY: vet
vet: ## go vet (warnings are errors)
	go vet ./...

.PHONY: check
check: fmt-check vet ac-map test version-check ## Everything CI runs

.PHONY: clean
clean: ## Remove local build artifacts (bin/, dist/, test cache)
	rm -rf bin dist
	go clean -testcache

## --- container -----------------------------------------------------------

.PHONY: docker-build
docker-build: ## Build the container image (tags :$(VERSION_CURRENT) and :latest)
	docker build --build-arg VERSION=v$(VERSION_CURRENT) -t $(IMAGE):$(VERSION_CURRENT) -t $(IMAGE):latest .

.PHONY: docker-push
docker-push: ## Manual push to GHCR (CI does this on tags). Needs `docker login ghcr.io` first.
	docker push $(IMAGE):$(VERSION_CURRENT)
	docker push $(IMAGE):latest

## --- supply-chain hardening ----------------------------------------------

.PHONY: github-action-bump
github-action-bump: ## Pin .github/workflows/*.yml actions to the newest release of the same major, as full commit SHAs
	python3 scripts/pin-actions.py
	@echo "Actions bumped and verified. Review the diff, then run 'make check' before committing."

.PHONY: audit
audit: ## govulncheck (install: go install golang.org/x/vuln/cmd/govulncheck@latest)
	govulncheck ./...

## --- release --------------------------------------------------------------

.PHONY: version
version: ## Print the version currently in VERSION
	@echo $(VERSION_CURRENT)

.PHONY: version-check
version-check: ## VERSION must equal the version constant (internal/version)
	@test -f $(VERSION_FILE) || { echo "$(VERSION_FILE) missing - it is created with the v0.1.0 code"; exit 1; }
	@test "$$(grep -E 'Version[[:space:]]*=' $(VERSION_FILE) | head -1 | sed 's/.*"\(.*\)".*/\1/')" = "$(VERSION_CURRENT)" \
	  || { echo "VERSION ($(VERSION_CURRENT)) != $(VERSION_FILE)"; exit 1; }

.PHONY: bump
bump: ## Rewrite VERSION and the version constant with the new version (VERSION=x.y.z required)
	@if [ -z "$(VERSION)" ]; then echo "Usage: make bump VERSION=x.y.z"; exit 1; fi
	@echo "$(VERSION)" > VERSION
	@if [ -f "$(VERSION_FILE)" ]; then \
	  perl -0pi -e 's/var Version = ".*?"/var Version = "$(VERSION)"/' $(VERSION_FILE); \
	else \
	  echo "note: $(VERSION_FILE) not created yet; only VERSION updated"; \
	fi
	@echo "VERSION -> $(VERSION)"

.PHONY: release
release: ## Bump VERSION, push HEAD, tag, push the tag - triggers GitHub Actions (VERSION=x.y.z required)
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=x.y.z"; exit 1; fi
	$(MAKE) bump VERSION=$(VERSION)
	git add VERSION
	@if [ -f "$(VERSION_FILE)" ]; then git add $(VERSION_FILE); fi
	git commit --amend --no-edit
	git push origin HEAD
	git tag v$(VERSION)
	git push origin v$(VERSION)
	@echo "Released v$(VERSION) - GitHub Actions will build and publish."
