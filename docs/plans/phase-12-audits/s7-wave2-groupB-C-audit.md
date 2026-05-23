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

To be filled in by the edit commit.
