# Process Saturation Recovery Evidence - 2026-08-28

## Verdict

Current node state is recovered for process-table headroom, but the historical
4000+ saturation root cause is blocked from definitive attribution in this
executor.

The live audit did not observe saturation. It observed 698-713 total processes
during the stability window and 718 total processes after the bounded
fork/exec probe. The fork/exec probe successfully started and waited for 250
short-lived child processes with zero spawn failures and zero wait failures.

This is not a claim that `3999/4000` is recovered. The highest measured live
count, including the probe burst, was 958 total processes.

## Blocking Evidence For Historical Root Cause

- Required `task-input/v1/README.md` and `task-input/v1/manifest.json` were
  absent in this worktree. See `raw/00-environment.txt`.
- This executor had no agent-center MCP/node-recovery tools exposed, and the
  task instructions explicitly prohibited using SQLite files, admin sockets,
  admin HTTP, worker tokens, `mcp_config.runtime.json`, raw HTTP, or process
  arguments as agent-center access fallbacks.
- Because the node was already well below the process ceiling at observation
  time, there was no live 4000+ process population to attribute exactly.

## Evidence Collection

Collector scripts committed with this report:

- `collect-process-evidence.sh`: captures full process rows, process totals,
  user totals, command-family totals, parent/child aggregation, and five
  samples separated by 15 seconds.
- `probe-fork-exec-headroom.sh`: starts 250 `/bin/sleep 5` children, records
  every started PID, records spawn/wait success and failure counts, and records
  before/during/after process totals.

Raw evidence files are under `raw/`. Credential-bearing argument values in
process command lines are redacted as `<REDACTED>` or `<REDACTED_MCP_CONFIG>`;
PIDs, owners, PPIDs, elapsed times, executable paths, and non-secret arguments
are preserved.

## Pre/Post Counts

Pre-remediation snapshot (`raw/before-counts.txt`, 2026-08-28T09:43:22Z):

- Total processes: 698
- Distinct users: 38
- Top owners: `oopslink` 478, `root` 135, `_rmd` 12,
  `_cmiodalassistants` 10, `_softwareupdate` 8
- Top command families: Playwright Chromium 48, Google app processes 24,
  `distnoted` 21, agent-center worker/runtime/executor processes 21, `node` 16

Stability window (`raw/observation-*-counts.txt`):

| Sample | Timestamp UTC | Total | Users |
| --- | --- | ---: | ---: |
| 1 | 2026-08-28T09:43:22Z | 698 | 38 |
| 2 | 2026-08-28T09:43:37Z | 709 | 38 |
| 3 | 2026-08-28T09:43:53Z | 712 | 38 |
| 4 | 2026-08-28T09:44:08Z | 713 | 38 |
| 5 | 2026-08-28T09:44:24Z | 711 | 38 |

Post-probe snapshot (`raw/31-counts-after.txt`, 2026-08-28T09:47:52Z):

- Total processes: 718
- Distinct users: 38
- Top owners: `oopslink` 503, `root` 135, `_rmd` 12,
  `_cmiodalassistants` 10, `_softwareupdate` 8
- Top command families: Playwright Chromium 48, Google app processes 25,
  `distnoted` 21, agent-center worker/runtime/executor processes 21, `node` 16

## Owner, PPID, And Process-Chain Attribution

The process population is dominated by normal shared-node interactive and test
families owned by `oopslink`. The current top families are:

- Playwright Chromium headless shell: 48 processes. These are grouped under
  Playwright browser parent PIDs in the full and parent/child snapshots, for
  example `raw/30-ps-after-full.txt` and
  `raw/32-children-by-ppid-command-after.txt`.
- agent-center worker/runtime/executor binaries: 21 processes. These include
  the live worker process, agent-runtime processes, supervisors, mcp-hosts, and
  this executor chain. Full PPID and command lines are in
  `raw/30-ps-after-full.txt`.
- `node`: 16 processes, including agent-browser/node-repl/other development
  tooling. Full PPID and command lines are in the same raw snapshots.

No single owner, PPID, or command family was present at a scale compatible with
the alleged 4000+ saturation during this executor's observation window.

## Bounded Remediation Record

No kill, restart, or cleanup command was executed.

Reason: the required pre-action read-only PID/owner/command parsing did not
identify a precise runaway target, and broad pattern termination would risk
mis-killing active shared-node work. The current counts were already far below
the process ceiling, so there was no bounded live target to remediate.

Non-mis-kill proof:

- No remediation target list was selected.
- No kill/restart command appears in the committed collector/probe scripts.
- Post snapshot remained stable at 718 processes with the same normal top
  families as the pre snapshot.

## Fork/Exec Headroom Probe

Probe command:

```bash
bash docs/reports/process-saturation-20260828/probe-fork-exec-headroom.sh docs/reports/process-saturation-20260828/raw 250
```

Observed in `raw/20-fork-exec-headroom.txt`:

- Requested spawns: 250
- Pre-probe total processes: 707
- During-probe total processes: 958
- Spawn success: 250
- Spawn failure: 0
- Wait success: 250
- Wait failure: 0
- Post-probe total processes: 709
- Probe script exit code recorded in raw output: 0

## Recurrence Protection

Because definitive historical attribution is blocked here, prevention should be
handled by agent-center node recovery/observability where credentials and
authoritative runtime state are available:

- Capture a process census automatically when the node crosses a soft threshold
  well below 4000, including total count, users, PPID groups, and command
  families.
- Record the exact recovery target list before any kill/restart action.
- Prefer agent-center node recovery controls over shell-level process actions.
- Keep a short fork/exec headroom probe in the recovery gate and reject
  `3999/4000` as non-recovered.

## Raw Evidence Index

- `raw/before-ps-full.txt`: full pre-remediation process table.
- `raw/before-counts.txt`: pre-remediation totals by user and command.
- `raw/before-children-by-ppid-command.txt`: pre-remediation PPID/owner/command
  grouping.
- `raw/before-pstree.txt`: `pstree` output when available, or explicit
  unavailable marker.
- `raw/observation-1-ps-full.txt` through `raw/observation-5-ps-full.txt`:
  full process tables for the stability window.
- `raw/observation-1-counts.txt` through `raw/observation-5-counts.txt`:
  stability-window totals.
- `raw/20-fork-exec-headroom.txt`: complete fork/exec probe output.
- `raw/30-ps-after-full.txt`: full post-probe process table.
- `raw/31-counts-after.txt`: post-probe totals by user and command.
- `raw/32-children-by-ppid-command-after.txt`: post-probe PPID/owner/command
  grouping.
- `raw/SHA256SUMS.txt`: SHA256 digest file for the committed raw outputs,
  excluding `SHA256SUMS.txt` itself.
