# Phase 12 — Cleanup + Release: Detailed ST Plan

> 起草：2026-05-24 (P12 plan-first per x9527) · 状态：**待 x9527 审**
>
> 上游：[phase-12-cleanup-release.md](./phase-12-cleanup-release.md) — 高层目标 + § 3 工作项分解 (3.1–3.9)。本文档把那 9 个子项拆成 16 个可独立 ship 的 ST，每个一个 commit，附估时 + DoD。

---

## § 0. 范围 + 不做清单

**做**（per phase-12 § 3 + § 4 DoD）：
1. v1 vendor 残留扫描 (code / docs / assets / configs / schema columns)
2. CI lint rule 防 vendor 词回流
3. 17 个 v2 ADR drafts → Accepted promote
4. docs polish (Wave 2 Group A banner clean + Group B touch-up + Group C verify)
5. Migration tool v1 → v2 sqlite (含 dry-run)
6. Playwright 跨 phase e2e suite (8 scenarios)
7. Release checklist 执行 (version / CHANGELOG / DEPLOYMENT / master_key 指引)
8. v2.0 git tag + GitHub Release（**等 x9527 + 人放执行**）
9. phase-12-test-report.md 收口

**不做**：
- 性能 baseline / load testing — 显式记 v3 roadmap (per phase-12 § 5.4)
- Bridge 重新接入 / multi-user auth / TLS — v3+
- v1 install 跑时 read-only co-existence — 由 ops 自决
- 新功能开发 — 任何 "再加一个 page" / "再加一个 endpoint" 都是 v2.1+

---

## § 1. ST 拆分（16 个）

每个 ST 一个 commit，commit message 前缀 `feat(p12 S#)` 或 `docs(p12 S#)` 或 `chore(p12 S#)`。

### M1 — Cleanup & Lint (S1-S3, ~5h)

| ST | 内容 | 估时 | commit |
|---|---|---|---|
| **S1** | v1 vendor word grep audit + scripts/lint/no-vendor-refs.sh + CI invoke | 2h | `chore(p12 S1) v1 vendor grep audit + lint script` |
| **S2** | DB schema column drop migration (vendor_msg_ref / primary_channel_hint / etc) + migration up/down tests | 2h | `feat(p12 S2) drop v1-era schema columns + migration tests` |
| **S3** | assets/skills + systemd/docker compose/.env.example vendor scan + delete | 1h | `chore(p12 S3) assets + configs v1 vendor strip` |

### M2 — ADR & docs polish (S4-S7, ~7h)

| ST | 内容 | 估时 | commit |
|---|---|---|---|
| **S4** | `scripts/v2-promote-adrs.sh` + dry-run + execute + sed batch path updates (drafts/ → decisions/) | 3h | `chore(p12 S4) promote 17 v2 ADRs to Accepted + cross-ref batch` |
| **S5** | decisions/README + roadmap.md + v2-kickoff archive verify | 1h | `docs(p12 S5) decisions readme + roadmap polish + v2-kickoff verify` |
| **S6** | Wave 2 Group A banner + strikethrough sweep (12 tactical/implementation docs) | 2h | `docs(p12 S6) clear v1-era banners on Wave 2 Group A docs` |
| **S7** | Wave 2 Group B touch-up + Group C verify (strategic + requirements + new v2 docs) | 1h | `docs(p12 S7) Wave 2 Group B+C polish` |

### M3 — Playwright e2e (S8-S11, ~11h)

| ST | 内容 | 估时 | commit |
|---|---|---|---|
| **S8** | Playwright scaffold in `tests/e2e/web/` (install + config + smoke test that loads SPA root) | 2h | `feat(p12 S8) playwright scaffold + bundle smoke` |
| **S9** | e2e: cold-start user journey (center → enroll → agent → secret → channel → send → reply → derive → conclude → task) | 3h | `test(p12 S9) e2e cold-start user journey` |
| **S10** | e2e: dispatch NACK → Issue surfacing + InputRequest 端到端 + DM 1:1 | 3h | `test(p12 S10) e2e nack-issue + input-request + dm` |
| **S11** | e2e: Web↔CLI 双向 + SSE reconnect with Last-Event-ID + carry-over derive UI | 3h | `test(p12 S11) e2e web-cli bidirectional + SSE recovery + carry-over` |

### M4 — Migration tool (S12-S13, ~6h)

| ST | 内容 | 估时 | commit |
|---|---|---|---|
| **S12** | `cmd/v1-to-v2-migrate/` Go binary: identity prefix / conversation kind normalize / drop vendor cols / dry-run + apply modes + tests | 4h | `feat(p12 S12) v1-to-v2 sqlite migration tool` |
| **S13** | `docs/migration/v1-to-v2.md` step-by-step + master_key backup section + DEPLOYMENT v2 guide | 2h | `docs(p12 S13) v1-to-v2 migration guide + master_key + deployment v2` |

### M5 — Release ship (S14-S16, ~3.5h)

