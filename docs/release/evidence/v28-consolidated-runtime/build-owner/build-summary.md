# Consolidated runtime — build-owner summary (Dev2)

v2.8 100% @ trunk **ae4697a** (= v2.7.1). On-demand test instance (no persistent v2.8 runtime; reproducible via `install test-instance`). Instance ran on oopslink@home; Tester + Tester2 direct-connected (same machine).

## Build
- `make build` off `ae4697a` → `cd web && pnpm install --frozen-lockfile` (pulls react-markdown ^10 + remark-gfm ^4) + vite build → `internal/webconsole/spa/dist/` + `go build` backend + fakeagent. All green.
- `./bin/agent-center install test-instance --with-agent --workers 2 --output json` → instance `t1` / org `acme-t1` @ `http://127.0.0.1:50265`, 2 workers, 1 agent + 1 dispatched task. Healthy (web HTTP 200).

## Reproduce
```
git worktree add /tmp/wt-runtime ae4697a && cd /tmp/wt-runtime
make build
./bin/agent-center install test-instance --with-agent --workers 2 --output json
```

## Seed executed (one spin, via the real center HTTP API — signin Owner t1)
- **>50-events attempt (#188)**: dispatched 9 extra tasks to agent-6c0ef378. fakeagent (deterministic test-double) plateaued at ~27 events (4 tasks produced 15/6/5/1, rest 0; agent busy-but-not-emitting). Event-type variety complete (tool_use/tool_result/system/system_init/rate_limit/thinking/assistant_text/result).
- **#185 (archived) chip**: created ArchivedBot (agent-a1fa32b6) → assigned to task-bbd1e611 → archived (lifecycle=archived, worker_id cleared per #272).
- **#190 dangling-binding**: created DanglingBot (agent-906b52f4) on worker-b5cbc1d4 (Tester deletes that worker to exercise dangling). worker-16e42ca7 (agent-6c0ef378) kept for #273 WorkerDetail Bound Agents.
- **#182 P1 badges**: added Peer Bob (human) → Bob posted `@Owner t1 ...` in channel-7b996912 → owner mention_count++; owner followed the channel.
- **#192 picker**: owner + Peer Bob multi-human + channels (filter options).

## Build-owner run-real (contract verification, my features)
- **#274 activity contract**: `GET /api/agents/{id}/activity?limit=50` → `{activity, next_cursor}` exactly matches useInfiniteQuery (next_cursor=null at ≤50 = correct terminal). No mock-drift.
- **#182**: channel-7b996912 unread=1 / mention=1 / followed=True (after projection) — verified.
- **#185**: backend DTO `task.assignee.assignee_lifecycle='archived'` (NESTED); FE OrgWorkItemsView.tsx:112 reads `it.assignee.assignee_lifecycle` (nested, correct) → chip renders. (Self-retracted an initial flat-field false-alarm.)

## #188 disposition
Cursor mechanism verified at 3 layers: code (PR #188 part-1 TDD), data/API (Tester PR #186/#188 + consolidated limit=10 → 3-page chain re-group no gap/dup), run-real (this build, contract match). The >50-event Load-older UI trigger is **fakeagent test-double-bounded**, not a production limitation (a real claude agent querying a large codebase naturally produces >50 events). PD-accepted disposition.

## Cross-refs
- Tester data/API evidence: `../data-api/runtime-data-api-evidence.txt` (5/5 PASS).
- Tester2 §3.3/§4.3 UI evidence: `../ui-a11y/` (mostly PASS + 2 #275 runtime findings → Dev PR #193 fix: Esc dismissed-trigger + id hover-only; my §5.3 LGTM).
