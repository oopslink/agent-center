package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/outbox"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// auto_assign_trigger_projector.go (v2.18.3 BE-2, issue-577a7b0e) — the EVENT-driven
// half of the auto-assign dual-track trigger. The periodic AutoAssignReconciler is the
// completeness backstop (~25s); this projector is the FAST PATH: the instant a task
// enters the pool or an agent frees a run slot, it runs a project-scoped auto-assign
// sweep so an ownerless pool task is picked up in ~relay-latency rather than waiting
// for the next periodic tick.
//
// It consumes the events that change the matchable set:
//   - pm.task.state_changed — covers BOTH triggers: a task moving to `open` in the pool
//     (a fresh dispatch / reopen → newly matchable), AND a task reaching a terminal /
//     blocked state (its assignee just freed a run slot → that agent may now take pool
//     work). Either way the project's pool is worth re-sweeping.
//   - pm.task.created — a task created directly into the pool.
//
// The scope is the event's project (parsed from the refs), so each trigger sweeps ONLY
// that project's builtin pool — cheap to fire per event. The sweep itself is idempotent
// + CAS-safe (ClaimIfUnassigned), so a redelivered event or an overlap with the periodic
// sweep / a concurrent claim_task never double-assigns.
//
// BEST-EFFORT: a sweep error is logged-and-swallowed (return nil) so the event is marked
// applied and not retried forever — the periodic backstop covers any miss. This trigger
// is an optimisation, never the correctness guarantee.
//
// LOOP-SAFE: an auto-assign emits pm.task.assigned (+ pm.task.auto_assigned), NOT
// pm.task.state_changed / pm.task.created, so the assign never re-enters this projector.
type AutoAssignTriggerProjector struct {
	svc *Service
	log func(string, ...any)
}

// NewAutoAssignTriggerProjector wires the projector over the pm Service. nil log → no-op.
func NewAutoAssignTriggerProjector(svc *Service, log func(string, ...any)) *AutoAssignTriggerProjector {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &AutoAssignTriggerProjector{svc: svc, log: log}
}

// Name is the AppliedStore key (distinct per projector on the shared relay).
func (p *AutoAssignTriggerProjector) Name() string { return "pm-auto-assign-trigger" }

// Project fires a project-scoped auto-assign sweep on a relevant event. Irrelevant
// event types and events with no resolvable project are a no-op. The relay records the
// trailing MarkApplied on a nil return, so a swallowed sweep error (logged) still marks
// the event done — the periodic backstop is the safety net.
func (p *AutoAssignTriggerProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case EvtTaskStateChanged, EvtTaskCreated:
		// relevant — handled below
	default:
		return nil
	}
	projectID := projectIDFromRefs(e.Refs)
	if projectID == "" {
		return nil // no project locus → nothing to scope a sweep to
	}
	if _, err := p.svc.TriggerAutoAssignForProject(ctx, projectID); err != nil {
		// Best-effort: log + swallow so the event is marked applied; the periodic sweep
		// covers the miss. Returning the error would retry the event forever on a
		// persistent fault.
		p.log("auto-assign-trigger: scoped sweep failed (project %s): %v", projectID, err)
	}
	return nil
}

// projectIDFromRefs extracts project_id from an outbox event's refs JSON (every pm task
// event sets refs = {"task_id","project_id"} via refsJSON). Returns "" when absent /
// unparseable.
func projectIDFromRefs(refs string) pm.ProjectID {
	if strings.TrimSpace(refs) == "" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(refs), &m); err != nil {
		return ""
	}
	return pm.ProjectID(m["project_id"])
}

var _ outbox.Projector = (*AutoAssignTriggerProjector)(nil)
