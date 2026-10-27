# SharedCal developer tasks. Run `make help` for a list.

SHELL      := bash
.SHELLFLAGS := -eu -o pipefail -c
GO         ?= go
# Go packages, excluding any Go sources shipped inside web/node_modules.
PKGS            = $(shell $(GO) list ./... | grep -v /node_modules/)
NPM        ?= npm
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE      ?= ghcr.io/felixtrav/sharedcal
PLATFORMS  ?= linux/amd64,linux/arm64
LDFLAGS    := -s -w -X main.version=$(VERSION)

STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0

# `make dev` settings. The mock OIDC provider issues tokens for test users;
# the Vite dev server (5173) proxies /api, /auth, /ics to the Go server (8080).
DEV_DATA       ?= $(CURDIR)/data/dev
MOCKOIDC_URL   ?= http://localhost:9999
DEV_CLIENT_ID  ?= sharedcal
DEV_SECRET     ?= sharedcal-dev-secret

.PHONY: help dev build web go test test-all-dbs lint vuln image clean web-placeholder

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n",$$1,$$2}'

web/node_modules: web/package-lock.json
	cd web && $(NPM) ci
	@touch $@

# web/embed.go embeds web/dist, so Go builds need at least a placeholder.
web-placeholder:
	@mkdir -p web/dist
	@[ -f web/dist/index.html ] || echo '<!doctype html><title>SharedCal</title>' > web/dist/index.html

dev: web/node_modules web-placeholder ## Run mock OIDC + Go server + Vite with hot reload
	@mkdir -p $(DEV_DATA)
	@trap 'kill 0' EXIT INT TERM; \
	$(GO) run ./cmd/mockoidc & \
	APP_URL=http://localhost:5173 DATA_DIR=$(DEV_DATA) LOG_FORMAT=text LOG_LEVEL=debug \
	OIDC_ISSUER=$(MOCKOIDC_URL) OIDC_CLIENT_ID=$(DEV_CLIENT_ID) OIDC_CLIENT_SECRET=$(DEV_SECRET) \
	OIDC_ADMIN_GROUPS=calendar-admins OIDC_WRITER_GROUPS=calendar-writers OIDC_VIEWER_GROUPS=calendar-viewers \
	$(GO) run ./cmd/sharedcal serve & \
	(cd web && $(NPM) run dev -- --host 0.0.0.0) & \
	wait

build: web go ## Build the web app, then the Go binary into bin/sharedcal

web: web/node_modules
	cd web && $(NPM) run build

go:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/sharedcal ./cmd/sharedcal

test: web-placeholder ## Go tests on SQLite (plus web unit tests if node_modules exist)
	$(GO) test -race -count=1 $(PKGS)
	@if [ -d web/node_modules ]; then cd web && $(NPM) test; fi

test-all-dbs: web-placeholder ## Go tests on SQLite, PostgreSQL and MySQL (needs Docker for testcontainers)
	SHAREDCAL_TEST_DB=sqlite   $(GO) test -count=1 $(PKGS)
	SHAREDCAL_TEST_DB=postgres $(GO) test -count=1 $(PKGS)
	SHAREDCAL_TEST_DB=mysql    $(GO) test -count=1 $(PKGS)

lint: web-placeholder ## gofmt, go vet, staticcheck, eslint and tsc
	@out=$$(gofmt -l $$(git ls-files '*.go' 2>/dev/null || find . -name '*.go' -not -path './web/*')); \
	  if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet $(PKGS)
	$(GO) run $(STATICCHECK) $(PKGS)
	@if [ -d web/node_modules ]; then cd web && $(NPM) run lint && $(NPM) run typecheck; fi

vuln: web-placeholder ## govulncheck and npm audit
	$(GO) run $(GOVULNCHECK) $(PKGS)
	cd web && $(NPM) audit --audit-level=moderate

image: ## Build the multi-arch container image (docker buildx; add PUSH=1 to push)
	docker buildx build -f deploy/Dockerfile --platform $(PLATFORMS) \
	  --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) \
	  $(if $(PUSH),--push,) .

clean: ## Remove build output
	rm -rf bin web/dist
