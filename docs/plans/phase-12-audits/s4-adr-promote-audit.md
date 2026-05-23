# P12 S4 — promote 17 v2 ADR drafts to Accepted

> Run 2026-05-24 · per x9527 oversight: every ADR moving from
> `decisions/drafts/` to `decisions/` MUST carry an evidence trail
> (phase + key commit SHAs) proving the decision was actually
> implemented. ADRs without evidence are rejected — either go back and
> ship them, or withdraw the ADR. Audit log lands SEPARATELY from the
> promote commit.

## § 0. Scope

`docs/design/decisions/drafts/` currently contains 17 ADR files. Two
of them (0031 + 0039) are already marked Accepted in `README.md` but
still live under `drafts/` — they were authored as meta-decisions that
re-shaped the v2 effort itself and got Accepted at authoring time. The
remaining 15 are Draft per `README.md`.

This S4 audit answers two questions for each:

1. **Was the ADR's intent actually implemented?** — evidence trail
2. **Are the docs / commits internally consistent?** — promote is
   safe to perform vs. requires repair

If any ADR fails (1), the promote skips it and S4 surfaces the gap.

## § 1. Evidence trail per ADR

| ADR | Title | Delivered in | Key commits (SHA — short summary) |
|---|---|---|---|
| 0023 | Worker enroll 轻量化 | **P8 § 3.3** | `462f3a3` feat(p8 § 3.3) WorkerEnrollService.Exchange (ADR-0023 § 1) · `c0f6373` feat(p8 § 3.2) BootstrapToken Entity + Repository + Service |
| 0024 | AgentInstance 一等公民化 | **P8 § 3.5** (amended by 0029) | `80ee406` feat(p8 § 3.5) AgentInstance AR + Repository + Management + Lifecycle services |
| 0025 | `agent:create` 协议 = G1 CLI Endpoint | **P10 F5** | `bbfa27a` feat(p10 F5) agent CLI + App wiring (Identity auto-register) |
| 0026 | SecretManagement BC + plaintext-never-echo | **P8 § 3.7-3.8** + **P11 F11** | `6d39ee8` feat(p8 § 3.7+3.8) SecretManagement BC8 — UserSecret + SecretRef VO · `fc433f9` feat(p11 frontend F11/16) secret CRUD + create modal (no plaintext echo) |
| 0027 | MCP per-agent 注入 (G4) | **P9 § 3.8** | `9841f74` feat(p9 § 3.2 + § 3.8) PromptAssembly v2 + MCPInjectionService |
| 0028 | Skill File Mount (v2 lite, G5) | **P9 § 3.9** | `80e5c89` docs(p9 § 3.9 + § 4) supervisor.md NACK SOP + Phase 9 test report (skill file is `assets/skills/supervisor.md`) |
| 0029 | Supervisor as Built-in AgentInstance (amends 0024) | **P8 § 3.6** | `f5c6a86` feat(p8 § 3.6) SupervisorInvocation.AgentInstanceID field |
| 0030 | AgentAdapter 矩阵扩展 (v2 G3) | **P9 § 3.3-3.6** | `009a6e6` feat(p9 § 3.3-3.6) AgentAdapter v2 interface + 3 adapter impls (claudecode / codex / opencode) |
| 0031 | v2 Drop Bridge / Vendor Integration | **P10 § 3.9** + **P12 S1-S3** | `91a2d40` feat(p10 § 3.9) delete Bridge BC code per ADR-0031 · `7cf72fb` feat(p10 F3+F6) drop v1 bridge tables (migration 0025) · `44b298a` chore(p12 S1) v1 vendor cleanup |
| 0032 | Conversation Channel as first-class + schema reset (CV1) | **P10 § 3.0-3.3** | `cefc135` feat(p10 § 3.0+3.1+3.2) Conversation v2 schema + AR/Repo + Identity refactor · `1e3918a` feat(p10 § 3.3) ChannelManagementService (CV1) + CLI |
| 0033 | Identity 模型重构 (CV2a) | **P10 § 3.2** (same commit as 0032 — Identity refactor is part of the same change-set; migration 0021 + entity rewrite) | `cefc135` feat(p10 § 3.0+3.1+3.2) Conversation v2 schema + AR/Repo + Identity refactor |
| 0034 | Conversation Participants 字段 (CV2b) | **P10 § 3.4** | `052c708` feat(p10 § 3.4) ParticipantManagementService (CV2b) + CLI |
| 0035 | 跨 Conversation Message Carry-over (CV3) | **P10 § 3.5** | `a6e175d` feat(p10 § 3.5) CarryOverService + ConversationMessageReferenceRepository (CV3) |
| 0036 | 派生 Issue/Task from Messages (CV4) | **P10 § 3.6** + **P10 F2** + **P11 SPA F9** | `c97a1ca` feat(p10 § 3.6) MessageDerivationService (CV4) service layer · `da9fa2d` feat(p10 F2) CV4 派生 CLI — issue/task --from-conversation · `8c57dde` feat(p11 frontend F9/16) derive issue/task UI from messages |
| 0037 | Web Console as v2 main UI (W1) | **P11 § 3.2-3.4** + **SPA F1-F16** | `ebb8c22` feat(p11) EventSink → SSE Bus auto fan-out · `b85d4c6` feat(p11 frontend F1/16) SPA scaffold · `07cca9b` feat(p11 frontend F15/16) Makefile + go:embed SPA into binary · `ab98065` docs(p11 F16/16) tactical + deployment + release notes + P11 final close |
| 0038 | CLI UX 增强 (W2) | **P11 § 3.8-3.9** | `ff2b137` feat(p11 § 3.8) --format universal CLI flag (table\|json\|text) · `3baa1fc` feat(p11 § 3.9) help discoverability — grouped root + topics + examples |
| 0039 | Conversation 业务模型 v2 统一 (supersedes 0017/0021/0022) | **P10 entire effort** | docs in this ADR are themselves the unification artifact; backed by `cefc135` + `1e3918a` + `052c708` + `a6e175d` + `c97a1ca` (CV1-CV4 sequence) |

