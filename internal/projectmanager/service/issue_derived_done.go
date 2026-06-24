package service

import (
	"context"
	"errors"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// issue_derived_done.go (T464 — issue-41aceddb) — the "issue's derived tasks all
// concluded → nudge the owner to review + close" trigger.
//
// completeTaskHandler → CompleteTask (and DiscardTask / SetTaskStatus) only propagate
// into the plan DAG; they never touch the SOURCE issue, so an issue owner gets no
// signal when the executable work it spawned is all done and has to keep asking. This
// hook closes that gap WITHOUT any auto-close: when a task carrying a
// derived_from_issue link enters a terminal state AND that makes ALL of the issue's
// derived tasks terminal, it emits EvtIssueDerivedTasksDone so the WakeProjector
// @-nudges + (for an agent owner) wakes the owner to self-review and close_issue.
//
// TRIGGER-ONLY (oopslink, explicit): the issue's status is NEVER changed here — close
// is owner-only via the close_issue MCP tool. No auto-close, no project-level switch.
//
// Idempotency without a persistent latch: the "all-terminal ON THIS transition"
// condition fires exactly once per "fill" of the derived-task set — only the LAST
// non-terminal task's conclusion satisfies it, so the other conclusions are silent.
// It re-arms naturally: a NEW derived task added later is non-terminal, so the set is
// no longer all-terminal until that task too concludes, which re-fires once. Event
// redelivery is absorbed by the WakeProjector's AppliedStore (one wake per event).

// maybeNotifyIssueDerivedTasksDone runs inside taskStateOp's tx after a task
// transition. It is a no-op unless: the task carries a derived_from_issue link; THIS
// transition moved it from non-terminal → terminal (not a terminal→terminal re-set);
// the issue is still actionable (not resolved/closed/discarded); and every derived
// task of that issue is now terminal. When all hold, it emits EvtIssueDerivedTasksDone.
func (s *Service) maybeNotifyIssueDerivedTasksDone(ctx context.Context, t *pm.Task, prevStatus pm.TaskStatus) error {
	issueID := t.DerivedFromIssue()
	if issueID == "" {
		return nil // not derived from any issue
	}
	// Only the transition INTO terminal matters — a terminal→terminal re-set (e.g.
	// completed→discarded) does not change whether the set is "all concluded".
	if prevStatus.IsTerminal() || !t.Status().IsTerminal() {
		return nil
	}
	i, err := s.issues.FindByID(ctx, issueID)
	if err != nil {
		if errors.Is(err, pm.ErrIssueNotFound) {
			return nil // dangling link → nothing to nudge
		}
		return err
	}
	// A "please review and close" nudge only makes sense while the issue is still open
	// work. An already-concluded issue (resolved/closed/discarded) is a no-op.
	switch i.Status() {
	case pm.IssueResolved, pm.IssueClosed, pm.IssueDiscarded:
		return nil
	}
	// Are ALL of the issue's derived tasks terminal now? (Same set as the
	// list_tasks_of_issue read: project tasks filtered by derived_from_issue.)
	all, err := s.tasks.ListByProject(ctx, i.ProjectID())
	if err != nil {
		return err
	}
	var total, completed, discarded int
	for _, dt := range all {
		if dt.DerivedFromIssue() != issueID {
			continue
		}
		switch dt.Status() {
		case pm.TaskCompleted:
			total++
			completed++
		case pm.TaskDiscarded:
			total++
			discarded++
		default:
			return nil // a still-in-flight derived task → not all concluded yet
		}
	}
	if total == 0 {
		return nil // defensive: t is derived from issueID, so total >= 1
	}
	return s.emit(ctx, EvtIssueDerivedTasksDone,
		refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
		issueDerivedTasksDonePayload{
			IssueID:       string(i.ID()),
			ProjectID:     string(i.ProjectID()),
			OwnerRef:      "pm://issues/" + string(i.ID()),
			OwnerIdentity: string(i.CreatedBy()),
			Total:         total,
			Completed:     completed,
			Discarded:     discarded,
		})
}
