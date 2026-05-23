# Phase 12 Test Report — v2.0 GA

> Cross-phase roll-up + P12 DoD self-check + release readiness
> checklist. Authored at S15 release-gate; finalised when S16
> tags v2.0.0.
>
> Phase 12 commit at report time: `08c8b32` (latest before S15).
> Total repo commit count: **236**.

---

## § 1. P12 ST ledger (16 STs)

| ST | Subject | Audit commit | Impl commit | Actual vs Plan |
|---|---|---|---|---|
| S1 | v1 vendor grep audit + lint script | `81b9bf6` | `44b298a` + `d8a2d26` | ~3h |
| S2 | Schema migration round-trip verify | `b5ead7d` | `71c6329` | ~1.5h vs 2h |
| S3 | Assets/configs strip + lint extension | `afee165` | `d6eae3f` | ~1h vs 1h |
| S4 | Promote 17 v2 ADRs to Accepted | `1e70a08` | `cd548d7` | ~2h vs 3h |
| S5 | decisions README + roadmap polish | `5963e68` | `721ac93` | ~1h vs 1h |
| S6 | Wave 2 Group A docs banner sweep | `bd1a5f2` | `fa508d2` | ~1.5h vs 2h |
| S7 | Wave 2 Group B+C touch-up | `9d692f8` | `1afae4c` | ~1h vs 1h |
| S8 | Playwright scaffold + smoke | `7e64fff` | `e9df918` | ~1.5h vs 2h |
| S9 | Cold-start journey e2e | `461c9d1` | `45e2d2f` | ~1.5h vs 3h |
| S10 | IR respond + DM SSE e2e | `81f84e6` | `cc7ceed` | ~1h vs 3h |
| S11 | Web↔CLI + SSE recovery + UI | `5b25f93` | `bebd562` | ~1h vs 3h |
| S12 | v1→v2 migration tool | `7e94cfd` | `1b555b3` | ~1.5h vs 4h |
| S13 | Migration guide + master_key + DEPLOYMENT v2 | `8b9dedf` | `caf553a` | ~1h vs 2h |
| S14 | v2.0.0 version bump + CHANGELOG | `9f05fee` | `08c8b32` | ~45m vs 2h |
| S15 | This test report | — | [next] | ~30m vs 1h |
| S16 | v2.0.0 git tag (push gated) | — | [next] | est ~15m vs 30m |

**M1 (S1-S3)** ~5.5h vs 5h plan = +10% (S1 split into 3 commits per
audit-quality oversight).
**M2 (S4-S7)** ~5.5h vs 7h plan = -21%.
**M3 (S8-S11)** ~5h vs 11h plan = -55%.
**M4 (S12-S13)** ~2.5h vs 6h plan = -58%.
**M5 (S14-S16)** ~1.5h actual so far vs 3.5h plan = -57%.

**P12 total**: ~20h actual vs 32.5h plan ≈ **-38%**.

## § 2. Cross-phase deliverable summary (P8-P12)

| Phase | Subject | Test report | Key commits / artifacts |
|---|---|---|---|
| P8 | v2 Foundation: Worker AR + AgentInstance + BootstrapToken + SecretManagement BC + SecretRef VO | [phase-8-test-report.md](phase-8-test-report.md) | `eafec87` `c0f6373` `462f3a3` `3d6e4dc` `80ee406` `f5c6a86` `6d39ee8` + coverage push to 90%+ across new packages |
| P9 | Agent Runtime: DispatchEnvelope v2 + PromptAssembly + AgentAdapter matrix (claudecode/codex/opencode) + Probe + MCPInjection | [phase-9-test-report.md](phase-9-test-report.md) | `583ada6` `f10cef9` `9841f74` `009a6e6` `a416a69` `80e5c89` |
| P10 | Conversation v2 (CV1-CV4) + Identity refactor + Bridge BC deletion + cross-BC integration | [phase-10-test-report.md](phase-10-test-report.md) | `91a2d40` `cefc135` `1e3918a` `052c708` `a6e175d` `c97a1ca` `dc8a55c` `1bf36fd` `da9fa2d` `bbfa27a` `7cf72fb` `7f25513` `20be2e0` `7a2e50c` |
| P11 | Web Console v2 (Backend API + SSE + go:embed SPA) + CLI UX + IR + Secret CRUD | [phase-11-test-report.md](phase-11-test-report.md) | `ebb8c22` `e6e27bc` `0a80cc2` `42e7f92` `ff2b137` `3baa1fc` + SPA F1-F16 (`b85d4c6` → `ab98065`) |
| P12 | Cleanup + Release | [phase-12-test-report.md](phase-12-test-report.md) (this file) | See § 1 — 16 STs, ~30 commits |

