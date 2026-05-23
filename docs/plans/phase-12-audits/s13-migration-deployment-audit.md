# P12 S13 — v1→v2 migration guide + master_key + DEPLOYMENT v2 audit

> Run 2026-05-24 · per x9527 M4 oversight: complete the migration
> story with operator-facing docs. master_key is a v2 critical
> asset — generation, backup, rotation, multi-machine sync MUST be
> documented. Audit log + doc commit separated.

## § 0. Scope

Three doc deliverables:

1. **`docs/migration/v1-to-v2.md`** (new) — step-by-step migration
   guide: pre-flight backup, dry-run, apply, verify, rollback,
   bridge-archive interpretation.
2. **`docs/operations/master-key.md`** (new) — master_key
   generation / storage / backup / rotation / multi-machine sync
   procedures. SecretManagement BC is a v2-new BC and the master
   key is the only piece of cryptographic state that can't be
   reconstructed if lost.
3. **`docs/design/implementation/06-deployment.md`** (update) — add a
   "v2 fresh install vs v1→v2 upgrade" section that links to the
   two new docs.

## § 1. Migration guide structure

```
docs/migration/v1-to-v2.md
├── § 0  Why upgrade
├── § 1  Pre-flight checklist
│        ├── stop v1 server
│        ├── backup sqlite + master.key (if pre-existing)
│        └── verify --dry-run reports zero unexpected counts
├── § 2  Run the migration
│        ├── dry-run
│        ├── interpret output
│        └── apply
├── § 3  Verify v2 server boots
├── § 4  Bridge archive — what + where + when needed
├── § 5  Rollback (untested — use the pre-migration backup)
└── § 6  FAQ / common pitfalls
```

## § 2. Master-key doc structure

```
docs/operations/master-key.md
├── § 0  What it is + why it matters (ADR-0026 § 4 cross-link)
├── § 1  First-time generation
│        head -c 32 /dev/urandom | base64 > master.key
│        chmod 0600 master.key
├── § 2  Backup
│        ├── store off-machine (password manager, vault, sealed envelope)
│        ├── do NOT commit to git, do NOT email plain
│        └── one-line audit field for who-has-the-backup
├── § 3  Rotation
│        ├── v2 does NOT rotate in place (UserSecret are encrypted
│        │   at-rest with the master key; rotating without
│        │   re-encrypting all secrets would lose them)
│        ├── procedure: revoke all secrets → swap key → recreate
│        └── deferred-to-v3 note: key envelope rotation
├── § 4  Multi-machine sync
│        ├── v2 single-node only — no sync needed
│        └── v3 candidate: KMS / cloud-vault adapters
└── § 5  Disaster recovery
         ├── lost master.key + no backup = all secrets dead
         ├── recovery procedure: revoke all UserSecrets via SQL +
         │   delete user_secrets rows + start fresh
         └── operational impact: agents requiring those secrets
             must be reconfigured
```

## § 3. DEPLOYMENT v2 update

Add to `06-deployment.md`:
- New § 10.4 "v1 → v2 upgrade" pointing at `docs/migration/v1-to-v2.md`
- Update § 10.1 to mention master.key generation step (already
  added in S6 sweep)
- Cross-link `docs/operations/master-key.md`

## § 4. Acceptance criteria

- Audit log committed first.
- Doc commit lands second; 2 new docs + 1 doc updated.
- `make lint-vendor` clean (the migration doc names dropped v1
  vendor tables in the bridge-archive section — allowlist may need
  extension or the references can be paraphrased).
- All cross-references resolve (no broken links).

## § 5. What S13 does NOT do

- Automated rollback — see migration guide § 5; manual backup
  restore only.
- Multi-machine deployment guide — v2 is single-node by design;
  v3 candidate.
- Key vault / KMS integration — v3 candidate (per § 2 § 4).

## § 6. Execution log

### 6.1 Audit commit
`8b9dedf docs(p12 S13) migration + master_key + DEPLOYMENT v2 audit`
— this file (§ 0-5).

### 6.2 Doc commit