**Result: 17/17 ADRs have evidence. No rejections.**

## § 2. Promote operation spec

For each ADR file under `decisions/drafts/`:

1. Add a `Status` table to the top of the file (per the format
   established by `decisions/0001-no-mcp.md`):
   ```markdown
   | Field | Value |
   |---|---|
   | Status | Accepted |
   | Date | 2026-05-24 |
   | Delivered | (phase + commit refs from § 1) |
   ```
   - Where an existing Status note exists in prose, replace it.
   - For 0029, keep "amends 0024" annotation.
2. `git mv decisions/drafts/<file> decisions/<file>`.
3. Update every cross-reference in the repo from
   `decisions/drafts/00NN-` → `decisions/00NN-` (sed batch).
4. Update `decisions/README.md` to reflect new paths + Accepted status.

Order: steps 1+2 per file (transformation), then step 3 (sed batch
once), then step 4 (README).

## § 3. Cross-reference scan

Files currently referencing `decisions/drafts/...`:

```
$ grep -RIln 'decisions/drafts/' docs/ internal/ cmd/ web/ assets/ contrib/ scripts/ Makefile 2>/dev/null | head
```

(Captured in § 8 after the script run.)

## § 4. Promote script

`scripts/promote-v2-adrs.sh` — Bash script that performs steps 1-4
above. Properties:

- **Idempotent**: running twice on a fully-promoted tree is a no-op
  (files already in `decisions/`; Status already Accepted).
- **Dry-run mode** (`--dry-run` first arg): prints planned `git mv` +
  sed substitutions without touching the tree.
- **Selective mode** (`--only <id>`): promote a single ADR (used for
  debug / verification).

The script is run ONCE for the v2 batch; it's not a recurring CI
gate. After v2.0 GA, future ADRs follow the normal "author with
Status: Draft → mature → flip to Accepted in place" flow, never going
through `drafts/`.

## § 5. Acceptance criteria

- Audit log committed first (this file, § 0-7).
- Promote commit lands second; `decisions/drafts/` empty after run.
- All 17 ADR files in `decisions/` with `Status: Accepted` + Delivered row.
- Zero references to `decisions/drafts/` anywhere in `docs/`, `internal/`,
  `cmd/`, `web/`, `assets/`, `contrib/`, `scripts/`, `Makefile`.
- `decisions/README.md` updated.
- `make lint-vendor` still clean (audit doc / promoted ADRs use known
  vendor terms; allowlist may need narrowing).

## § 6. What S4 does NOT do

- **No content edits to the ADRs themselves** (beyond the Status
  table). The decisions are valid as drafted; this ST is bureaucratic
  promotion + cross-ref hygiene, not re-litigation.
