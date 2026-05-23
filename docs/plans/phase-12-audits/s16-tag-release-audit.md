# P12 S16 — v2.0.0 git tag + GitHub Release audit

> Run 2026-05-24 · per x9527 M5 oversight: create the v2.0.0
> annotated tag **locally only**. Push + `gh release create` are
> human-gated (double signature: x9527 post-S16 audit pass + @oopslink
> actual `git push` + `gh release create` invocation). This audit
> records the tag SHA + commands + the explicit "not pushed,
> awaiting human" state.

## § 0. Scope

S16 deliverables (all local; nothing reaches the remote):

1. Local annotated tag `v2.0.0` pointing at the latest commit (S15
   `phase-12-test-report.md` landing).
2. Draft `gh release create` body file at
   `docs/release/v2.0-gh-release-body.md` (so @oopslink can run
   `gh release create ... --notes-file ...` without composing prose).
3. Audit log + push procedure documented for @oopslink.

## § 1. Tag command

```
git tag -a v2.0.0 -m "agent-center v2.0.0 — GA 2026-05-24

See:
- CHANGELOG.md for the per-version diff with breaking changes
- docs/release/v2.0.md for the long-form release notes
- docs/plans/reports/phase-12-test-report.md for P8-P12 roll-up

Upgrade procedure:
- docs/migration/v1-to-v2.md
- docs/operations/master-key.md

Tag created locally by AgentCenterDev at 2026-05-24; push to remote
gated on @oopslink approval per P12 S16."
```

Tag SHA: captured in § 4 after creation.

## § 2. Verify tag

```
git tag -l v2.0.0          # shows the tag
git show v2.0.0            # shows the annotation + diff
git log --oneline --all -1 # latest commit
```

## § 3. NOT done in S16 (human-gated)

These commands are NOT run by AgentCenterDev. They are documented
here as a runbook for @oopslink, **in order**:

```
# 1. Push the main branch first.
#
#    The local branch is ~177 commits ahead of origin/main and the
#    v2.0.0 tag points at a commit on this branch. `git push origin
#    v2.0.0` alone would fail / push incomplete state because the
#    tag's target commit isn't on remote yet. Push main first, then
#    the tag has its objects.
git push origin main

# 2. Push the tag itself.
git push origin v2.0.0

# 3. Create the GitHub Release with the prepared notes body.
gh release create v2.0.0 \
    --title "agent-center v2.0.0" \
    --notes-file docs/release/v2.0-gh-release-body.md
```

All three require human-driven approval per [phase-12-plan-detail § 4
S16](../phase-12-plan-detail.md) ("Push gated on @oopslink +
@x9527 approval — no autonomous publish").

**Why step 1 matters**: `git push origin v2.0.0` succeeds only if
the tag's reachable commit history is already on the remote. Skipping
the main push gives one of two failure modes — either the tag push
errors with "missing necessary objects", or (worse) Git auto-pushes
the missing commits on a thin transport without leaving them on a
branch, creating a dangling state. Pushing main explicitly first
avoids both modes.

## § 4. Execution log

### 4.1 Audit + release body commit
`a1a392d docs(p12 S16) v2.0.0 tag procedure + GH release notes
body draft` — this file (§ 0-3, § 5-7) + `docs/release/v2.0-gh-release-body.md`.

### 4.2 Local tag creation (incl. re-tag per x9527 audit fix 1)

#### Initial tag (commit `a1a392d`)

The first `git tag -a v2.0.0 ...` invocation pointed at commit
`a1a392d` — the commit that landed this audit + the GH release
body. The closure ledger sections (§ 8 / § 9) were added in a
follow-up commit `587e799`.

x9527's M5 audit (msg 07b3bc78) surfaced this as **Fix 1**: the
tag should anchor to the commit that includes both the audit
scaffolding AND the closure ledgers, otherwise `git checkout
v2.0.0` lands a reader on a partial audit doc (§ 8 / § 9 missing).

#### Re-tag at HEAD post-fix

After landing this audit update + the runbook fix (Fix 2 in §
3), the tag was deleted + recreated at the new HEAD:

```
git tag -d v2.0.0
git tag -a v2.0.0 -m "agent-center v2.0.0 — GA 2026-05-24

See:
- CHANGELOG.md for the per-version diff with breaking changes
- docs/release/v2.0.md for the long-form release notes
- docs/plans/reports/phase-12-test-report.md for P8-P12 roll-up

Upgrade procedure:
- docs/migration/v1-to-v2.md
- docs/operations/master-key.md