| ST | 内容 | 估时 | commit |
|---|---|---|---|
| **S14** | Release checklist execution: version bump v1.x→v2.0.0 + CHANGELOG.md finalize (from `docs/release/v2.0-draft.md`) | 2h | `chore(p12 S14) version bump v2.0.0 + CHANGELOG finalize` |
| **S15** | phase-12-test-report.md (cross-phase coverage roll-up + DoD self-check + commit list) | 1h | `docs(p12 S15) phase-12-test-report — v2.0 GA ready` |
| **S16** | git tag `v2.0.0` + GitHub Release notes (push **awaiting human approval**) | 0.5h | `release(p12 S16) v2.0.0 GA tag + GitHub Release` |

**Total estimate: ~32.5h ≈ 4.5 working days** (single-thread).

---

## § 2. Milestone summary

| Milestone | STs | Estimate |
|---|---|---|
| M1 Cleanup & Lint | S1–S3 | 5h |
| M2 ADR & docs polish | S4–S7 | 7h |
| M3 Playwright e2e | S8–S11 | 11h |
| M4 Migration tool | S12–S13 | 6h |
| M5 Release ship | S14–S16 | 3.5h |
| **TOTAL** | 16 ST | **~32.5h** |

P11 actual was 25.5h vs 36h plan (-29%); P12 is mostly docs / scripts / e2e (lower-risk paths) — expect on-schedule to under by similar margin, BUT release gate (S16) is gated on human (oopslink) approval not just x9527.

---

## § 3. Dependencies & ordering

```
S1 ────┬───── S2 ──────┬─── S3 ─────┐
       │                │            │
       └─── S4 ─── S5 ──┘            │
                  │                  │
                  └─ S6 ─── S7 ──────┤
                                     │
                                     ├─── S12 ──── S13 ──┐
                                     │                   │
                                     └─── S8 ──┬─ S9 ────┤
                                               ├─ S10 ───┤
                                               └─ S11 ───┤
                                                         │
                                                  S14 ──┴─ S15 ── S16
```

Strict serial: S1 → S2 → S3 (lint chain) and S4 → S5 (ADR promote chain). e2e (S8-S11) can run after S3 lands. Migration (S12-S13) needs S2 (schema clean) but is otherwise independent. S14-S16 are the release cascade — all earlier ST must complete first.

---

## § 4. Per-ST DoD highlights

### S1 v1 vendor grep audit
- Verify: `git grep -i 'feishu\|lark\|bridge' -- internal/ assets/` returns **0** non-whitelisted hits
- Whitelist: docs/, ADR-0031 (Bridge drop), historical ADR banners (0007/0016/0019), v3+ roadmap
- Output `scripts/lint/no-vendor-refs.sh` that returns 0 on clean, 1 on hit, with the matching lines
- Add CI invoke (`.github/workflows/lint.yml` or wherever ci lives)

### S2 schema migration drop
- New migration file `internal/persistence/migrations/0026_drop_v1_vendor_cols.sql` (or whatever next migration number)
- Drop targets: `messages.vendor_msg_ref`, `conversations.primary_channel_hint`, `conversations.primary_channel_thread_key` (and any other v1 leftovers found in S1)
- Two test paths:
  - Fresh v2 DB: column never exists — migrator no-ops gracefully
  - v1 DB (constructed in test via raw SQL) → migrator drops → schema check passes
- Existing migration tests still pass

### S3 assets + config strip
- `find assets/ docs/ -name '*feishu*' -o -name '*lark*' -o -name '*bridge*'` → 0 hits except whitelisted
- Remove any `assets/skills/feishu_*` / `lark_*` if present
- systemd unit / docker-compose.yml / .env.example: no `bridge.feishu.*` / `FEISHU_*` / `LARK_*` keys
- Commit also re-runs S1 lint to prove clean

### S4 ADR promote
- `scripts/v2-promote-adrs.sh --dry-run` shows planned `git mv` + sed operations
- Live run: 17 v2 ADRs (0023-0030 + 0032-0039) all in `docs/design/decisions/` (not drafts/)
- All have `Status: Accepted` (verify via grep)
- Cross-refs throughout docs use `decisions/<id>-` not `decisions/drafts/<id>-`
- ADR-0029 still references its amends-but-not-supersedes link to 0024

### S5 README + roadmap + kickoff
- `decisions/README.md` table: all 17 v2 ADRs listed, Status: Accepted
- `roadmap.md`: v2 shipped items removed; v3+ items consolidated
- `v2-kickoff-2026-05-22.md`: archive header present + full

### S6 Wave 2 Group A docs sweep
- 12 documents per phase-12 § 3.7 Group A table
- For each: no `~~strikethrough~~`, no "⚠ v1-era doc" banner, no "(v2 删 per ADR-XXXX)" inline comments — only clean v2 prose

### S7 Wave 2 Group B+C touch-up
- Group B: strategic/03-bounded-contexts + requirements + conventions + ddd-blueprint + roadmap + decisions/README — verify v2-correct
- Group C: tactical/secret-management + tactical/workforce/04-agent-instance + tactical/presentation/02-spa-architecture (already shipped in P11 F16) — content matches implementation

