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

## § 6. Execution log + M4 closure

Filled in by the doc commit.
