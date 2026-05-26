# Changelog

All notable changes land here. Format inspired by
[keepachangelog.com](https://keepachangelog.com/en/1.1.0/); semver
([semver.org](https://semver.org/)) for version numbers.

This project did not maintain a CHANGELOG.md before v2.0.0; commit
history is the authoritative record for v1. For the v2 design /
ADR / phase plan landscape, see
[docs/design/ddd-blueprint.md § 5](docs/design/ddd-blueprint.md#-5-v20-ga-status).

---

## [Unreleased — v2.4.0 draft] — 2026-05-26

> This is the draft release-notes section for v2.4. v2.3 work landed
> on `main` between v2.2 and v2.4 without its own tag — its highlights
> are summarized below under "v2.3 carry-over" so the v2.2 → v2.4
> diff stays readable.

### Highlights

v2.4 ships the **first-mile deployment** experience that v2.0 GA was
missing. Before v2.4 you assembled the worker invocation by hand
(fingerprint, bearer token, capabilities, etc.); now `./install center`
and a Web Console **Add Worker** Modal cover the path from extracted
tarball to running worker in well under a minute on Mac.

### Added (v2.4-D first-mile)

- **`agent-center install center|worker` subcommand** — single
  idempotent command for fresh install + upgrade, with cross-OS
  service unit generation (launchd on Mac, systemd on Linux).
- **Atomic symlink-swap upgrade with auto-rollback** — new version is
  laid down at `<prefix>/versions/<new>/`, the schema migration runs,
  `<prefix>/current` is flipped via POSIX `rename(2)`, and the
  installer probes the health endpoint. Probe failure → automatic
  symlink rollback + service restart.
- **One-time-use enroll tokens** — new `AdminToken` flavor with
  `is_enroll + expires_at + used_at` columns (migration 0029).
  30-minute default TTL; CAS-based first-use-burns via
  `used_at IS NULL` in the auth middleware. Coexists with v2.3-3a
  long-term tokens.
- **Web Console Add Worker UX** — `/fleet` top-bar **+ Add Worker**
  button + `AddWorkerModal` (7-state machine: minting / ready /
  success / token_used / token_expired / timeout_hint) showing a
  copyable install command. Live transition to **Worker connected**
  via SSE `workforce.worker.enrolled`. Newly-enrolled Fleet rows
  briefly pulse green; a global toast in the bottom-right acts as
  fallback when the Modal is closed.
- **Home Get-started card** — Home page shows a prominent **Add a
  worker** CTA when no workers are enrolled, so the first-mile gap
  is visible on the landing surface.
- **Friendly install failure messages** — disk full, port in use,
  permission denied (systemd unit / binary write), upgrade health
  probe failure all map to `<friendly> / What to try / Underlying`
  output instead of raw syscall errors. Preflight port-availability
  check runs before service activation.

### Added (v2.3 carry-over — already on `main`)

- **Multi-host TCP+TLS admin endpoint** with SSH-style fingerprint
  pinning, per-token bucket rate limiting, and audit IP capture.
  See [docs/deployment/v2.3-multi-host.md](docs/deployment/v2.3-multi-host.md)
  for the operator walkthrough — still authoritative for the cross-
  host internals.
- **Real agent dispatch chain** — `/admin/secret/user-secret/resolve`,
  `/admin/blob/put`, `defaultAgentSpawner` wires `AssemblePrompt` +
  `MCPInjector`. Previously v2.2 wired the transport but the agent
  spawn was a stub.
- **`AdminToken` AR + middleware + CLI** — `agent-center admintoken
  create/list/revoke` for long-lived per-worker tokens. v2.4-D's
  enroll-token model layers on top.
- **BC-native `/api/issues` + `/api/tasks` list endpoints** + SPA
  surfaces driven by them (project filter is now a real filter, not
  cosmetic).
- **SPA polish** — DeriveModal project picker, unread tracking
  schema + service + frontend, per-conversation SSE subscribe, Web
  Console UX/UI overhaul, Home `bento-grid` dashboard.

### Schema

- **migration 0029** — `admin_tokens` gains `is_enroll`,
  `expires_at`, `used_at` columns + partial index for the
  enroll-token sub-table.
- `targetSchemaVersion` bumped 28 → 29.

### Docs

- New: [docs/deployment/v2.4-first-mile.md](docs/deployment/v2.4-first-mile.md)
  — operator guide for install / enroll / upgrade / rollback / 12
  failure modes.
- The v2.3 multi-host guide is unchanged and remains authoritative
  for fingerprint hygiene, rate-limit tuning, and cross-host
  internals.

### Deferred to v3 (or later v2 minors)

- Tarball distribution (downloads.agent-center.dev etc.) — v2.5+
- New SSE events `worker.enroll_attempt_failed` +
  `admintoken.expired` — these are nice-to-have for richer Modal
  feedback; the client's 5-min timeout state covers the silent-fail
  case in v2.4. See audit
  [v24-D-A4](docs/plans/v2.4-audits/v24-D-A4-sse-events-audit.md).
- Linux acceptance — v2.4 scope is Mac-only per @oopslink's
  acceptance scope. Linux units are written + unit-tested but not
  acceptance-validated; that lands in v2.5.

---

## [v2.2.0] — 2026-05-25

⚠ **MINOR VERSION** with one breaking config-default change
(Web Console default flipped to ON). Full upgrade procedure in
[docs/migration/v2.0-to-v2.2.md](docs/migration/v2.0-to-v2.2.md).

### Highlights

v2.2 closes the v2.0 GA defect that @oopslink surfaced on 2026-05-24
("前端 + 数据面完整，但 worker process 装配从未交付"). v2.0/v2.1
shipped without an actual worker process, without admin transport
between CLI and server, and with `dispatch.NoopSender{}` wired into
production — dispatched tasks went into /dev/null. v2.2 ships the
full transport architecture per `conventions.md § 0.4` ("AppService
is the only entry to domain state").

### Added

- **`cmd/worker-daemon` binary** — separate process that connects to
  the server via admin unix socket, enrolls, polls the dispatch + kill
  queues, spawns the agent CLI subprocess, and reports back via admin
  endpoints. Replaces the placeholder `agent-center worker run` that
  v2.0 GA shipped as "reserved for Phase 2".
- **`cmd/fakeagent` binary** — scripted agent stub for LLM-independent
  testing. Used by the deployed-binary e2e smoke and operator
  manual-verify recipes.
- **Admin endpoint (unix socket)** — `internal/admin/api` package with
  93 routes covering the full CLI AppService surface, per BC. Default
  socket path `/run/agent-center/admin.sock` (configurable via
  `server.admin_socket_path`). Per ADR-0037 still loopback only;
  multi-host TCP reserved for v2.3 (ADR-0040).
- **In-process dispatch queue** (`internal/admin/dispatchq`) — real
  `EnvelopeSender` + `KillSender` backed by per-worker FIFO. Worker
  daemons drain via admin endpoint.
- **Real `SupervisorSpawner` wired in ServerCommand** — supervisor
  invocations actually fork+exec. v2.0 GA had `app.SupervisorSpawner = nil`.
- **Deployed-binary smoke gate** — `make smoke` runs Phase D Playwright
  spec end-to-end against real binaries (no mocks). New
  `tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts` drives a task
  through `submitted → working → completed`.
- **Process gates** (per `conventions § 0.4` Enforce mechanisms):
  - `make lint-mock-default` — `NoopSender{}` / `NoopKillSender{}` in
    production wiring must carry `// FIXME(prod-wiring):` annotation.
  - `make lint-doc-impl-drift` — anchor-based check for documented
    architecture claims vs codebase reality.
  - `TestArch_NoDirectPersistenceOpenInHandlers` — enforces
    `internal/cli/handlers_*.go` whitelist.
- **Layered test report standard** (`docs/rules/testing.md § 2.3`) —
  unit / integration-with-mocks / deployed-binary-smoke must be
  reported separately; deployed-smoke = 0 means the phase MUST NOT
  close.
- **v2.0 → v2.2 upgrade guide** (`docs/migration/v2.0-to-v2.2.md`).
- **Mac single-host deployment guide** (`docs/deployment/v2.2-mac-single-host.md`).

### Breaking changes

1. **Web Console default flipped to ON**. `config.WebConsoleConfig`
   default seeds `Enabled: true, ListenAddr: "127.0.0.1:7100"`. v2.0
   configs that omitted `web_console.enabled` ran headless; v2.2 such
   configs now boot the SPA on loopback. Opt out with explicit
   `web_console: {enabled: false}`. See migration guide § 2.1.

### Refactor

- **CLI through admin transport** — all 36 CLI subcommands now route
  through admin endpoint via `internal/cli/admin_client.go` instead
  of opening sqlite directly. Whitelist: `handlers_migrate*.go` and
  `handlers_system.go` only.
- **`dispatch.NoopSender` + `kill.NoopKillSender` removed from
  production wiring** — replaced with `dispatchq.DispatchSender` and
  `dispatchq.KillSender`. The Noop variants remain in their packages
  as legitimate test doubles (with `// FIXME(prod-wiring):` annotations
  on the constructor fallback paths).
- **`internal/workerdaemon/` package** — previously ~2500 LOC never
  imported in production; v2.2 wires it through `cmd/worker-daemon`.

### Known follow-ups (v2.3 backlog)

Filed in `docs/plans/v2.2-audits/v22-closeout-audit.md § 4`:
participant/leave endpoint, msg/find-recent endpoint, dispatch +
DecisionRecord same-tx, kill + DecisionRecord same-tx,
read-task-context endpoint, worker heartbeat endpoint, MCP injection
wire, artifact blob upload, multi-host TCP transport.

---

## [v2.0.0] — 2026-05-24

⚠ **MAJOR VERSION**. Read the [breaking changes](#breaking-changes)
section below before upgrading. The full operator upgrade procedure
is in [docs/migration/v1-to-v2.md](docs/migration/v1-to-v2.md).

### Breaking changes

1. **`migrate` CLI command refactored into a group**

   | v1 | v2 |
   |---|---|
   | `agent-center migrate` | `agent-center migrate up` |
   | `agent-center migrate --target=N` | `agent-center migrate up --target=N` |
   | _(did not exist)_ | `agent-center migrate v1-to-v2 --dry-run` |
   | _(did not exist)_ | `agent-center migrate v1-to-v2 --apply` |

   Why: v2 introduces a second migration verb (`v1-to-v2`), and the
   router requires a leaf-vs-group split. Existing schema-up behavior
   is preserved verbatim under `migrate up`.
   Action: update any scripts that invoke `migrate ...` to use
   `migrate up ...`.

2. **Bridge BC + vendor IM integration removed**
   (per [ADR-0031](docs/design/decisions/0031-v2-drop-bridge-vendor-integration.md))

   Feishu / Lark / Bridge BC tables / vendor adapters deleted. v2
   exposes Web Console (loopback bind only) + CLI as the only user
   entry points. **If you depend on vendor IM, do not upgrade** until
   v3 re-introduces external IM with a new architecture.

3. **Identity model 4 kinds → 3 kinds**
   (per [ADR-0033](docs/design/decisions/0033-identity-model-refactor.md))

   v1 supported `user / agent / supervisor / bot`. v2 supports
   `user / agent / system`. Migration 0021 DELETEs identities with
   v1-only kinds; the `migrate v1-to-v2` tool runs this automatically.

4. **Conversation v2 unified model**
   (per [ADR-0039](docs/design/decisions/0039-conversation-business-model-v2-unified.md))

   `Conversation.kind` value `group_thread` is renamed to `channel`.
   `kind=task` is 1:1 with Task; `kind=issue` is 1:1 with Issue (the
   v1 separate `IssueComment` table is gone — issue discussion lives
   as Messages in the Issue's bound Conversation). ADR-0017 / 0021 /
   0022 are superseded. Migration 0024 handles the rename
   automatically.

5. **SecretManagement BC introduces master.key + single-node only**
   (per [ADR-0026](docs/design/decisions/0026-user-secret-management-bc.md))

   v2 requires `secret_management.master_key_file` set in config + a
   32-byte AES-256 key on disk (mode 0600). Without it, the secret
   service is disabled (every secret endpoint returns 501).

   **Operational caveat — v2 is single-node by design**: multi-machine
   installs each maintain their OWN master key + UserSecret set;
   cross-machine secret sync is a v3 candidate (KMS adapter). If you
   run multiple agent-center instances, do not rely on master keys
   matching across machines. See
   [docs/operations/master-key.md](docs/operations/master-key.md)
   for generation / backup / rotation procedures.

6. **`notification.*` + `bridge.*` config sections removed**

   v2 rejects unknown YAML keys (per the `04-configuration § 4`
   strict-validate rule) — these sections will cause startup failure
   if left in place. Strip both before upgrading.

### Added

- **Web Console v2** — React SPA bundled into the single binary via
  go:embed; 13 pages cover channel / DM / issue / task / agent /
  secret / input-request / fleet
  ([ADR-0037](docs/design/decisions/0037-web-console-as-main-user-ui.md))
- **SecretManagement BC** — `UserSecret` AR + master-key-encrypted
  at-rest + plaintext-never-echo guarantee
  ([ADR-0026](docs/design/decisions/0026-user-secret-management-bc.md))
- **AgentInstance first-class entity** + lifecycle CLI
  ([ADR-0024](docs/design/decisions/0024-agent-instance-first-class.md)
  / [ADR-0025](docs/design/decisions/0025-agent-create-via-cli-not-protocol.md))
- **Worker enroll** bootstrap-token exchange
  ([ADR-0023](docs/design/decisions/0023-worker-enroll-lightweight.md))
- **AgentAdapter v2 matrix** — claudecode + codex + opencode
  adapters ([ADR-0030](docs/design/decisions/0030-agentadapter-matrix-expansion.md))
- **MCP per-agent injection**
  ([ADR-0027](docs/design/decisions/0027-mcp-per-agent-injection.md))
- **Skill file mount** — `assets/skills/supervisor.md`
  ([ADR-0028](docs/design/decisions/0028-skill-file-mount-lite.md))
- **Conversation v2**: channel first-class (CV1) / Identity refactor
  (CV2a) / Participants JSON (CV2b) / Cross-conv message carry-over
  (CV3) / Issue+Task derive-from-messages (CV4) — ADRs 0032 / 0033 /
  0034 / 0035 / 0036 / 0039
- **CLI UX**: `--format=table|json|text` universal flag + grouped
  help + topic index
  ([ADR-0038](docs/design/decisions/0038-cli-ux-enhancement.md))
- **`agent-center migrate v1-to-v2`** migration tool: `--dry-run` /
  `--apply` / idempotent / bridge-archive JSON
- **Playwright e2e suite** — 12 cases / 7 spec files; opt-in via
  `make e2e`; dual-mode chromium-mac + chromium-linux config
- **v1 vendor lint guard** — `make lint-vendor` + positive-fail
  self-test (`make lint-vendor-selftest`)
- **Operator docs**:
  [v1→v2 migration guide](docs/migration/v1-to-v2.md) +
  [master_key operations](docs/operations/master-key.md)
- **Migration round-trip + v1 column/table/kind absence guard
  tests** in `internal/persistence/migration_round_trip_test.go`

### Changed

- Bounded-context composition: Bridge removed, SecretManagement
  added (net BC count unchanged at 7)
- Issue discussion model: separate `IssueComment` table is gone;
  Issue messages live as `Message` rows in the `kind=issue`
  Conversation per ADR-0039
- Roadmap restructured into three sections: **v2 ✅ 已完成** /
  **v2.1 backlog** / **v3 推迟**
- Decisions/README + all design docs polished for v2; v2 banner
  applied to 16 tactical / implementation docs
- 17 v2 ADRs (0023-0039) promoted from `decisions/drafts/` to
  `decisions/` with `Status: Accepted` + evidence-trail Delivered row

### Removed

- Bridge BC (`internal/bridge/*` deleted in P10 § 3.9)
- Feishu / DingTalk vendor adapters + WebSocket outbound
- v1 ADRs **0009 / 0017 / 0020 / 0021 / 0022** (one-time exception
  to "never delete ADRs" per ADR-0031)
- v1 vendor config sections (`notification.*`, `bridge.*`)
- v1 vendor identity kinds (`supervisor`, `bot`)
- Schema artifacts: `vendor_msg_ref`, `channel_bindings`,
  `feishu_delivery_ledger`, `bridge_subscription_cursors`,
  `conversations.{primary_channel_hint, primary_channel_thread_key,
  title}`, `workers.capabilities`

### Deprecated

None. v2 has no deprecation period — every v1 surface either
survived intact, was breaking-changed, or removed outright.

### v2.1+ backlog

Explicitly deferred (see [docs/plans/v2.1-backlog.md](docs/plans/v2.1-backlog.md)
+ [roadmap.md](docs/design/roadmap.md)):

- Unread message tracking (per-conv read state)
- SPA coverage micro-pass (98.6% → 100% lines)
- DeriveModal project picker (full submit-to-navigation e2e)
- Worker-chain e2e via docker compose (NACK→Issue / dispatch /
  execute) — v3 deployment e2e candidate
- chromium-linux Playwright CI integration
- KMS / vault-backed master key (multi-machine secret sync)

---

[v2.0.0]: https://github.com/oopslink/agent-center/releases/tag/v2.0.0
