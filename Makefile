.PHONY: build build-frontend build-backend test cover cover-html lint lint-vendor lint-vendor-selftest vet tidy clean

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

build: build-frontend build-backend

build-frontend:
	cd $(WEB) && pnpm install --frozen-lockfile
	cd $(WEB) && pnpm run build
	# vite's emptyOutDir wipes .gitkeep; restore it so the directory
	# survives `make clean` + remains tracked for fresh clones (go:embed
	# needs the dir to exist before the SPA is ever built).
	echo "Populated by 'make build-frontend' (vite build outDir)." > ./internal/webconsole/spa/dist/.gitkeep

build-backend:
	go build -o ./bin/$(BIN) ./cmd/agent-center

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

# lint — composite target for all repo-level linters.
lint: vet lint-vendor

tidy:
	go mod tidy

clean:
	rm -rf ./bin coverage.out coverage.html
	# Leave .gitkeep in spa/dist/ so go:embed still has a directory.
	find ./internal/webconsole/spa/dist -mindepth 1 ! -name '.gitkeep' -delete
