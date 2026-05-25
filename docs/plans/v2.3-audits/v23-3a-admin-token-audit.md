# v2.3-3a Admin Token Auth Foundation Audit

> Closes slock task #28. Replaces the "loopback unix-socket = implicit
> trust" model with proper bearer-token auth on every admin endpoint
> call. Per @oopslink: "标准的 API token 方案" — identity-distinguishable,
> revocable, scope-narrowable, transport-agnostic. Strong precondition
> for task #29 (real-agent dispatch — needs `secret:resolve` scope)
> and task #27 (multi-host TCP — same middleware applies over TCP).

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。先建 `admintoken` 新 BC（AR / Repo / Service），再加 middleware；不是「先做 middleware 再倒着补 AR」 |
| 保留 BC invariants？ | 是。所有现有 BC 零行为变更；admin transport 是新增 wrapping 层 |
| 没省 transport？ | 是。**强化** transport：所有 admin endpoint 现在强制 bearer auth，`/admin/health` 是唯一白名单 |
| Mock-as-default 消除？ | N/A（本 ST 无 sender/spawner 改动） |
| 起点 = "领域模型要求"？ | 是。`AdminToken` AR shape + scope 集合从领域抽象（不是从 HTTP 反推） |

## § 1. 范围

### 新 BC `admintoken/`
- `types.go` — TokenID / Owner / Scope types + 9 sentinel errors
- `admin_token.go` — AR with `HasScope` / `Revoke` / `MarkUsed` + pure helpers `GeneratePlaintext` (acat_<32 base64url>) / `HashPlaintext` (sha256) / `ParseBearer`
- `repository.go` — Repository port
- `sqlite/repo.go` — Save / FindByID / FindByHash / FindByOwner / FindAll / Revoke (CAS) / UpdateLastUsedAt
- `service/service.go` — Create / VerifyPlaintext / Revoke / FindByID / FindAll / **MarkUsedAsync (single pump + 30s throttle)**
- migration 0028 admin_tokens

### Admin endpoint middleware
- `internal/admin/api/auth.go` — `AuthMiddleware(Verifier)` → 401 / inject AuthContext / async last_used_at bump
- `RequireScope(w, r, scope)` helper for scope-gated handlers
- whitelist: `GET /admin/health` only

### Admin endpoints for token CRUD
- `POST /admin/admintoken/create` (scope `admin:token`) — returns plaintext ONCE
- `GET /admin/admintoken/list` (scope `admin:token`) — projection excludes value_hash + plaintext
- `POST /admin/admintoken/revoke` (scope `admin:token`)

### CLI
- `agent-center admin token {create,list,revoke}` (handlers_admin.go)
- `Client.WithToken(t)` + `Authorization: Bearer <token>` header in every request
- token resolution order: env `AGENT_CENTER_ADMIN_TOKEN` → `~/.agent-center/admin_token` → `<sqlite_dir>/bootstrap_token`

### Worker daemon
- `AdminClient.WithToken(t)` + header
- `cmd/worker-daemon/main.go --admin-token` flag (env fallback)

### Server boot
- `EnsureBootstrapToken` runs on first start: if `admin_tokens` empty, create owner=`system:bootstrap` scope=`*`, write plaintext to `<sqlite_dir>/bootstrap_token` (0600), log banner
- `runAdminEndpoint` wraps mux with `AuthMiddleware(app.AdminTokenSvc)`

## § 2. 设计要点 / 决策

