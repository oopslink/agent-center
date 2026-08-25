# T1517 Final Main Fresh Validation

## Provenance

- Live `origin/main` before clone: `188082803dbf2d110397293cf6128b86f603b9a4`.
- Fresh detached clone: `/tmp/t1517-final-main-fresh-1787642555`.
- Detached SHA: `188082803dbf2d110397293cf6128b86f603b9a4`.
- Previous local T1512 evidence commit observed: `d785a6a8fa8aa244a17041762a2ac5026714754a`, parent `1a8a55e9e9b862c15c05300689c1b08cd328df7d`; current live main is newer.
- Final remote re-read after validation: `188082803dbf2d110397293cf6128b86f603b9a4`.
- Task-input package: `task-input/v1/README.md` and `task-input/v1/manifest.json` were not present in the executor workspace; validation proceeded from the user-provided task contract and live `origin/main`.

## Build And Runtime

- `make build`: PASS. Raw log: `evidence/logs/make-build.log`.
- Binary version: `agent-center HEAD-18808280 (commit 18808280)`. Raw: `evidence/raw/binary-version.txt`.
- Final isolated runtime:
  - Prefix: `/tmp/t1517-final-main-runtime-final-1787643350`
  - Web: `http://127.0.0.1:18125`
  - Admin TCP: `127.0.0.1:18126`
  - DB/blob/master key are under the isolated prefix.
  - Startup log: `evidence/logs/server-final-attached.log`.
- Fresh bootstrap probe: `{"initialized":false}`. Raw: `evidence/raw/http-final-bootstrap.json`.

## Real Validation Coverage

The final runner `run-real-validation.mjs` used the deployed binary over HTTP and a fresh Chromium session. It did not read SQLite.

- RAM Roles: system roles visible, custom role create, stale version 409, version create, referenced revoke 409.
- Managed/Internal: direct binding creates hidden managed/internal role; RAM Role catalog excludes `managed`, `internal`, and `role-access-*`; direct role detail returns 404.
- Direct binding: default Team Role has no RAM Roles, permission/resource-kind linkage returns `org=allowed` and `team=not_applicable`, revoke preview succeeds, bad token returns 409, confirm succeeds, derived revoke is `not_applicable`.
- Team Role layering: create team with RAM Roles, read mapping, preview added role, save mapping, stale save 409, empty operator role.
- UI/browser: real product pages captured at desktop/tablet/mobile where applicable; no console errors; no unexpected browser API failures.
- MCP: `go run ./cmd/mcp-tools-export` exported the real per-agent MCP catalog via in-memory MCP transport, 104 tools across 10 domains. No `role-access-*` or `Access grant` strings were found in the export.

## Evidence Index

- Structured checklist: `evidence/raw/checklist.json`.
- Verdict file: `evidence/raw/verdict.txt`.
- API originals: `evidence/raw/*api*`, numbered request/response JSON files, and `evidence/raw/scenario-ids.json`.
- Browser network: `evidence/network/browser-network.json`.
- Browser console: `evidence/console/browser-console.json`.
- Screenshots:
  - `evidence/screenshots/access-ram-roles-1440x900.png`
  - `evidence/screenshots/access-ram-roles-768x900.png`
  - `evidence/screenshots/access-ram-roles-390x844.png`
  - `evidence/screenshots/subject-access-1440x900.png`
  - `evidence/screenshots/team-role-developer-1440x900.png`
  - `evidence/screenshots/team-role-operator-empty-1280x720.png`
- MCP export: `evidence/raw/mcp-tools.gen.js`, log `evidence/logs/mcp-tools-export.log`.

## Setup Retries Preserved

Several startup/runner attempts were preserved as raw logs:

- `server.log`: rejected unsupported `blob_store.retention_days`.
- `server-2.log`: Web Console auth missing `secret_management.master_key_file`.
- `server-3.log`: master key was hex, but this SHA expects base64-decoded 32 bytes.
- `real-validation-1.log` and `real-validation-2.log`: runner path/module resolution setup issues before final execution.
- `real-validation-4.log`: completed scenario against a reused DB after an earlier partial run; superseded by final fresh runtime on port `18125`.

## Verdict

PASS for final fresh `origin/main` SHA `188082803dbf2d110397293cf6128b86f603b9a4`.

All final checklist rows are `MATCH`; no product code was modified.