- **No supersedes operations** — ADR-0009/0017/0020/0021/0022 are
  already deleted (per README note); ADR-0039 already supersedes
  0017/0021/0022 in its prose.
- **No new ADR numbers** — v2 caps at 0039; the next ADR (post-v2)
  starts at 0040.

## § 7. Why some ADRs already had Accepted status

0031 and 0039 were "meta-decisions" — the ADR existence itself was
the deliverable (0031 ratifies that v2 drops Bridge; 0039 unifies
Conversation BC under a single model). They were marked Accepted at
authoring time because the *decision* was ratified by user
(`@oopslink`). S4 still moves them to `decisions/` to consolidate the
batch.

## § 8. Execution log

### 8.1 Audit commit
`1e70a08 docs(p12 S4) v2 ADR promote audit + evidence trail` — this
file as initially drafted (§ 0-7).

### 8.2 Promote commit
- `scripts/promote-v2-adrs.sh` — Python-in-Bash script. Idempotent
  Status table transform (flips `Status: Draft → Accepted`, updates
  Date, inserts `Delivered` row from the evidence map embedded in the
  script). `--dry-run` mode supported.
- 17 ADR files `git mv`d from `docs/design/decisions/drafts/` →
  `docs/design/decisions/`.
- 43 docs / plan / convention files updated via Python sed
  (`decisions/drafts/00NN-` → `decisions/00NN-`).
- `docs/design/decisions/README.md` updated: 17 link paths flipped,
  all 17 statuses set to Accepted (matches the in-file Status table),
  rule-note paragraph appended explaining the drafts/ flow is closed.
- `docs/design/drafts/v2-kickoff-2026-05-22.md` updated: the archive
  doc's ADR-link table flipped to new paths + a "Path migration note
  (2026-05-24)" header explaining the doc preserves the *archive-time
  status text* (Draft) while the links resolve to the *current*
  promoted files. The archive's historical narrative survives; dead
  links don't.

### 8.3 Bug caught during promote: lint allowlist held a regression

After running the promote script, `make lint-vendor` failed:
`scripts/lint/test-no-vendor-refs.sh` contains literal `feishu`
strings in its heredocs (the deliberate injections the self-test
asserts the lint catches). The S3 commit `d6eae3f` added this file
but never re-ran `make lint-vendor` standalone — I verified the
self-test (which exited green by re-running the lint *after*
cleanup) and conflated that with `make lint-vendor` passing on the
self-test script's own content. The lint was silently failing for
two commits (`d6eae3f` and `1e70a08`) before this S4 run surfaced it.

Fix in this commit:

```diff
-^scripts/lint/no-vendor-refs\.(sh|allowlist)
+^scripts/lint/no-vendor-refs\.(sh|allowlist)
+^scripts/lint/test-no-vendor-refs\.sh
```

Explicit per-file, **not** a `^scripts/lint/` wildcard — a wildcard
would whitelist `scripts/lint/.selftest/sample.go` and friends, which
would silently break the self-test's positive-fail assertion.

The promote script also gets an allowlist line for the same reason
(ADR-0031 evidence string names "Bridge BC").

The self-test's `git add -N` also needed `-f` because the
`.selftest/` dir is `.gitignore`d (per S3 defensive practice). Now:
`git add -fN ...` bypasses the ignore for staging visibility only.

**Lesson**: post-commit lint sweep is mandatory after editing the
lint script itself. S5+ will run `make lint-vendor` + `make
lint-vendor-selftest` as the final verification before each commit.

### 8.4 Verification

```
$ make lint-vendor          # clean (all hits whitelisted)
$ make lint-vendor-selftest # phase A + B both OK
$ go test ./...             # green
$ go vet ./...              # clean
$ grep -RIln 'decisions/drafts/0' docs/ internal/ cmd/ web/ assets/ contrib/ scripts/ Makefile
   docs/plans/phase-12-audits/s4-adr-promote-audit.md   (this audit doc — intentional self-reference)
   scripts/promote-v2-adrs.sh                            (the script that performed the move)
```

Zero broken cross-references in active docs; the two remaining hits
are the scaffolding that documents what was done.

### 8.5 What S5 inherits

`decisions/README.md` already has the 17 promoted entries with new
paths + Accepted status. S5's README work is mostly housekeeping:
double-check the rule-note appendix is right, verify the table sort
order, and ensure no stray status notes still say "Draft" anywhere
else in the file.
