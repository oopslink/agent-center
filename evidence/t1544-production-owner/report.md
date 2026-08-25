# T1544 Production-Isolated Plan Owner Verification

- Task: `task-31b7cd50` (T1544)
- Production baseline: `origin/main@3cba9b306bb9a11cad7a2d1b1a0140f1f64df5cc`
- Evidence branch: `evidence/t1544-production-owner`
- Isolated fixture plan: `plan-04fd1e00`
- G0: `generation-eb4dabf6`
- Fixture source task: `task-c338d336` (T1547)
- Safety: disposable control-plane records only; no product/source changes.

## Acceptance checklist

- [x] Production deployment provenance supplied by upstream T1543.
- [x] Isolated plan and worker fixture created through production MCP.
- [x] G0 node dispatched and authoritative plan readback captured.
- [x] Worker block reaches authoritative `blocked/paused` state.
- [ ] Owner attention/wake evidence is delivered (no directed unread event observed).
- [ ] Non-owner mutation is rejected.
- [ ] Owner evolution atomically supersedes original and creates continuation with lineage, assignee, explicit contract, dependency, and acceptance description (blocked by production `plan_conflict`).
- [ ] Active generation/effective frontier excludes historical superseded node.
- [ ] Continuation dispatch reaches a real terminal execution state or explicit rejection.
- [ ] UI screenshots plus console/network evidence captured.

## Observations

1. `create_plan` returned `plan-04fd1e00`; immediate `get_plan` exposes `creator_ref=agent:agent-d819c80f` but no explicit `owner_ref` field. This is retained as an open contract observation pending UI/API cross-check.
2. `start_plan` returned `ok=true`; authoritative readback showed `active_generation_id=generation-eb4dabf6`, source node `task-c338d336`, `effective=true`, and `task_status/node_status=running/running`.
3. Worker performed the requested block. Authoritative `get_plan` then showed `task_status=blocked`, `node_status=paused`; plan conversation message `01M0XGM0D55XX3VRZC5JNQ0NRS` records exact reason `T1544_ISOLATED_BLOCK`.
4. Immediately after the block, `get_my_unread` returned an empty list. No Owner-directed Block Event / attention / wake notification was observable through the supported MCP channel.
5. Owner evolution attempt `t1544-prod-isolated-replace-v1` tried to supersede the blocked node and atomically add a continuation with `follows_task_id`, explicit `evidence_only`, `supervisor_inline`, assignee, and a seq edge. Production rejected it with `plan_conflict: plan node is in-flight (dispatched/running/terminal) — its structure cannot be live-edited`.
6. The source task was then explicitly terminalized with `discard_task`; authoritative `get_plan` showed `task_status=discarded`, `node_status=failed`, `active_failures=[task-c338d336]`, while the old executor-liveness frontier projection remained visible.
7. Owner evolution retry `t1544-prod-isolated-replace-v2`, now against the formally discarded terminal node, was rejected with the identical `plan_conflict`. Therefore no new generation, continuation, or dispatch was created.

## Verdict

**BLOCK / FAIL.** Production cannot complete the required Plan Owner Replace/evolution path through the supported MCP API. The failure reproduces both while the source is blocked/paused and after it is formally discarded. In addition, no directed Owner wake/attention message was observable, and the plan retains a stale executor-liveness frontier after discard. UI screenshot/console/network gates were not pursued after the authoritative API contract failed; acceptance must remain blocked rather than manufacturing partial PASS evidence.

## Provenance

- Supervisor-inline fork attempt for T1544 was accepted as command `01M0XGFC97VQY4085TEVAQGGXR`, then terminally rejected with `error_kind=supervisor_inline`; it is not counted as an execution.
- T1544 was started directly and read back as `running`.
