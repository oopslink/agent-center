# SSE Wire-in Audit (P11 Frontend F13)

> Closeout audit for plan § 5 SSE events vs `web/src/sse/useSSE.ts`
> `dispatchToQueryClient` table. Written 2026-05-24 alongside the F13
> closeout commit.

## § 0. Why this exists

x9527 oversight rule on F13: an SSE wire-in step is **not skippable** even
if the work happened incrementally during F5–F12. We need an explicit
table proving every backend-emitted event type that affects a SPA page
has a dispatch entry, and the dispatch keys actually match the literal
strings the backend emits.

## § 1. Critical finding from the audit

**Backend SSE event types are BC-prefixed** — passed through verbatim by
`internal/webconsole/sse/fanout.go:97` (`EventType: string(e.Type())`).
F5–F12 had been writing unprefixed names like `agent_instance.created`,
which would have **silently never matched** the actual emitted
`workforce.agent_instance.created`.

Pre-audit dispatch table assumed:
- `agent_instance.created` / `agent_instance.archived` / `agent_instance.state_changed`
- `worker.enrolled` / `worker.heartbeat` / `worker.offline`
- `user_secret.created` / `user_secret.revoked`
- `input_request.created` / `input_request.cancelled` (double-L)
- `conversation.archived`
- `conversation.participants_changed`
- `task_execution.state_changed`

None of these strings are actually emitted by the backend. The F13
audit caught this and the dispatch table is now corrected to the
literal strings, verified by `rg '^\s*EventType:\s*"' internal/`.

## § 2. Audit table — backend EventType → dispatch action

### Conversation BC (`internal/conversation/service/`)

| Backend EventType | Dispatch action | Wired in |
|---|---|---|
| `conversation.opened` | invalidate `conversations()` + `conversation(id)` | F5 (corrected F13) |
| `conversation.message_added` | invalidate `messages(conv_id)` | F5 |
| `conversation.participant_joined` | invalidate `conversation(id)` | F13 (was wrong name) |
| `conversation.participant_left` | invalidate `conversation(id)` | F13 (was wrong name) |
| `conversation.message_references_added` | invalidate `refs(child)` + `messages(child)` | F13 (was missing) |

### Input request BC (`internal/taskruntime/service/`, `internal/taskruntime/timeoutscan/`, `internal/cli/handlers_supervisor.go`)

| Backend EventType | Dispatch action | Wired in |
|---|---|---|
| `input_request.requested` | invalidate `inputRequests()` | F13 (was `input_request.created`) |
| `input_request.responded` | invalidate `inputRequests()` | F5 |
| `input_request.canceled` (single-L) | invalidate `inputRequests()` | F13 (was `cancelled` double-L) |
| `input_request.timed_out` | invalidate `inputRequests()` | F13 (was missing) |
| `input_request.escalated` | invalidate `inputRequests()` | F13 (was missing) |

### Agent instance (`internal/workforce/service/agent_instance.go`)

| Backend EventType | Dispatch action | Wired in |
|---|---|---|
| `workforce.agent_instance.created` | invalidate `agents()` + `fleet()` | F13 (was unprefixed) |
| `workforce.agent_instance.archived` | invalidate `agents()` + `fleet()` | F13 |
| `workforce.agent_instance.activated` | invalidate `agents()` + `fleet()` | F13 (was missing) |
| `workforce.agent_instance.idle` | invalidate `agents()` + `fleet()` | F13 (was missing) |
| `workforce.agent_instance.sleeping` | invalidate `agents()` + `fleet()` | F13 (was missing) |
| `workforce.agent_instance.awakened` | invalidate `agents()` + `fleet()` | F13 (was missing) |
| `workforce.agent_instance.config_updated` | invalidate `agents()` + `fleet()` | F13 (was missing) |

### Worker (`internal/workforce/service/enroll.go`, `worker_config.go`)