- `docs/migration/v1-to-v2.md` (new) — 7 sections + FAQ:
  - § 0 why upgrade (links the 6 driving ADRs)
  - § 1 pre-flight: stop / backup sqlite / backup config / master.key
    generation / strip v1 vendor config sections
  - § 2 dry-run + apply commands with expected output
  - § 3 verify v2 server boots (curl /api/health + version)
  - § 4 bridge archive shape + when you need it + when you can
    throw it away
  - § 5 manual rollback (uses pre-flight backup)
  - § 6 FAQ — old schema baseline / unknown_section / master.key
    missing / loopback bind / re-archive (bug indicator)
- `docs/operations/master-key.md` (new) — 7 sections:
  - § 0 loss profile table (in-mem only / disk only / both lost)
  - § 1 first-time generation
  - § 2 backup — acceptable + forbidden locations + audit log
  - § 3 rotation — v2 DESTROYS all secrets; only do if compromised;
    v3 envelope rotation deferred
  - § 4 multi-machine — v2 single-node; v3 KMS adapter candidate
  - § 5 disaster recovery
  - § 6 audit log of key events
- `docs/design/implementation/06-deployment.md` — added § 10.4
  v1→v2 upgrade pointer + § 10.5 master-key operations summary;
  both link to the new docs.

### 6.3 Allowlist extension

The migration tool + tests + 2 new docs reference v1 vendor table
names by design (snapshot / interpret). Allowlist gained 3 entries
under category 2 (in-code historical markers):

```
^internal/cli/handlers_migrate_v1_to_v2(_test)?\.go:
^docs/migration/v1-to-v2\.md:
^docs/operations/master-key\.md:
```

`make lint-vendor` clean post-extension; `make lint-vendor-selftest`
still passes both phases (positive-fail assertion intact).

### 6.4 Cross-link audit

Every cross-reference resolves to an existing file:
- migration guide → operations/master-key.md ✓
- migration guide → design/decisions/0026/0031/0037 + release notes ✓
- master-key doc → design/decisions/0026/0027 + 04-configuration ✓
- 06-deployment § 10.4 → docs/migration/v1-to-v2.md ✓
- 06-deployment § 10.5 → docs/operations/master-key.md ✓

### 6.5 Verification

- `make lint-vendor` clean
- `make lint-vendor-selftest` both phases OK
- `go test ./...` green
- `go vet ./...` clean

## § 7. M4 closure ledger

### 7.1 Per-ST commits

| ST | Audit | Code/Doc | Highlights |
|---|---|---|---|
| S12 | `7e94cfd` | `1b555b3` | Migration tool (5 tests; group refactor; idempotent; bridge archive) |
| S13 | `8b9dedf` | [this commit] | Migration guide + master-key ops doc + DEPLOYMENT v2 update + allowlist extension |

4 commits / 2 audit logs / 2 implementation commits.

### 7.2 Estimate vs actual

| | Plan | Actual |
|---|---|---|
| S12 migration tool | 4h | ~1.5h |
| S13 docs | 2h | ~1h |
| **M4 total** | **6h** | **~2.5h** |
| **Delta** | — | **-58%** |

Accelerators:
- Migration tool composes on existing `Migrator.Up()` (S2 work);
  only the bridge archive + CLI wiring are new.
- Idempotency + dry-run / apply patterns were already in S1's
  promote-v2-adrs.sh script; reused the conceptual model.
- Docs reuse the structure conventions from S5/S6/S7 doc work.

### 7.3 What M4 ships

- `agent-center migrate v1-to-v2` subcommand with --dry-run /
  --apply / --archive-dir / idempotent / refuses-silent
- 5 automated tests covering dry-run, apply, idempotent, refuse,
  fresh-v2 paths
- Operator-facing migration guide with backup procedure, dry-run
  interpretation, apply, verify, manual rollback, FAQ
- Master-key operational doc — the v2-critical asset that has no
  in-code counterpart (must be operator-managed)
- 06-deployment.md cross-references to both new docs

### 7.4 What M4 hands off to M5

S14 release work needs to:
- Bump version to v2.0.0
- Rename `docs/release/v2.0-draft.md` → `v2.0.md`
- Add a "breaking change: `migrate` is now a group; use `migrate up`
  instead of bare `migrate`" line to CHANGELOG
- Cross-link the migration guide from release notes

S15 test report rolls up all phases (P8-P12).

S16 git tag + GitHub Release — push gated on human approval.
