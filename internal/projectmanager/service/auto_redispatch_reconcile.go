package service

import (
	"context"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// AutoRedispatch tunables. Cap 3 = three automatic re-dispatch attempts per stuck
// episode before the auto path gives up and leaves the task for manual recovery
// (unblock_task); a small cap because each attempt is one full stale cycle apart
// (a re-dispatched item that the agent never picks up goes stale again only after
// WorkItemReconcileDefaultStaleAge ≈ 30 min), so 3 attempts already span ~1.5h of
// an agent failing to come back. Tick 1 min = the cadence the sweep runs at; a
// returned agent is re-dispatched to within (tick) of becoming available.
const (
	AutoRedispatchDefaultMaxAttempts = 3
	AutoRedispatchDefaultTick        = 1 * time.Minute
)

// RedispatchEligibilityPort is the agent-BC seam the reconciler depends on (DDD BC
// boundary, conventions §0): the ProjectManager owns "which tasks are stuck" but
// must NOT reach into agent repositories to learn "is the assignee back + free".
// The agent BC supplies an adapter (agent/service.RedispatchEligibility) that
// satisfies this interface structurally; the cli wiring layer composes the two.
type RedispatchEligibilityPort interface {
	// Eligible reports whether the stuck task taskRef ("pm://tasks/<id>") assigned
	// to agentRef ("agent:<id>") should be auto-redispatched now (assignee back
	// online + no live WorkItem already in flight). A non-agent / unresolved
	// assignee → (false, nil). Errors are infrastructure-only (logged, never fatal).
	Eligible(ctx context.Context, agentRef, taskRef string) (bool, error)
}

// AutoRedispatchReconciler is the v2.9.1 P1 follow-up to the P0 manual recovery
// (task-aab863b3): it AUTOMATES re-dispatch of nodes whose active WorkItem was
// released by the WorkItemReconciler stale sweep (FailFromAgentDeath) — which
// leaves the Task `running` with a blocked_reason="agent execution failed"
// stuck-annotation (ADR-0046) and NO live WorkItem. The P0 added the manual
// unblock_task entry point; this reconciler periodically re-dispatches such tasks
// the moment their assignee agent is available again, capped at MaxAttempts per
// episode — after which it backs off and leaves the task stuck for manual recovery
// (auto优先, 手动兜底). Successful re-activation clears the annotation via the
// existing TaskStatusSyncProjector A① path, which ends the episode and resets the
// per-task attempt counter.
//
// Attempt counting is in-memory and EPISODE-scoped: the map is keyed by TaskID and
// pruned every tick for tasks no longer stuck (recovered), so a fresh stuck episode
// always gets a fresh budget. In-memory is sufficient for the cap's purpose —
// bounding a tight release→redispatch→release loop within a running process; a
// process restart re-resumes in-flight work via boot-reconcile and a fresh budget
// after an operator-initiated restart is acceptable (documented tradeoff).
type AutoRedispatchReconciler struct {
	svc         *Service
	gate        RedispatchEligibilityPort
	clk         clock.Clock
	maxAttempts int
	tick        time.Duration
	log         func(string, ...any)

	attempts  map[pm.TaskID]int  // episode attempt counter (pruned on recovery)
	exhausted map[pm.TaskID]bool // cap-exhausted-logged latch (one event per episode)
}

// NewAutoRedispatchReconciler wires the reconciler. Zero maxAttempts/tick → package
// defaults; nil clk → system clock; nil log → no-op. gate is required (a nil gate
// makes every task ineligible — the reconciler then never redispatches, which is a
// safe no-op rather than a panic).
func NewAutoRedispatchReconciler(
	svc *Service,
	gate RedispatchEligibilityPort,
	clk clock.Clock,
	maxAttempts int,
	tick time.Duration,
	log func(string, ...any),
) *AutoRedispatchReconciler {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if maxAttempts <= 0 {
		maxAttempts = AutoRedispatchDefaultMaxAttempts
	}
	if tick <= 0 {
		tick = AutoRedispatchDefaultTick
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &AutoRedispatchReconciler{
		svc:         svc,
		gate:        gate,
		clk:         clk,
		maxAttempts: maxAttempts,
		tick:        tick,
		log:         log,
		attempts:    map[pm.TaskID]int{},
		exhausted:   map[pm.TaskID]bool{},
	}
}

// Run blocks until ctx is done, calling Tick on the configured cadence. A failed
// tick is logged, never fatal.
func (r *AutoRedispatchReconciler) Run(ctx context.Context) error {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if n, err := r.Tick(ctx); err != nil {
				r.log("auto-redispatch: tick failed: %v", err)
			} else if n > 0 {
				r.log("auto-redispatch: re-dispatched %d stuck task(s)", n)
			}
		}
	}
}