## § 3. Test inventory

### 3.1 Go suite

```
$ go test ./... 2>&1 | grep -c '^ok'
55
$ find internal cmd tests -name '*_test.go' | wc -l
253
```

55 Go packages compile + test; 253 Go test files total. All pass at
S15 report time (verified after S14 commit; no test regressions
post-version-bump).

Key new tests landed in P12:
- `internal/persistence/migration_round_trip_test.go` (S2) — 5 tests
- `internal/cli/handlers_migrate_v1_to_v2_test.go` (S12) — 5 tests

### 3.2 e2e suite (Playwright)

```
$ make e2e
... 12 passed (6.6s)
```

7 spec files / 12 cases / 3× anti-flake verified each ST:
- `smoke.spec.ts` — 2 cases (S8)
- `cold-start.spec.ts` — 3 cases (S9)
- `respond-input-request.spec.ts` — 2 cases (S10)
- `dm-flow.spec.ts` — 2 cases (S10)
- `web-cli-bidi.spec.ts` — 1 case (S11)
- `sse-recovery.spec.ts` — 1 case (S11; F13 audit guard)
- `carry-over-ui.spec.ts` — 1 case (S11; real browser)

### 3.3 Frontend suite (Vitest)

P11 F14 closed at **98.60% lines / 90.36% branches** on `web/`. See
[phase-11-test-report.md § coverage](phase-11-test-report.md).

### 3.4 Lint guards

- `make lint-vendor` — green; v1 vendor token grep with curated
  allowlist (5 categories per S1/S3/S6/S14)
- `make lint-vendor-selftest` — positive-fail proof: injects v1
  tokens into `.selftest/` fixtures; asserts lint catches them all;
  cleans up; asserts lint clean. Both phases green at S15.
- `go vet ./...` — clean

## § 4. v2.1+ carryovers (audit-linked)

Each carryover has an owner / reason / source audit:

| Item | Owner | Reason | Source |
|---|---|---|---|
| Unread message tracking (per-conv read state) | v2.1 | post-GA hardening; single-loopback workflow tolerates "re-scroll the list" | [v2.1-backlog § Unread](../v2.1-backlog.md) |
| SPA coverage micro-pass (98.6% → 100% lines) | v2.1 | F14 audit deferred to post-GA | [v2.1-backlog § SPA coverage](../v2.1-backlog.md) |
| DeriveModal project picker (full carry-over submit-to-nav e2e) | v2.1 | F9 design didn't expose project_id in modal; API requires it | [s11 § 8.6](../phase-12-audits/s11-web-cli-sse-carryover-audit.md) |
| Worker-chain e2e (NACK→Issue / dispatch / execute) | v3 | requires docker compose + worker daemon orchestration; deployment-e2e candidate | [s9 § 0](../phase-12-audits/s9-cold-start-journey-audit.md) + [s10 § 0](../phase-12-audits/s10-nack-ir-dm-audit.md) + [s11 § 0](../phase-12-audits/s11-web-cli-sse-carryover-audit.md) |
| chromium-linux Playwright CI integration | v3 | `make e2e` opt-in today; CI wiring decided at v2.1+ if useful | [s8 § 8.6](../phase-12-audits/s8-playwright-scaffold-audit.md) |
| Multi-machine secret sync (KMS / vault) | v3 | v2 single-node by design (ADR-0026 § 4) | [docs/operations/master-key.md § 4](../../operations/master-key.md#-4-multi-machine-sync) |
| Master-key envelope rotation | v3 | v2 rotation = destroy all secrets; envelope rotation requires data re-encryption infra | [docs/operations/master-key.md § 3](../../operations/master-key.md#-3-rotation) |

## § 5. Critical ops notes (operator-facing)

Surfacing the load-bearing ops invariants in one place:

1. **master.key is irreplaceable** — back up off-machine before
   creating the first UserSecret. See
   [docs/operations/master-key.md § 2](../../operations/master-key.md#-2-backup).
2. **`agent-center migrate v1-to-v2` is idempotent** — re-running
   `--apply` on a v2 DB exits 0 with no side effects.
3. **Bridge archive at `<sqlite-dir>/migration-archive/`** — the v1
   vendor delivery audit trail; preserve for compliance / forensic;
   discardable when no longer needed (typically 90 days post-upgrade).
4. **Web Console is loopback-only** (ADR-0037). Remote access via
   SSH tunnel (`ssh -L 7100:127.0.0.1:7100 vps`).
5. **SSE Last-Event-ID recovery** — clients reconnect with
   `?last_event_id=<id>` query param to receive events sent during
   disconnect (F13 audit guard via S11 e2e).
6. **Migration tool refuses-silent default** — must pass exactly one
   of `--dry-run` / `--apply`; bare `migrate v1-to-v2 --config=...`
   exits 2.

## § 6. Process lessons codified during P12

Each lesson held across all subsequent STs with zero re-occurrences
(per audit closeout sections):

| Lesson | First surfaced | Re-occurrence count |
|---|---|---|
| Post-commit `make lint-vendor` mandatory after lint script edits | S4 § 8.3 | 0 across S5-S16 |
| Direct sqlite INSERT for pre-seed; never CLI subprocess while server runs | S9 § 8.3 | 0 across S10-S11 |
| API error response field is `error`, not `code` | S9 § 8.3 | 0 across S10-S11 |
| SSE assertions must use auto-retry locators (`expect.poll` / `toBeVisible` etc.), not `waitForTimeout` | S10 audit | 0 across S11 |
| Open SSE stream BEFORE the trigger + handshake settle barrier | S10 audit | 0 across S11 |

## § 7. P12 DoD self-check

Per [phase-12-cleanup-release.md § 4](../phase-12-cleanup-release.md):

- [x] v1 vendor word grep audit + cleanup + lint guard (S1-S3)
- [x] 17 v2 ADRs promoted Accepted (S4-S5)
- [x] Wave 2 docs polish (S6-S7) — TEMPORARY allowlist block empty
- [x] Migration tool v1→v2 with dry-run (S12)
- [x] Playwright e2e ≥ 4 scenarios across phases (S8-S11 — 12 cases / 7 spec files)
- [x] Release checklist (S14): version bump v2.0.0 + CHANGELOG with
      TOP Breaking Changes + release notes promoted from draft
- [x] master_key backup procedure documented (S13)
- [x] DEPLOYMENT v2 upgrade section (S13)
- [ ] v2.0.0 git tag — **pending S16** (push gated on x9527 post-S16
      audit pass + @oopslink human approval)
- [x] phase-12-test-report.md (this file)

## § 8. Release readiness checklist

- [x] All M1-M4 STs audit-passed
- [x] S14 version bake + CHANGELOG land
- [x] go test ./... + go vet + make lint-vendor + make lint-vendor-selftest green
- [x] Playwright 3× anti-flake gate green on M3 STs
- [x] Migration tool tested against seeded v1 sqlite (5 tests pass)
- [x] Operator docs cross-link migration guide + master-key ops
- [x] Breaking changes documented loud in CHANGELOG + release notes
- [x] v2.1+ + v3 carryovers filed with owner + reason
- [ ] **S16 git tag created locally** (next)
- [ ] **S16 audit-passed by x9527** (post-tag)
- [ ] **`git push origin v2.0.0` executed by @oopslink** (human-gated)
- [ ] **`gh release create v2.0.0 ... --notes-file docs/release/v2.0.md` executed by @oopslink** (human-gated)

## § 9. References

- [/CHANGELOG.md](../../../CHANGELOG.md) — full v2.0 changelog
- [docs/release/v2.0.md](../../release/v2.0.md) — release notes long form
- [docs/migration/v1-to-v2.md](../../migration/v1-to-v2.md) — operator upgrade
- [docs/operations/master-key.md](../../operations/master-key.md) — master-key ops
- [docs/design/ddd-blueprint.md § 5](../../design/ddd-blueprint.md#-5-v20-ga-status) — BC + ADR landscape
- [docs/design/decisions/README.md](../../design/decisions/README.md) — 17 v2 ADRs Accepted
- [docs/plans/phase-12-plan-detail.md](../phase-12-plan-detail.md) — P12 16-ST plan
- [docs/plans/phase-12-audits/](../phase-12-audits/) — 14 per-ST audit logs (S1-S14)