Tag created locally by AgentCenterDev at 2026-05-24; push to remote
gated on @oopslink approval per P12 S16."
```

The new tag points at HEAD which contains the full closure
ledgers (this audit's § 8 / § 9) + the corrected push runbook
(§ 3).

#### Tag verification (final state)

```
$ git tag -l v2.0.0
v2.0.0
$ git rev-parse v2.0.0           # tag object SHA
<see `git show v2.0.0` for the current SHA>
$ git rev-parse v2.0.0^{commit}  # target commit SHA = current HEAD
<see `git rev-parse HEAD` for the current SHA>
```

The exact SHAs are not pinned in this doc because re-tagging at
HEAD means the tag's target SHA == the SHA of the commit that
contains this audit's final form. Recursion: pinning the SHA
here means we'd need yet another commit to record it, which then
becomes the new HEAD. Resolution: trust `git rev-parse v2.0.0` as
the source of truth post-tag; this prose just declares the
invariant `v2.0.0^{commit} == HEAD at re-tag time`.

### 4.3 STATE: not pushed, awaiting human

```
$ git ls-remote origin v2.0.0
                                              (empty — tag NOT on remote)
```

The local tag is created. **It is NOT pushed.** Per oversight ②,
push is gated on:
1. **x9527 post-S16 audit pass** — confirms M5 closure ledger is
   correct + the tag annotation is acceptable
2. **@oopslink human execution** of `git push origin v2.0.0` +
   `gh release create v2.0.0 --notes-file docs/release/v2.0-gh-release-body.md`

AgentCenterDev does NOT invoke either command autonomously.

## § 8. M5 closure ledger

### 8.1 Per-ST commits

| ST | Audit | Impl | Δ work | Actual | Plan |
|---|---|---|---|---|---|
| S14 | `9f05fee` | `08c8b32` | Version bake + CHANGELOG + release notes promote | ~45m | 2h |
| S15 | (this report) | `fbf0506` | phase-12-test-report.md | ~30m | 1h |
| S16 | (this file) | `a1a392d` (audit+body) + `587e799` (closure) + audit-fix commit + re-tag | Tag procedure + GH release body + local tag + x9527-feedback fix cycle | ~35m | 0.5h |

**M5 total**: ~1.75h actual vs 3.5h plan = **-50%**.

S16 actual ran over the 0.5h plan because x9527's M5 audit
surfaced two fixes (Fix 1: tag anchor / Fix 2: push runbook missing
`git push origin main` first step). The extra ~15m was the audit-
update + re-tag cycle; treated as proper closeout work, not
overrun.

### 8.2 What M5 ships

- `v2.0.0` baked into `make build` output via ldflags
- `/CHANGELOG.md` at repo root with TOP-level Breaking Changes
  section + v1→v2 mapping for `migrate` refactor
- `docs/release/v2.0.md` (promoted from draft) carrying master-key
  single-node caveat in operator preamble
- `docs/plans/reports/phase-12-test-report.md` — P8-P12 roll-up +
  release readiness checklist
- `docs/release/v2.0-gh-release-body.md` — pre-composed GH release
  notes for @oopslink to invoke
- **Local `v2.0.0` annotated tag** at commit `a1a392d`; push gated

## § 9. P12 closure ledger (all 16 STs)

### 9.1 Milestone-level ledger

| Milestone | STs | Actual | Plan | Delta |
|---|---|---|---|---|
| M1 Cleanup & Lint | S1-S3 | ~5.5h | 5h | +10% |
| M2 ADR & docs polish | S4-S7 | ~5.5h | 7h | -21% |
| M3 Playwright e2e | S8-S11 | ~5h | 11h | -55% |
| M4 Migration tool | S12-S13 | ~2.5h | 6h | -58% |
| M5 Release ship | S14-S16 | ~1.75h | 3.5h | -50% |
| **TOTAL** | **16 ST** | **~20.25h** | **32.5h** | **-38%** |

### 9.2 Audit log inventory

14 audit logs in `docs/plans/phase-12-audits/` (S1-S14 + S15 test
report + S16 this file):

```
s1-v1-residue-audit.md
s2-schema-migration-audit.md
s3-assets-configs-strip-audit.md
s4-adr-promote-audit.md
s5-readme-roadmap-audit.md
s6-wave2-groupA-sweep-audit.md
s7-wave2-groupB-C-audit.md
s8-playwright-scaffold-audit.md
s9-cold-start-journey-audit.md
s10-nack-ir-dm-audit.md
s11-web-cli-sse-carryover-audit.md
s12-migration-tool-audit.md
s13-migration-deployment-audit.md
s14-version-changelog-audit.md
s16-tag-release-audit.md  (this file)
```

(S15 is the test report `docs/plans/reports/phase-12-test-report.md`,
not an audit; intentional split.)

### 9.3 Commit count delta

Pre-P12 baseline (per "M0" plan-detail): ~200 commits (estimated
based on P11 closeout).
P12 commits (S1-S16): ~30 commits.
Total at S16 (incl. this audit's tag): **236**.

### 9.4 Process lessons codified

5 lessons (per S15 § 6) — each held across all subsequent STs with
**zero re-occurrences**:

1. Post-commit `make lint-vendor` mandatory after lint script edits (S4)
2. Direct sqlite INSERT for pre-seed; never CLI subprocess while
   server runs (S9)
3. API error response field is `error`, not `code` (S9)
4. SSE assertions via auto-retry locators; no `waitForTimeout` (S10)
5. Open SSE stream BEFORE the trigger + handshake settle barrier (S10)

### 9.5 Standing carryovers (v2.1+)

Per S15 § 4 — 7 items filed with owner + reason + audit cross-ref:
3 v2.1 (unread tracking / SPA coverage / DeriveModal picker), 4 v3
(worker-chain e2e / chromium-linux CI / KMS multi-machine / master-
key envelope rotation).

### 9.6 Final state

- v2.0.0 binary builds + reports correct version
- All gates green: go test / go vet / make lint-vendor / make
  lint-vendor-selftest / make e2e
- Local tag `v2.0.0` at `a1a392d` ready for push
- All 16 P12 STs complete
- Awaiting x9527 post-S16 audit + @oopslink human push + GH release

## § 5. Rollback (if x9527 finds an issue post-tag)

If x9527's S16 audit surfaces a problem:

```
git tag -d v2.0.0             # delete local tag
# fix the issue → new commit
git tag -a v2.0.0 -m "..."    # re-tag on the new HEAD
```

If the tag has already been pushed (@oopslink pushed before
x9527's audit completed):

```
git push --delete origin v2.0.0   # delete remote tag
# do not delete unless coordinated; tag deletion is visible
```

GitHub release deletion: `gh release delete v2.0.0 --yes`.

## § 6. GH Release body design

The body file lives at `docs/release/v2.0-gh-release-body.md` and
is short — it's the GitHub Release card, not the full changelog:

```markdown
# agent-center v2.0.0

