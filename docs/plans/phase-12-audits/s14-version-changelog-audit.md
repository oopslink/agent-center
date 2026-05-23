# P12 S14 — v2.0.0 version bump + CHANGELOG audit

> Run 2026-05-24 · per x9527 M5 oversight: VERSION wired into build;
> CHANGELOG.md at repo root with TOP-level Breaking Changes section
> (loud, not footnote) including v1→v2 command mapping for the
> migrate refactor; release notes draft promoted to final; release
> notes carry the master-key multi-machine = single-node caveat
> (not buried only in ops doc). Audit + code commit separated.

## § 0. Scope

S14 deliverables (3 file changes + 1 verify):

1. **Build version**: Makefile `VERSION := v2.0.0` + ldflags into
   `buildVersion`. `agent-center version` reports `v2.0.0` after
   `make build`.
2. **`/CHANGELOG.md`** (new at repo root, per P12 plan-detail open
   question 2 — "CHANGELOG location: /CHANGELOG.md or docs/release/?"
   resolved to repo root for industry convention):
   - **TOP-level "Breaking changes" section** with the `migrate`
     command refactor + v1→v2 mapping
   - "Added" / "Changed" / "Removed" / "Deprecated" sections
   - Cross-link `docs/migration/v1-to-v2.md`
3. **`docs/release/v2.0.md`** — promote from `v2.0-draft.md` (git
   mv); update banner from "draft" → "v2.0 GA 2026-05-24"; add the
   master-key multi-machine single-node caveat into the breaking
   changes prose (not just ops doc); ensure CHANGELOG + release
   notes content reconcile.
4. **Verify**: `make build && ./bin/agent-center version` shows
   `v2.0.0`; `go test ./... + go vet ./... + make lint`-vendor clean.

## § 1. CHANGELOG.md design

