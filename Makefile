.PHONY: help build build-frontend build-backend build-worker-daemon build-fakeagent test cover cover-html lint lint-vendor lint-vendor-selftest lint-mock-default lint-doc-impl-drift smoke vet tidy clean e2e e2e-install

# Default target prints discoverable entry points. Run `make` (no
# args) or `make help` to see what's available.
help:
	@echo "agent-center make targets:"
	@echo ""
	@echo "  build / build-frontend / build-backend"
	@echo "  build-worker-daemon / build-fakeagent"
	@echo ""
	@echo "  test                 — go test ./..."
	@echo "  cover / cover-html   — go test with coverage report"
	@echo "  vet                  — go vet ./..."
	@echo "  tidy                 — go mod tidy"
	@echo "  clean                — remove ./bin and coverage artifacts"
	@echo ""
	@echo "Lint (conventions § 0.4 enforce mechanisms):"
	@echo "  lint                     — vet + lint-vendor + lint-mock-default + lint-doc-impl-drift"
	@echo "  lint-vendor              — #v1 vendor residue grep (ADR-0031)"
	@echo "  lint-vendor-selftest     — positive-fail check for lint-vendor"
	@echo "  lint-mock-default        — § 0.4 #2: NoopSender/NoopKillSender prod-wiring guard"
	@echo "  lint-doc-impl-drift      — § 0.4 #3: ADR claim vs code contradiction checks"
	@echo ""
	@echo "Deployed-binary smoke (conventions § 0.4 #4):"
	@echo "  smoke                — fresh-binary deploy + drive task pipeline → done"
	@echo ""
	@echo "End-to-end (Playwright):"
	@echo "  e2e-install          — one-time pnpm + chromium install"
	@echo "  e2e                  — full e2e suite (includes deployed-pipeline spec)"

# Build pipeline composes a frontend bundle then embeds it into the Go
# binary via go:embed (Phase 11 § 3.4 + F15).
#
#   make build              — frontend + backend (one binary, SPA baked in)
#   make build-frontend     — pnpm install + vite build → internal/webconsole/spa/dist/
#   make build-backend      — go build (consumes whatever dist/ holds)
#
# Dev flow is unaffected: run `pnpm --dir web run dev` and start the
# binary; vite proxies /api to the loopback Go server. The SPA chunk in
# the binary isn't consulted while the developer hits the dev server.

BIN := agent-center
WEB := web

# v2.2 GA build identity. VERSION can be overridden at build time
# (e.g. `VERSION=v2.2.1 make build`); COMMIT is auto-discovered from
# the working tree (falls back to "unknown" outside a checkout).
VERSION ?= v2.2.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

build: build-frontend build-backend build-worker-daemon build-fakeagent

build-frontend:
	cd $(WEB) && pnpm install --frozen-lockfile
	cd $(WEB) && pnpm run build
	# vite's emptyOutDir wipes .gitkeep; restore it so the directory
	# survives `make clean` + remains tracked for fresh clones (go:embed
	# needs the dir to exist before the SPA is ever built).
	echo "Populated by 'make build-frontend' (vite build outDir)." > ./internal/webconsole/spa/dist/.gitkeep

build-backend:
	go build -ldflags "-X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT)" \
	    -o ./bin/$(BIN) ./cmd/agent-center

# v2.2-C worker daemon binary — the missing v2.0 GA consumer of the
# dispatchq queue (conventions § 0.4: worker talks to center via the
# admin endpoint, not by re-opening sqlite).
build-worker-daemon:
	go build -o ./bin/agent-center-worker-daemon ./cmd/worker-daemon

# v2.2-D fakeagent — LLM-free agent stub used by e2e tests. Without
# this in bin/ the Phase D deploy-binary e2e cannot run.
build-fakeagent:
	go build -o ./bin/fakeagent ./cmd/fakeagent

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

cover-html: cover
	go tool cover -html=coverage.out -o coverage.html

vet:
	go vet ./...

# lint-vendor — fail if v1 vendor refs (feishu / lark / dingtalk / wechat
# / vendor_msg_ref / internal/bridge) leak back into the tree. See
# scripts/lint/no-vendor-refs.sh for the whitelist mechanism.
lint-vendor:
	./scripts/lint/no-vendor-refs.sh

# lint-vendor-selftest — positive-fail check for lint-vendor: injects a
# violation in each high-risk file type, asserts the lint flags them,
# then cleans up. Opt-in (not part of `make lint`) because it mutates
# the worktree briefly.
lint-vendor-selftest:
	./scripts/lint/test-no-vendor-refs.sh

# lint-mock-default — conventions § 0.4 enforce mechanism #2: catch
# mock-as-default literals (NoopSender / NoopKillSender) on production
# wiring paths without an explicit `// FIXME(prod-wiring):` annotation.
# v2.0 GA shipped with these silently wired into the real server boot;
# dispatch events were dropped and no one noticed until hand-deploy.
lint-mock-default:
	./scripts/lint/no-mock-default.sh

# lint-doc-impl-drift — conventions § 0.4 enforce mechanism #3: encode
# "ADR claims X → grep code condition Y" so docs that are no longer
# true (or never were) fail fast. See script header for how to add a
# new check.
lint-doc-impl-drift:
	./scripts/lint/doc-impl-drift.sh

# smoke — conventions § 0.4 enforce mechanism #4: deployed-binary
# smoke gate. Builds fresh binaries and drives the full task-dispatch
# pipeline via the v22-deployed-pipeline Playwright spec. Phase-close
# rule (per testing.md § 2.3): deployed-smoke count = 0 means the
# phase MUST NOT close.
smoke:
	./scripts/smoke/deploy-smoke.sh

# lint — composite target for all repo-level linters.
lint: vet lint-vendor lint-mock-default lint-doc-impl-drift

# e2e-install — first-time setup of the Playwright e2e suite.
# Drops chromium browser (~170MB) into Playwright's cache.
e2e-install:
	cd tests/e2e/v2 && pnpm install --frozen-lockfile
	cd tests/e2e/v2 && pnpm exec playwright install chromium

# e2e — run the Playwright e2e suite against a fresh local binary.
# Builds first so `bin/agent-center` is up-to-date; each test spawns
# its own binary on a temp port via tests/e2e/v2/fixtures/agent-center.ts.
e2e: build
	cd tests/e2e/v2 && pnpm test

tidy:
	go mod tidy

clean:
	rm -rf ./bin coverage.out coverage.html
	# Leave .gitkeep in spa/dist/ so go:embed still has a directory.
	find ./internal/webconsole/spa/dist -mindepth 1 ! -name '.gitkeep' -delete