1. **scope set v0** (immutable per token): `*`, `admin:token`, `secret:resolve`, `blob:put`, `dispatch:pull`, `task:*`. Future scopes additive; revoke + recreate to change scopes per token (no UPDATE)
2. **value_hash = sha256(`acat_<body>`)**: hash includes the `acat_` prefix so DB dumps reveal nothing useful
3. **plaintext format `acat_<43-char base64url>`** — greppable (`acat_[A-Za-z0-9_-]+` finds any leaked plaintext)
4. **MarkUsedAsync = 1 goroutine pump + per-id 30s throttle**: original `go func{UPDATE}` pattern caused `SQLITE_LOCKED(517)` on macOS under worker-daemon polling (5/sec). Pump serializes; throttle absorbs bursts. Bookkeeping is best-effort
5. **No plaintext in list response or events**: ADR-0026 plaintext-never-echo extended to admin transport. Plaintext only flows via the one-time `create` response
6. **Bootstrap token is system-owned + `*` scope**: minimum bootstrap that unblocks an operator to mint scoped tokens for workers/CLI. Should be revoked after long-term tokens land — documented in DEPLOYMENT
7. **Test harness mints fresh token per setupAdminServerForTests**: doesn't depend on bootstrap state; closes pump on cleanup

## § 3. 验证

### Tests added
- `internal/admintoken/admin_token_test.go` — AR (~14 cases): plaintext shape, hash stability, ParseBearer (missing/invalid/Bearer-prefix/bare), HasScope (* match + exact), Revoke twice = ErrTokenRevoked, New validates owner / scopes / hash size
- `internal/admintoken/service/service_test.go` — Service: Create / VerifyPlaintext happy / VerifyPlaintext revoked / VerifyPlaintext unknown / Revoke / scope dedupe
- `internal/admintoken/sqlite/repo_test.go` — Repo round-trips: Save / FindByID / FindByHash / FindByOwner / FindAll / Revoke (CAS path) / UpdateLastUsedAt
- `internal/admin/api/auth_test.go` — middleware: /admin/health pass-through; 401 missing/invalid/unknown/revoked; happy path injects AuthContext + calls MarkUsedAsync; RequireScope 403

### Full-stack verification
- ✅ `go test ./...` — every package green (no regression)
- ✅ `make lint` — vet / no-vendor-refs / no-mock-default / doc-impl-drift all pass
- ✅ `make smoke` — Phase D deployed-pipeline still ~2s pass; e2e spec updated to:
  - read `bootstrap_token` after server start
  - thread `Authorization: Bearer <token>` on all admin calls
  - pass `--admin-token <bootstrap-token>` to worker daemon
- ✅ `tests/integration/integration_test.go` migration version 27 → 28

## § 4. Out of scope (deferred)

- Per-scope endpoint gating beyond `admin:token` (e.g. `task:*` on task endpoints) — middleware authenticates, but per-endpoint scope checks land **with the endpoints that need them** in #29 (`secret:resolve`, `blob:put`)
- TCP listener support (task #27) — middleware is transport-agnostic so #27 only changes the listener
- token TTL / expiration — current model is "revoke-only"; expiry is future work
- audit events for token create/revoke — minimal v0 doesn't emit, but Service is positioned to emit when sink wiring is added

## § 5. Migration story

Existing v2.2.0 deployments upgrade:
1. `make build` v2.3 binary
2. Restart `agent-center server` — migration 0028 runs, `bootstrap_token` file written, banner shows path
3. Read `cat <sqlite_dir>/bootstrap_token`
4. Mint scoped worker tokens: `agent-center admin token create --owner=worker:mac-w-1 --scope=dispatch:pull,task:* --token <bootstrap>` (the `--token` flag passes bootstrap until env/file is set)
5. Restart worker daemon with `--admin-token <new>` or `AGENT_CENTER_ADMIN_TOKEN=<new>`
6. Optionally revoke bootstrap token

(DEPLOYMENT doc update lands in #29 alongside the secret-resolve / blob-put endpoint deploy guide.)

## § 6. Diff stats

- New: 7 files (admintoken BC + middleware + bootstrap + CLI command file + admin client method file + 2 migrations + 4 test files)
- Modified: 14 files (App wiring + admin server wiring + AdminClient + worker daemon + test helpers + e2e spec + migration target version bumps)
- Net: ~750 lines code + ~400 lines tests