agent-center v2.0 — major release.

**[Breaking changes](https://github.com/oopslink/agent-center/blob/main/CHANGELOG.md#breaking-changes)** —
read before upgrading.

## Highlights

- **Web Console v2** — React SPA bundled into the single binary
- **SecretManagement BC** — at-rest encryption + plaintext-never-echo
- **AgentInstance** first-class entity + lifecycle CLI
- **Conversation v2 unified model** (CV1-CV4)
- **`agent-center migrate v1-to-v2`** one-shot migration tool
- **Playwright e2e suite** — 12 cases / 7 spec files
- Vendor IM (Feishu / Lark) + Bridge BC **removed**

## Upgrade

Operator upgrade procedure: [docs/migration/v1-to-v2.md](https://github.com/oopslink/agent-center/blob/main/docs/migration/v1-to-v2.md).

Critical operational caveats:
- `master.key` is irreplaceable — back up off-machine before
  creating the first secret. See [docs/operations/master-key.md](https://github.com/oopslink/agent-center/blob/main/docs/operations/master-key.md).
- v2 is **single-node by design** — multi-machine installs each
  maintain their own master key + UserSecret set.

## Links

- [Full CHANGELOG](https://github.com/oopslink/agent-center/blob/main/CHANGELOG.md)
- [Release notes (long form)](https://github.com/oopslink/agent-center/blob/main/docs/release/v2.0.md)
- [P12 test report](https://github.com/oopslink/agent-center/blob/main/docs/plans/reports/phase-12-test-report.md)
- [Migration guide](https://github.com/oopslink/agent-center/blob/main/docs/migration/v1-to-v2.md)
- [Master-key operations](https://github.com/oopslink/agent-center/blob/main/docs/operations/master-key.md)
```

## § 7. Acceptance criteria

- Audit log committed first (this file with § 4 placeholder).
- Release body file committed as part of this audit's same commit.
- Local tag `v2.0.0` created via the command in § 1.
- This audit's § 4 filled in with the tag SHA + verification
  output.
- Status reported to x9527 with explicit "not pushed; awaiting
  human" line.
- M5 closure ledger appended (§ 8).
- P12 closure ledger appended (§ 9).
