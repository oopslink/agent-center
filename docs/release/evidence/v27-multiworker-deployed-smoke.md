# v2.7 deployed-smoke — multi-worker per machine isolation

**Date:** 2026-06-03
**Trunk:** `e5240dc` (v2.7-integration; install logic == `#171` `7dcbda7`)
**Run by:** AgentCenterDev (install/backend domain)
**Acceptance-checklist ref:** §5 Worker — "deployed-smoke 已验" marker.

## What this proves

A real `agent-center install worker` binary, run twice with two distinct
`--worker-id`s, produces two fully independent installs on one machine:
separate prefix subtrees, launchd labels, data dirs, and logs, with zero
overlap and no clobber. Also confirms the `#171` guard (`--worker-id`
required, no hostname fallback).

This is the **filesystem + service-label isolation layer**. The runtime
integration layer (two live daemons actually running side by side) is
already covered by the worker-daemon tests (#108 / D5 et al.) and is not
re-done here — per PD.

## Method (safe, no host pollution)

- Real binary built from trunk: `go build -o $BIN ./cmd/agent-center`.
- Isolated `HOME=<tmp>` so the default worker prefix resolves to
  `<tmp>/.agent-center/workers/<id>/` and any plist lands under
  `<tmp>/Library/LaunchAgents/` — **the real `~/Library/LaunchAgents` is
  never touched**.
- `AGENT_CENTER_INSTALL_SKIP_ACTIVATE=1` so `launchctl bootstrap` is not
  invoked (file layout + rendered plists are still written, which is what
  the isolation check inspects).
- `unix:/...` bootstrap so no server fingerprint is required; a dummy
  token is fine because enrollment only runs when the service starts
  (skipped).

### Reproduce

```bash
BIN=/tmp/ac-mw-smoke/agent-center
go build -o "$BIN" ./cmd/agent-center
export HOME=/tmp/ac-mw-smoke/home; rm -rf "$HOME"; mkdir -p "$HOME"
export AGENT_CENTER_INSTALL_SKIP_ACTIVATE=1

"$BIN" install worker --user-mode --worker-id=w-alpha \
  --bootstrap=unix:/tmp/ac-mw-smoke/alpha.sock --token=dummy-alpha
"$BIN" install worker --user-mode --worker-id=w-beta \
  --bootstrap=unix:/tmp/ac-mw-smoke/beta.sock --token=dummy-beta

# bare install (no --worker-id) must error (#171)
"$BIN" install worker --user-mode --bootstrap=unix:/tmp/x.sock --token=dummy
```

## Results — 6/6 PASS

| # | Check | Result |
|---|---|---|
| 1 | Prefix subtree isolation | ✅ `~/.agent-center/workers/w-alpha/` and `…/w-beta/` are fully independent (each has `etc/ var/ logs/ current versions/`) |
| 2 | Second install does not clobber the first | ✅ w-alpha file set **byte-identical** before/after the w-beta install (`diff` clean) |
| 3 | launchd Label isolation | ✅ two plists `com.agent-center.worker.w-alpha` vs `…w-beta`; each `ProgramArguments` references its own `--worker-id` + prefix |
| 4 | Data dir isolation | ✅ `…/w-alpha/var/worker.db` vs `…/w-beta/var/worker.db`; the two configs share **zero** paths (`comm -12` empty) |
| 5 | Logs isolation | ✅ separate `…/w-<id>/logs` per worker |
| 6 | `#171` guard | ✅ bare `install worker` (no `--worker-id`) → `install_worker_missing_id`, exit 2, message points at the Web Console Add Worker command |

### Captured output (abridged)

```
[alpha]   prefix:    .../workers/w-alpha
[alpha]   service:   com.agent-center.worker.w-alpha (launchd)
[beta]    prefix:    .../workers/w-beta
[beta]    service:   com.agent-center.worker.w-beta (launchd)

1. PASS: w-alpha file set identical before/after w-beta install (no clobber)
2. ~/.agent-center/workers/w-alpha/  ~/.agent-center/workers/w-beta/
3. com.agent-center.worker.w-alpha   (refs --worker-id=w-alpha, workers/w-alpha)
   com.agent-center.worker.w-beta    (refs --worker-id=w-beta,  workers/w-beta)
4. alpha sqlite_path .../w-alpha/var/worker.db ; beta .../w-beta/var/worker.db ; SHARED: (none)
5. ~/.agent-center/workers/w-alpha/logs  ~/.agent-center/workers/w-beta/logs
6. Error: install_worker_missing_id: --worker-id is required (no hostname default)... exit=2
```

## Conclusion

v2.7 multi-worker-per-machine is **independent and collision-free** on the
real-binary install path, and the `#171` worker-id-required guard is
effective. Suitable as the §5 Worker deployed-smoke evidence for release.