### S8 Playwright scaffold
- `tests/e2e/web/` directory: package.json + playwright.config.ts + 1 smoke test (`smoke.spec.ts`)
- Smoke test: `make build` artifact start → playwright `goto('http://127.0.0.1:7100/')` → assert sidebar visible
- New `make e2e` target wires `playwright test`

### S9 cold-start journey
- 1 test file: `cold-start.spec.ts`
- Sequence per phase-12 § 3.1 bullet 1, automated via Playwright + child_process.spawn to drive CLI alongside browser
- Assertions: each step's expected page text + final state (channel exists + agent active + secret created + issue concluded)

### S10 NACK + IR + DM
- 3 test files (one per scenario): `dispatch-nack-issue.spec.ts`, `input-request.spec.ts`, `dm.spec.ts`
- Each: setup → trigger → assert UI surface (Issue card visible / IR badge appears / DM message arrives)

### S11 Web↔CLI bidirectional + SSE
- 3 test files: `web-cli-bidi.spec.ts`, `sse-reconnect.spec.ts`, `carry-over-derive.spec.ts`
- SSE reconnect: kill server mid-test → restart → assert browser receives missed events via Last-Event-ID

### S12 Migration tool
- `cmd/v1-to-v2-migrate/main.go` (or `cmd/agent-center/v1-to-v2.go` subcommand)
- Modes: `--dry-run` (prints planned changes) / `--apply` (executes)
- Transformations:
  - Identity: bare ids → `kind:id` prefix
  - Conversation: `group_thread` → `channel` kind
  - Drop vendor columns (calls S2 migration)
  - Backup snapshot before apply
- Tests: round-trip a constructed v1 sqlite through `--dry-run` → assert plan, then `--apply` → assert post-state

### S13 Migration + DEPLOYMENT docs
- `docs/migration/v1-to-v2.md`: prereqs / backup / dry-run / apply / verify / rollback
- DEPLOYMENT v2 section in `docs/design/implementation/06-deployment.md`: stop v1, snapshot, migrate, fresh v2 start
- Master_key backup section: ADR-0026 § 4 cross-link + explicit "if you lose this key, every secret is dead — backup before first use"

### S14 Version bump + CHANGELOG
- `version.go` (or wherever): `v1.x.x` → `v2.0.0`
- `agent-center version` outputs v2.0.0
- `CHANGELOG.md` (new at repo root): full v2 entry from `docs/release/v2.0-draft.md`
- `docs/release/v2.0.md` (drop -draft suffix on rename)

### S15 phase-12-test-report.md
- Cross-phase coverage roll-up (Phase 8-12 summary)
- DoD self-check per `phase-12-cleanup-release.md § 4`
- Full commit list (P12 all 16 ST)
- "P12 ✅ FULLY COMPLETE — v2.0 GA ready" closing

### S16 v2.0.0 git tag
- `git tag -a v2.0.0 -m "agent-center v2.0.0 — see docs/release/v2.0.md"`
- `gh release create v2.0.0 --title 'v2.0.0' --notes-file docs/release/v2.0.md`
- **Push gated on @oopslink + @x9527 approval** — no autonomous publish

---

## § 5. Risks

| Risk | Mitigation |
|---|---|
| Playwright on Darwin arm64 / Linux CI differences | scaffold S8 verifies both; pin browser version |
| Migration tool surfaces a v1 schema variant we didn't anticipate | dry-run mode + `--report-only` flag; document recovery |
| ADR promote script edge cases (file already in decisions/ from earlier P10/P11 work) | script idempotent; first-pass dry-run reviewed |
| v1 → v2 user lost master_key during migration | DEPLOYMENT migration doc puts master_key backup as Step 0 + checklist item |
| Single dev estimate optimism (we hit -29% in P11) | M3 e2e is the biggest unknown — if S8 scaffold alone takes > 4h re-plan; M4 migration tool similarly |

---

## § 6. Open questions (await x9527)

1. **Playwright runtime in CI**: do we have a Linux CI runner that can launch headless chromium + a real binary? Or do we run e2e locally + record results?
2. **CHANGELOG location**: `/CHANGELOG.md` (repo root, standard) vs `docs/release/CHANGELOG.md`?
3. **Migration tool placement**: separate `cmd/v1-to-v2-migrate/` binary, or `agent-center migrate v1-to-v2` subcommand under existing CLI?
4. **GitHub Release**: who owns the actual `gh release create` invocation — me drafting + oopslink running, or AgentCenterDev with explicit per-event approval?

---

## § 7. DoD (本 plan 自身的)

- [ ] x9527 审过本 plan + 5 open questions 拍板
- [ ] commit 本文档到 `docs/plans/phase-12-plan-detail.md`
- [ ] 主道贴摘要 + open questions
- [ ] 等 x9527 放执行 → 进 S1
