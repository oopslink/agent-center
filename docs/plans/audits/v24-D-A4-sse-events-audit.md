# v2.4-D-A4 SSE event types — scope clarification + deferral

> Closes slock task #38. v0 scope downsized after auditing the existing
> event-emission surface — only one of the three originally-proposed
> event types needs new backend work, and that one already ships.
> Documented here per § 0.6 layer discipline: state what IS, not what
> was supposedly planned to be.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。Frontend Modal (F3 #43) needs SSE signal when a worker enrolls so it can transition State 1 (Ready) → State 3 (Success). |
| 保留 BC invariants？ | N/A — no code change in this ST. |
| 没省 transport？ | N/A. |
| Mock-as-default 消除？ | N/A. |
| 起点 = "previously didn't emit"？ | 否（per § 0.6）。`workforce.worker.enrolled` event已存在 (`internal/workforce/service/enroll.go:83-95` since v2.0)。本 ST 是 audit + 决定不重复落地。 |

## § 1. 三个事件类型的逐一审计

### 1.1 `worker.enrolled` → **already exists**

`internal/workforce/service/enroll.go:83-95` emits event type `workforce.worker.enrolled` on successful enroll inside the same tx as the worker repo save. Payload contains `worker_id` + `capabilities`. EventRefs.WorkerID is set. v2.0 EventSink → SSE bus auto-fanout (per task #48) wires this through to subscribers.

**F3 (task #43) action**: subscribe to event type prefix `workforce.worker.enrolled` (or include in the worker.* prefix subscription). No backend change needed.

### 1.2 `worker.enroll_attempt_failed` → **deferred, frontend has equivalent path**

Originally proposed for Modal State 4/5 (token-used / token-expired). Backend implementation would require: extending AuthMiddleware to emit observability events on auth rejections, plus a per-token rejection sink wired through observability.EventSink (same pattern as v2.3-7c admin.rate_limit_hit).

**Why deferred**:
- Modal can derive the state purely from frontend: countdown timer for token-expired (knows `expires_at` at mint time); HTTP response code on worker daemon's own enroll POST attempt for "your install command failed" feedback (worker prints to stderr, operator sees it locally).
- Backend doesn't need to gossip the failure over SSE because the Modal on the operator's screen is the one watching for success; failure visibility is local to the worker process.
- The "another machine consumed your token" case (Modal State 4) IS unique-to-backend and warrants the event for a future iteration. Documented as a v2.4-followup item.

**v2.4 ship-blocker?** No. F3 wires the Success path via the existing `workforce.worker.enrolled`; failure paths are timer-based + worker-side stderr.

### 1.3 `admintoken.expired` → **deferred, frontend handles via countdown**

Originally proposed for Modal State 5 (token expired, "Generate new"). Backend implementation would require a periodic sweep job or per-verify-rejection emission (the latter only triggers when SOMEONE tries to use the expired token — i.e. too late for the Modal to react).

**Why deferred**:
- Frontend knows `expires_at` from the mint response and runs a 1-minute-resolution countdown locally. When it hits 0 (or the user clicks "Generate new"), the Modal transitions to State 5 directly. No backend SSE needed.
- A backend sweep job to emit at the exact expiry moment is over-engineering for a single-user UX timer.

**v2.4 ship-blocker?** No.

## § 2. v2.4 ship surface after this audit

| Event | Status | Used by |
|---|---|---|
| `workforce.worker.enrolled` | **shipped v2.0** | F3 Modal State 3 + Fleet row highlight |
| `worker.enroll_attempt_failed` | **deferred** (frontend timer + worker stderr fill the gap) | F3 Modal — uses timer instead |
| `admintoken.expired` | **deferred** (frontend countdown fills the gap) | F3 Modal — uses countdown instead |

## § 3. Verification

| Gate | Result |
|---|---|
| `go test ./...` | unchanged from prior commit; A4 introduces no code |
| `make lint` | unchanged |
| `make smoke` | unchanged |

## § 4. Deviation

vs PD's original ST description "新 SSE 事件: worker.enrolled / worker.enroll_attempt_failed / admintoken.expired 三类型 emit + bus auto-fanout"：only 1 of 3 ships. The other 2 are explicitly deferred with frontend-side substitution path documented.

PD acceptance question: is the "frontend timer + worker stderr" path acceptable for v2.4 ship, OR does the v2.4-D-F2 Modal need the backend events? If the latter, A4 reopens as a ~3-4h ST adding an observability sink to the AuthMiddleware + admintoken service (mirror of v2.3-7c RateLimitSink pattern).

## § 5. § 0.6 layer discipline

- **Observation**: `workforce.worker.enrolled` event已 ship; no `worker.enroll_attempt_failed` or `admintoken.expired` event types exist
- **Capability**: frontend can derive equivalent UX from local state (countdown + worker-side feedback) for the F3 Modal scope
- **Not claimed**: "we never intended to emit these" — only "they would require ~3-4h to wire and the UX can ship without them"

## § 6. Follow-up

If PD acceptance flags the missing backend events as ship-blocking → reopen A4 as ~3-4h work mirroring v2.3-7c RateLimitSink pattern. Otherwise this ST stays closed.
