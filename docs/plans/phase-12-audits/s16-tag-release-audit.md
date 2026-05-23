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
here as a runbook for @oopslink:

```
# 1. Push tag (signs the public release commitment)
git push origin v2.0.0

# 2. Create GitHub Release with the prepared notes body
gh release create v2.0.0 \
    --title "agent-center v2.0.0" \
    --notes-file docs/release/v2.0-gh-release-body.md
```

Both require human-driven approval per [phase-12-plan-detail § 4
S16](../phase-12-plan-detail.md) ("Push gated on @oopslink +
@x9527 approval — no autonomous publish").

## § 4. Execution log

Filled by the tag commit at the bottom of this file.

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
