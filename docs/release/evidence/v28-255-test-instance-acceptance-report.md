# v2.8 #255 test-instance helper — acceptance report (Tester, independent black-box)

| | |
|---|---|
| **Scope** | #255 = pure infra + infra-piggyback (tenant-seed/UI/agent → #257; update/restart → #256) |
| **Build** | PR #154 `feat/v28-255-test-instance` @ `193afbb`, real `make build` (frontend embed + go build), binary `agent-center v2.7.1 (commit 193afbb)` |
| **Method** | Real macOS install path (real launchd activation, generated config), isolated worktree `/tmp/wt-pr154`, independent of Dev's own smoke |
| **Date** | 2026-06-05 |
| **Verdict** | **GO** — all 6 dimensions + all blocking gates PASS; #257/#256 deferrals carry explicit self-documenting pointers |

> Real machine had a **live prod center running** (`~/.agent-center/.../agent-center server`, PID 78827, with a live worker+agent) — used as the strongest D1 evidence (cleanup must not touch a real in-use prod install).

## Per-dimension results

### A — install / up / infra-piggyback
- **A1 PASS** — `install test-instance --workers 2` (no `--id`) → exit 0 in 3.4s; auto-allocated id `t1`; namespace `~/.agent-center-test/t1/`; machine-readable JSON access pack on stdout.
- **A2 PASS (blocking, anti-#159)** — center config `~/.agent-center-test/t1/center/etc/config.yaml` **contains `blob_store:`** (line 27); worker configs correctly have none. Confirms the helper drives the *real* install codepath (`installCenterFresh`→`writeCenterConfig`), not a hand-crafted config.
- **A3 PASS** — dynamic free ports (t1: web 57555 / server 57556 / admin 57557); `/api/health`→200 on web port; **none == :7000**.
- **A4 PASS** — real launchd activation: `com.agent-center.center.test-t1` (running, pid 54490) + `com.agent-center.worker.test-t1-w1` + `...-w2` all loaded.
- **A5' PASS (blocking; PD-adjusted bar)** — infra functional proof: center `/api/health`→200 `{"status":"ok","version":"v2.7.1"}`; 2 workers **workforce-registered** (listed, services running, token exchanged during install). **Control-connect 409 `worker_not_org_enrolled` independently verified** in worker log (`control: connect... status=409 ...(will retry; control disabled until then)`) — exactly matches Dev's report; full org-enroll/control-connect → #257 (needs tenant org).
- **A6' PASS (blocking; piggyback + self-documenting)** — JSON pack has `id, prefix, web_url, server_port, admin_port, admin_bootstrap_token, workers[]` **plus all three self-documenting fields**: `web_login` + `entity_ids` + `workers_note`, each pointing to `--with-seed`/#257. Doc-honesty applied to the runtime artifact — consumer sees what's missing + where to get it, no implicit gap.

### B — discovery / #170
- **B1 PASS** — `list-test-instances` → t1, web 57555, workers 2, online=yes.
- **B2 PASS** — `list-local-workers` → both workers with `NS=test`, instance t1, ids test-t1-w1/w2, mode service. Namespace field present.
- **B3 (partial / noted)** — prod-side namespace not exercised: no prod *worker* is installed on this machine (only a prod *center*); installing one would pollute the real machine, so not run. Test-side namespace verified; prod side is symmetric by the `list-local-centers` (#211) discovery pattern.

### C — concurrency
- **C1 PASS** — `install test-instance --workers 1` while t1 up → auto-id `t2`, distinct ports (58034/35/36), distinct prefix `/t2`, distinct labels `...test-t2...`; both concurrent, both `/api/health`→200.
- **C2 (noted)** — true simultaneous-parallel race not stress-tested; sequential distinct allocation verified; atomic-`mkdir` claim is the race-safe mechanism per design.

### D — uninstall / cleanup
- **D1 PASS (blocking, MOST DANGEROUS — constraint #3)** — test-instance install/uninstall **never touched real prod `~/.agent-center`**. Verified rigorously despite a live prod instance churning its own DB: (a) **zero** prod files reference test-t1/test-t2/agent-center-test; (b) **no** test-* paths anywhere under prod; (c) file count unchanged (38→38, nothing added/removed); (d) structural content (excluding the live-instance DB) byte-stable; (e) the only changed file is the live prod center's own `agent-center.db-wal` (held open by prod PID 78827; my `list-*` commands confirmed not the cause). *(Naive whole-tree byte-hash drifts because a live prod instance mutates itself — that drift is prod's own activity, not test pollution.)*
- **D2 PASS** — `uninstall test-instance --id t1` → 3 labels booted out + `/t1` subtree removed; t1 web port released; no orphans.
- **D3 PASS** — uninstall t1 while t2 up → t2 **unaffected** (health 200, labels + subtree intact).
- **D4 PASS** — `uninstall test-instance --id <nonexistent>` → "not found — nothing to do"; safe no-op.

### E — boundary / safety
- **E1 PASS (blocking)** — `--id` path-traversal rejected: `../escape`, `../../etc`, `/abs/path`, `.` all → `test_instance_bad_id` slug error; no escape directories created. Cleanup physically cannot resolve outside the test root.
- **E3 PASS** — :7000 is held by ControlCenter (macOS AirPlay, PID 635); both instances correctly avoided it (exclusion is real, not theoretical).
- **E4 PASS (blocking; #249/#251 class)** — `admin_bootstrap_token` is **NOT** in any process cmdline (`ps`) and **NOT** in any launchd plist; appears only in the 0600 stdout pack. *(An initial false-positive was investigated and cleared — the ps hit was my own `curl`; the plist "hit" was a `find -exec grep -l` exit-code bug, not a real match.)*
- **E2 (not run)** — no-free-ports clean-fail not exercised (hard to occupy the whole candidate range); low risk.

### F — multi-instance isolation
- **F1 / F3 PASS** — t1 (57555) + t2 (58034) both live and independent; distinct data dirs, distinct launchd labels, distinct ports (none == 7000); tearing down t1 left t2 fully intact (also D3).
- **F2 → #257** — UI-level isolation needs a seeded tenant; deferred to #257 (Tester2 UI-consumer).

### T-9 — capability completeness
- helper CRUD for #255 = install(C) / list(R) / uninstall(D); **install↔uninstall entry-symmetry present** = completeness bar met. Update/restart → **#256** (v2.9); tenant-seed/UI/agent → **#257** — both documented scope-cuts with concrete pointers (doc-honesty), no implicit gaps.

## Blocking gates — all PASS
A2 (real config/blob_store) · A5' (infra functional incl real workforce-enroll + verified 409) · D1 (prod byte-intact) · E1 (no path-escape) · E4 (token not in ps/plist) · F1+F3 (infra isolation).

## Deployed-smoke
This entire run IS a deployed-binary smoke (real `agent-center` binary, real launchd, real subprocesses) → satisfies `testing.md` §2.3 (deployed-smoke ≥ 1) for the #255 PR.

## Methodology notes (two investigated false-alarms — kept for the record)
1. **E4 token-in-plist "⚠️"** was a bash bug (`find -exec grep -l` returns find's exit, not grep's match) + my own curl in `ps`. Corrected checks → clean.
2. **D1 whole-tree-hash "FAIL"** was a live prod instance mutating its own DB, not test pollution. Refined (structural hash + no-test-ref + file-count + namespace separation) → PASS.
Both reinforce: investigate before reporting; mechanism-correct ≠ tested; distinguish harness-error from real finding.

## Verdict: **GO for #255** (PR #154). Untested low-risk edges (B3 prod-worker namespace, C2 true-parallel race, E2 no-free-ports) noted, non-blocking.