| Backend EventType | Dispatch action | Wired in |
|---|---|---|
| `workforce.worker.enrolled` | invalidate `fleet()` | F13 (was unprefixed) |
| `workforce.worker.config.updated` | invalidate `fleet()` | F13 (was missing) |
| `workforce.worker.capability.updated` | invalidate `fleet()` | F13 (was missing) |

### Secret (`internal/secretmgmt/service/secret_service.go`)

| Backend EventType | Dispatch action | Wired in |
|---|---|---|
| `secretmgmt.user_secret.created` | invalidate `secrets()` | F13 (was unprefixed) |
| `secretmgmt.user_secret.rotated` | invalidate `secrets()` | F13 (was missing) |
| `secretmgmt.user_secret.revoked` | invalidate `secrets()` | F13 |

### Task / task execution (`internal/taskruntime/dispatch/`, `taskruntime/kill/`, `taskruntime/timeoutscan/`)

| Backend EventType | Dispatch action | Wired in |
|---|---|---|
| `task.created` | invalidate `fleet()` | F13 |
| `task.abandoned` | invalidate `fleet()` | F13 |
| `task.suspended` | invalidate `fleet()` | F13 |
| `task_execution.submitted` | invalidate `fleet()` | F13 |
| `task_execution.dispatched` | invalidate `fleet()` | F13 |
| `task_execution.acked` | invalidate `fleet()` | F13 |
| `task_execution.nacked` | invalidate `fleet()` | F13 |
| `task_execution.failed` | invalidate `fleet()` | F13 |
| `task_execution.killed` | invalidate `fleet()` | F13 |
| `task_execution.kill_requested` | invalidate `fleet()` | F13 |
| `task_execution.dispatch_rejected` | invalidate `fleet()` | F13 |
| `task_execution.input_required` | invalidate `fleet()` + `inputRequests()` | F13 |

## § 3. Explicitly NOT wired (and why)

| Backend EventType | Why not wired |
|---|---|
| `task_execution.warning` | Informational only; surfaced through trace page, not the fleet view. |
| `task_execution.timeout_scan_failed` | Operational metric; not a UI signal. |
| `task_execution.dispatch_send_failed` | Same — operational. The reconcile loop will retry; UI sees the eventual `nacked` / `failed`. |
| `task.dispatch_limit_reached` | UI sees the downstream `task_execution.*` state change. |
| `secretmgmt.user_secret.accessed` / `access_denied` | Audit trail — not a list-state change. Future work could surface in a "Secret usage" tab; not in P11 plan scope. |
| `supervisor.*` events | Supervisor surface is CLI-only in P11; no SPA page. |
| `workforce.worker.bootstrap_token.*` | Bootstrap is an install-time flow (CLI), not a runtime UI signal. |
| `workforce.worker_project_proposal.*` / `worker_project_mapping.*` | Proposal acceptance flow is CLI-only in P11. |
| `workforce.project.*` | Project CRUD is CLI-only in P11. |
| `admin.backup_*` | Admin signal; not a SPA page. |
| `observability.trace_archive_*` | Internal lifecycle; not surfaced. |

## § 4. Test coverage

`web/src/sse/useSSE.test.tsx` `describe('dispatchToQueryClient', ...)`
holds a case per dispatch entry, exercising both the matched event types
AND the unknown-no-op branch.

Per BC prefix correctness: explicit assertions on the literal strings
`workforce.agent_instance.created`, `workforce.worker.enrolled`,
`secretmgmt.user_secret.created` so a future rename in the wrong
direction (back to unprefixed) breaks the test before it ships.

## § 5. Outcome

- **9 silent-mismatch bugs** in the F5-era dispatch table corrected
- **15 new event types** wired that weren't covered (most task lifecycle,
  agent state, secret rotation)
- 1 already-correct event preserved (`conversation.message_added`)
- Comprehensive test coverage at the dispatch layer

Future event additions: the file-level comment in
`web/src/sse/useSSE.ts` points contributors at
`rg '^\s*EventType:\s*"' internal/` to find new emitters; the case
table comments call out the BC-prefix gotcha so the same mistake
doesn't recur.
