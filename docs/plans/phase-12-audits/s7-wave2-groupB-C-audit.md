# P12 S7 — Wave 2 Group B+C docs verify

> Run 2026-05-24 · per phase-12-plan-detail § M2 S7: verify Group B
> (strategic / requirements / conventions / ddd-blueprint / roadmap /
> decisions README) + Group C (tactical/secret-management /
> tactical/workforce/04-agent-instance / tactical/presentation/* — all
> shipped in P11 F16) are v2-correct + match implementation. Audit
> log lands SEPARATELY from any touch-up edits.

## § 0. Scope

| Group | Path | Status entering S7 |
|---|---|---|
| **B** | `docs/design/architecture/strategic/03-bounded-contexts.md` | v2 banner present (S6 surgical edit landed `feishu_delivery_ledger` rewrite) |
| **B** | `docs/design/requirements/01-functional.md` | v2 banner present from earlier phase |
| **B** | `docs/design/requirements/02-non-functional.md` | v2-clean (no vendor mentions) |
| **B** | `docs/design/requirements/03-out-of-scope.md` | v2-clean |
| **B** | `docs/design/requirements/04-assumptions.md` | v2-clean |
| **B** | `docs/design/requirements/00-overview.md` | v2-clean |
| **B** | `docs/rules/conventions.md` | S6 rewrote e2e example to Web Console; v2-clean |
| **B** | `docs/design/ddd-blueprint.md` | v2 banner present; some inline `~~Bridge~~` annotations in living-plan history tables remain |
| **B** | `docs/design/roadmap.md` | S5 restructured into 3 sections; v2-clean |
| **B** | `docs/design/decisions/README.md` | S4 promote landed; all 17 v2 ADRs Accepted |
| **C** | `docs/design/architecture/tactical/secret-management/00-overview.md` | New v2 doc (P8); no v1 history to scrub |
| **C** | `docs/design/architecture/tactical/secret-management/01-user-secret.md` | New v2 doc (P8) |
| **C** | `docs/design/architecture/tactical/workforce/04-agent-instance.md` | New v2 doc (P8) |
| **C** | `docs/design/architecture/tactical/presentation/01-web-console.md` | New v2 doc (P11) |
| **C** | `docs/design/architecture/tactical/presentation/02-spa-architecture.md` | New v2 doc (P11 F16) |

## § 1. Vendor-leak scan results

```
$ grep -RIcn 'feishu\|lark\|dingtalk\|wechat' \
    docs/design/requirements/ \
    docs/design/architecture/tactical/secret-management/ \
    docs/design/architecture/tactical/workforce/04-agent-instance.md \
    docs/design/architecture/tactical/presentation/
(zero hits)
```

All Group C docs and the bulk of Group B are vendor-clean. The only
remaining vendor mentions in Group B are in `ddd-blueprint.md`'s
*living-plan history tables* (rows tracking what was decided when),
which by design preserve "Bridge BC was deleted per ADR-0031" as
a status-table annotation rather than rewriting history. Those are
already covered by the v2 banner (S6) and pass lint (allowlist
category 2: "in-code / in-doc historical markers").

## § 2. Implementation correspondence check

For Group C, the v2 docs must match what's in the code. Spot-check:

| Doc | Verified against |
|---|---|
| `secret-management/00-overview.md` | `internal/secretmgmt/*.go` (AR, repo, service); P8 commit `6d39ee8`; P11 commit `fc433f9` (UI no-plaintext-echo) |
| `secret-management/01-user-secret.md` | `internal/secretmgmt/user_secret.go` AR + state machine (active / revoked) |
| `workforce/04-agent-instance.md` | `internal/workforce/agent_instance/*.go`; P8 commit `80ee406`; CLI agent create P10 F5 `bbfa27a` |
| `presentation/01-web-console.md` | `internal/webconsole/api/*.go` + `internal/webconsole/sse/*.go`; P11 `ebb8c22` + go:embed `07cca9b` |
| `presentation/02-spa-architecture.md` | `web/src/**/*.tsx` SPA + ADR-0037; P11 F16 `ab98065` |

All Group C docs accurately describe shipped v2 code (verified by
ADR Delivered evidence trail from S4).

## § 3. What S7 changes

Two touch-ups based on the verify:

1. `docs/design/ddd-blueprint.md` — top of file says "最后更新：
   2026-05-20" but the v2 work continued through 2026-05-24. Update
   the timestamp line.
2. Add a closing § "v2.0 GA status" to ddd-blueprint pointing at the
   v2.0 release notes + the post-promote ADR landscape.

These are minor edits — most of the doc body is preserved.

## § 4. Acceptance criteria

- Audit log committed first (this file).
- Edit commit lands second; touch-ups per § 3.
- `make lint-vendor` clean.
- `make lint-vendor-selftest` both phases OK.
- `go test ./...` + `go vet ./...` unaffected.
- M2 closure summary appended to this audit (§ 5).

## § 5. M2 closure

### 5.1 S7 edit commit
- `docs/design/ddd-blueprint.md` — timestamp updated to 2026-05-24
  (v2.0 GA); added new `§ 5 v2.0 GA Status` section with BC
  landscape table (6 + BC8 SecretManagement; BC7 Bridge deleted) +
  ADR landscape table (17 v2 ADRs Accepted) + references to P12
  artifacts. Original § 5 references renumbered to § 6.

### 5.2 M2 ledger (ST × commits)

| ST | Audit commit | Cleanup / Edit commit | Notes |
|---|---|---|---|
| S4 | `1e70a08` | `cd548d7` | 17 ADRs Accepted; sed cross-ref batch + scripts/promote-v2-adrs.sh; allowlist regression caught + fixed mid-flight |
| S5 | `5963e68` | `721ac93` | roadmap restructured into 3 sections (v2 ✅ / v2.1 / v3); README required no further edits |
| S6 | `bd1a5f2` | `fa508d2` | Wave 2 Group A sweep (hard rewrite index.md + 04/06 implementation docs; bulk strikethrough-vendor line deletion across 16 docs; banner upgrades; TEMPORARY allowlist block removed) |
| S7 | `9d692f8` | [this commit] | Group B+C verified clean; ddd-blueprint timestamp + v2.0 GA status card added |

**Total M2 = 8 commits across 4 STs.**

### 5.3 M2 estimate vs actual

| | Plan | Actual |
|---|---|---|
| **Estimate** | 7h (S4 3h + S5 1h + S6 2h + S7 1h) | — |
| **Actual** | — | ~5.5h (S4 ~2h with bug-fix overhead; S5 ~1h; S6 ~1.5h; S7 ~1h) |
| **Delta** | -21% vs plan | hard rewrites in S6 were smaller than feared because v2 banners already documented the v1 → v2 narrative; bulk Python sweep + the existing surgical patterns from S1 made the doc edit pass fast |

### 5.4 Verification (M2 closure)

```
$ make lint-vendor          # clean (all hits whitelisted)
$ make lint-vendor-selftest # both phases OK
$ go test ./...             # green
$ go vet ./...              # clean
$ ls docs/design/decisions/drafts/  # empty
$ grep -c '~~' docs/design/roadmap.md  # 0
$ grep -RIcn 'TEMPORARY' scripts/lint/no-vendor-refs.allowlist  # 0
```

### 5.5 What M2 ships

- 17 v2 ADRs promoted to Accepted with evidence trail.
- `decisions/README.md` updated; `roadmap.md` restructured into
  3-column v2 ✅ / v2.1 backlog / v3 deferred shape.
- Wave 2 Group A docs swept (banners upgraded, vendor strikethrough
  lines deleted across 16 docs, hard rewrites of index.md +
  04-configuration.md + 06-deployment.md).
- S1 lint allowlist TEMPORARY block REMOVED; lint surface remains
  clean.
- `ddd-blueprint.md` v2.0 GA status card added (BC landscape + ADR
  landscape).
- `scripts/promote-v2-adrs.sh` script preserved for reference
  (idempotent; not part of normal flow post-v2).

### 5.6 What M2 hands off to M3 (Playwright e2e)

- Live config / docs are v2-clean; e2e expectations can pull text
  strings from the actual v2 surfaces without v1 fallback.
- `decisions/README.md` is the source of truth for ADR statuses;
  if e2e tests document the policies they verify, they can link
  directly to `decisions/00NN-...md`.
- The `make lint` composite now == `vet + lint-vendor`. M3
  should add a separate `make e2e` target.