// Tick scans for stuck-released tasks and re-dispatches the eligible ones (assignee
// back + under the attempt cap). Returns the count re-dispatched. Exposed so tests
// can step the reconciler deterministically. Per-task errors are logged, not fatal.
func (r *AutoRedispatchReconciler) Tick(ctx context.Context) (int, error) {
	stuck, err := r.svc.listStuckReleasedTasks(ctx)
	if err != nil {
		return 0, err
	}
	// Prune attempt/exhausted state for tasks that are no longer stuck (recovered,
	// discarded, completed, reassigned-away) so a future stuck episode starts fresh.
	stuckNow := make(map[pm.TaskID]struct{}, len(stuck))
	for _, t := range stuck {
		stuckNow[t.ID()] = struct{}{}
	}
	for id := range r.attempts {
		if _, ok := stuckNow[id]; !ok {
			delete(r.attempts, id)
			delete(r.exhausted, id)
		}
	}

	redispatched := 0
	for _, t := range stuck {
		id := t.ID()
		if r.attempts[id] >= r.maxAttempts {
			r.markExhausted(ctx, t) // back off → manual recovery (idempotent latch)
			continue
		}
		eligible, err := r.gate.Eligible(ctx, string(t.Assignee()), "pm://tasks/"+string(id))
		if err != nil {
			r.log("auto-redispatch: eligibility check task=%s: %v", id, err)
			continue
		}
		if !eligible {
			continue // assignee not back yet, or a dispatch is already in flight
		}
		attempt := r.attempts[id] + 1
		if err := r.svc.autoRedispatchStuckTask(ctx, id, attempt); err != nil {
			r.log("auto-redispatch: re-dispatch task=%s: %v", id, err)
			continue
		}
		r.attempts[id] = attempt
		redispatched++
		r.log("auto-redispatch: re-dispatched stuck task=%s assignee=%s (attempt %d/%d)",
			id, t.Assignee(), attempt, r.maxAttempts)
	}
	return redispatched, nil
}

// markExhausted emits the one-shot cap-exhausted audit event for a task and latches
// it so the event fires at most once per stuck episode (the latch is cleared by the
// Tick prune when the episode ends).
func (r *AutoRedispatchReconciler) markExhausted(ctx context.Context, t *pm.Task) {
	if r.exhausted[t.ID()] {
		return
	}
	r.exhausted[t.ID()] = true
	if err := r.svc.emitAutoRedispatchExhausted(ctx, t, r.maxAttempts); err != nil {
		r.log("auto-redispatch: emit exhausted task=%s: %v", t.ID(), err)
	}
	r.log("auto-redispatch: task=%s hit retry cap (%d) — left stuck for manual recovery",
		t.ID(), r.maxAttempts)
}

// listStuckReleasedTasks returns the stuck-RELEASED tasks across all projects: a
// task is `running` carrying the blocked_reason stamped by the failure projector
// when its WorkItem was released (ADR-0046 stuck-annotation), AND assigned to an
// agent. Other blocked_reasons (a human/agent-authored block_task note) are NOT
// auto-redispatched — only the system stale-release reason is.
func (s *Service) listStuckReleasedTasks(ctx context.Context) ([]*pm.Task, error) {
	running, err := s.tasks.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskRunning})
	if err != nil {
		return nil, err
	}
	out := make([]*pm.Task, 0, len(running))
	for _, t := range running {
		if t.IsArchived() {
			continue // archived tasks are read-only
		}
		if t.BlockedReason() != taskBlockedOnFailureReason {
			continue // not a stale-release stuck task
		}
		if !strings.HasPrefix(string(t.Assignee()), "agent:") {
			continue // only agent assignees are auto-redispatched
		}
		out = append(out, t)
	}
	return out, nil
}

// autoRedispatchStuckTask is the SYSTEM re-dispatch path (no project-member check,
// unlike UnblockTask). It re-emits pm.task.assigned — which the WorkItemProjector
// turns into a fresh queued WorkItem for the assignee — WITHOUT clearing the
// blocked_reason: the stuck annotation is left intact so the task stays visibly
// stuck until a real WorkItem ACTIVATION clears it (the TaskStatusSyncProjector A①
// path). This keeps the stuck signal honest if the re-dispatch is never picked up,
// and avoids a false "recovered" flash. Re-loads the task in-tx and re-verifies it
// is still stuck (a concurrent recovery/discard makes this a no-op — idempotent).
func (s *Service) autoRedispatchStuckTask(ctx context.Context, taskID pm.TaskID, attempt int) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		// Re-verify still stuck under the tx (the gate decision was outside it).
		if t.IsArchived() || t.Status() != pm.TaskRunning || t.BlockedReason() != taskBlockedOnFailureReason {
			return nil
		}
		// Functional re-dispatch: WorkItemProjector mints a fresh WorkItem for the
		// (unchanged) assignee. Task row is NOT mutated (no version bump).
		if err := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); err != nil {
			return err
		}
		// Audit marker distinguishing the auto path from a manual unblock_task.
		return s.emit(txCtx, EvtTaskAutoRedispatched,
			refsJSON(map[string]string{"task_id": string(taskID), "project_id": string(t.ProjectID())}),
			autoRedispatchPayload{
				TaskID: string(taskID), ProjectID: string(t.ProjectID()),
				Assignee: string(t.Assignee()), Attempt: attempt,
			})
	})
}

// emitAutoRedispatchExhausted emits the one-shot cap-exhausted audit event.
func (s *Service) emitAutoRedispatchExhausted(ctx context.Context, t *pm.Task, maxAttempts int) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		return s.emit(txCtx, EvtTaskAutoRedispatchExhausted,
			refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
			autoRedispatchPayload{
				TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
				Assignee: string(t.Assignee()), Attempt: maxAttempts,
			})
	})
}

// autoRedispatchPayload is the audit-event body for the auto-redispatch events.
type autoRedispatchPayload struct {
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
	Assignee  string `json:"assignee"`
	Attempt   int    `json:"attempt"`
}