```
# Changelog

## [v2.0.0] — 2026-05-24

⚠ **MAJOR VERSION**. Read [breaking changes](#breaking-changes)
before upgrading. See [v1→v2 migration guide](docs/migration/v1-to-v2.md)
for the upgrade procedure.

### Breaking changes

1. **`migrate` CLI command refactored**
   v1: `agent-center migrate [--target=N]`
   v2: `agent-center migrate up [--target=N]`  (same behavior, renamed)
       `agent-center migrate v1-to-v2 --apply` (NEW; one-shot v1→v2 upgrade)
   Why: v2 introduces a second migration verb, requiring the group/leaf split.
   Action: update any scripts that invoke `migrate ...` to use
           `migrate up ...`.

2. **Bridge BC + vendor IM integration removed** (ADR-0031)
   Feishu / Lark / Bridge BC tables / vendor adapters deleted. v2
   exposes Web Console (loopback) + CLI as the only user entry
   points. If you depended on vendor IM, do not upgrade until v3
   re-introduces external IM.

3. **Identity model 4→3 kinds** (ADR-0033)
   v1 supported user/agent/supervisor/bot identity kinds. v2 only
   user/agent/system. Migration 0021 DELETEs rows with v1-only kinds.
   Tool: `agent-center migrate v1-to-v2` handles automatically.

4. **Conversation v2 unified model** (ADR-0039)
   Conversation kind `group_thread` → `channel`. `kind=task` 1:1 with
   Task; `kind=issue` 1:1 with Issue. ADR-0017/0021/0022 superseded.
   Migration 0024 handles rename automatically.

5. **SecretManagement BC requires master.key** (ADR-0026)
   v2 needs `secret_management.master_key_file` set in config + a
   32-byte AES-256 key on disk (mode 0600). Without it, the secret
   service is disabled (every secret operation returns 501).
   Operational caveat: v2 is **single-node by design** — multi-
   machine installs each maintain their OWN master key + UserSecret
   set; cross-machine secret sync is a v3 candidate. See
   [docs/operations/master-key.md](docs/operations/master-key.md).

6. **Notification + bridge config sections removed**
   `notification.*` and `bridge.*` YAML sections cause unknown-key
   errors in v2. Strip before upgrading.

### Added

- Web Console v2 — React SPA bundled into single binary via go:embed
  ([ADR-0037](docs/design/decisions/0037-web-console-as-main-user-ui.md))
- SecretManagement BC — UserSecret AR + AES-256 at-rest encryption
  + plaintext-never-echo guarantee
  ([ADR-0026](docs/design/decisions/0026-user-secret-management-bc.md))
- AgentInstance first-class entity + lifecycle CLI
  ([ADR-0024](docs/design/decisions/0024-agent-instance-first-class.md)
  / [ADR-0025](docs/design/decisions/0025-agent-create-via-cli-not-protocol.md))
- Worker enroll bootstrap-token exchange
  ([ADR-0023](docs/design/decisions/0023-worker-enroll-lightweight.md))
- AgentAdapter v2 matrix (claudecode / codex / opencode)
  ([ADR-0030](docs/design/decisions/0030-agentadapter-matrix-expansion.md))
- MCP per-agent injection ([ADR-0027](docs/design/decisions/0027-mcp-per-agent-injection.md))
- Skill file mount ([ADR-0028](docs/design/decisions/0028-skill-file-mount-lite.md))
- Conversation v2: channel first-class / Identity refactor /
  Participants JSON / CarryOver (CV3) / Derivation (CV4)
  (ADRs 0032-0036 + 0039)
- CLI UX: `--format=table|json|text` universal + grouped help +
  topic index ([ADR-0038](docs/design/decisions/0038-cli-ux-enhancement.md))
- `agent-center migrate v1-to-v2` migration tool (dry-run / apply
  / idempotent / bridge archive)
- Playwright e2e suite (12 cases / 7 spec files; opt-in `make e2e`)
- v1 vendor lint guard (`make lint-vendor` + self-test)
- Operator docs: [v1→v2 migration guide](docs/migration/v1-to-v2.md)
  + [master_key operations](docs/operations/master-key.md)
- Migration round-trip + v1 column/table/kind absence guard tests
  in `internal/persistence/migration_round_trip_test.go`

### Changed

- BC count effectively the same (7); composition rebalanced:
  Bridge removed, SecretManagement added
- Conversation BC unified (Issue uses `kind=issue` Conversation with
  Messages instead of separate IssueComment table)
- Roadmap restructured into v2 ✅ / v2.1 backlog / v3 deferred
- Decisions README + all design docs polished for v2

### Removed

- Bridge BC (`internal/bridge/*` deleted in P10 § 3.9)
- Feishu / DingTalk vendor adapters
- v1 ADRs 0009 / 0017 / 0020 / 0021 / 0022 (one-time exception
  per ADR-0031)
- v1 vendor config sections (`notification.*`, `bridge.*`)
- v1 vendor identity kinds (`supervisor`, `bot`)
- `vendor_msg_ref` / `channel_bindings` / `feishu_delivery_ledger`
  / `bridge_subscription_cursors` schema artifacts

### Deprecated

None. v2 has no deprecation period — everything either survived
intact, was breaking-changed, or removed outright.

---

For the full design landscape, see
[docs/design/ddd-blueprint.md § 5](docs/design/ddd-blueprint.md#-5-v20-ga-status).
For ADR landscape:
[docs/design/decisions/README.md](docs/design/decisions/README.md).
For pre-v2 changelog: this project did not maintain a CHANGELOG.md
before v2.0.0; commit history is the authoritative record for v1.
```

## § 2. Release notes promotion

`docs/release/v2.0-draft.md` → `docs/release/v2.0.md` via `git mv`.
Update the front-matter:

- Change "Status: draft" → "Status: GA 2026-05-24"
- Add the master-key multi-machine caveat in the breaking-changes
  prose
- Add a back-link to `/CHANGELOG.md`

## § 3. Build version wiring

```makefile
VERSION ?= v2.0.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

build-backend:
	go build -ldflags "-X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT)" \
	    -o ./bin/$(BIN) ./cmd/agent-center
```

After this, `./bin/agent-center version` reports `v2.0.0` (+ commit
short SHA).

## § 4. Acceptance criteria

- Audit log committed first.
- Code+doc commit lands second.
- `make build && ./bin/agent-center version` → contains "v2.0.0"
- `go test ./...` green + `go vet ./...` clean + `make lint-vendor`
  clean
- CHANGELOG.md exists at repo root with TOP-level Breaking Changes
- `docs/release/v2.0.md` exists (and v2.0-draft.md is gone)
- All cross-references resolve

## § 5. What S14 does NOT do

- Test report (S15)
- git tag (S16; gated on x9527 + @oopslink approval)
- Push tag to remote (never automatic per plan-detail § 4 S16)

## § 6. Execution log

To be appended by the code+doc commit.
