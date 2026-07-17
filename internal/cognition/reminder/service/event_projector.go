package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// pm outbox event-type strings this projector consumes. Mirrored from
// internal/projectmanager/service (pm.<entity>.<action>) as LOCAL constants so the
// Cognition BC does not import the ProjectManager service package — the projector
// only needs the coarse event-type + a narrow slice of each payload. Kept in sync
// with the producer by the wiring parity test (every emitted type has a consumer).
const (
	evtTaskStateChanged  = "pm.task.state_changed"
	evtIssueStateChanged = "pm.issue.state_changed"
	evtPlanCompleted     = "pm.plan.completed"
	evtPlanStopped       = "pm.plan.stopped"
	evtPlanFailed        = "pm.plan.failed"
)

// ReminderAuditEntry is the audit record written when an on_event reminder is armed
// by, or fires from, a pm entity's state change. It is keyed on the TRIGGERING pm
// object (plan/task/issue) so the entity's change-log shows the reminder activity.
// The cli adapter maps this to a pm.AuditEntry (change-log ledger, v2.29/v2.35).
type ReminderAuditEntry struct {
	ProjectID  string // the reminder's project (== the entity's project, enforced by the projector)
	ObjectType string // "plan" | "task" | "issue"
	ObjectID   string // the watched entity id
	ChangeType string // "reminder_armed" | "reminder_fired"
	ReminderID string
	Event      string // the watched transition
	Detail     map[string]any
	OccurredAt time.Time
}

// ReminderAuditSink writes a reminder audit entry into the change-log ledger. It is
// OPTIONAL (nil ⇒ no-op) and best-effort at the call site, mirroring the pm
// recordChange contract: an audit failure must never abort the arming/firing.
type ReminderAuditSink interface {
	AppendReminderAudit(ctx context.Context, e ReminderAuditEntry) error
}

// ReminderEventProjector is the outbox projector that ARMS event-driven reminders
// (reminder-event feature). On a matching pm entity state-change it finds every
// DORMANT on_event reminder watching that (entity_type, entity_id, event) IN THE
// SAME PROJECT and arms it (next_run_at = event_time + delay); the existing
// ReminderScheduler then fires it once and completes it (one-shot). Arming +
// AppliedStore.MarkApplied commit in ONE tx (the same-tx idempotency pattern the
// Relay requires), and FindArmedByEvent excludes already-armed reminders, so a
// redelivered or repeated event never re-arms.
type ReminderEventProjector struct {
	db      *sql.DB
	repo    reminder.Repository
	applied outbox.AppliedStore
	clk     clock.Clock
	audit   ReminderAuditSink // optional
}

// NewReminderEventProjector constructs the projector. audit may be nil.
func NewReminderEventProjector(db *sql.DB, repo reminder.Repository, applied outbox.AppliedStore, clk clock.Clock, audit ReminderAuditSink) *ReminderEventProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ReminderEventProjector{db: db, repo: repo, applied: applied, clk: clk, audit: audit}
}

// Name is the AppliedStore key (stable projector identifier).
func (p *ReminderEventProjector) Name() string { return "cognition.reminder.on_event" }

// eventPayload is the narrow slice of the pm task/issue/plan event payloads this
// projector reads: the entity id (one of the three id fields is set per type) plus
// the transition discriminators (status/prev_status/reason for tasks; status for
// issues) and the project id (for the same-project permission gate).
type eventPayload struct {
	TaskID     string `json:"task_id"`
	IssueID    string `json:"issue_id"`
	PlanID     string `json:"plan_id"`
	ProjectID  string `json:"project_id"`
	Status     string `json:"status"`
	PrevStatus string `json:"prev_status"`
	Reason     string `json:"reason"`
}

// Project applies one event: derive the (entity_type, entity_id, event) triple,
// then arm every dormant matching reminder in the same project. Non-matching events
// (or transitions outside the on_event vocabulary) are a no-op.
func (p *ReminderEventProjector) Project(ctx context.Context, e outbox.Event) error {
	entityType, entityID, event, ok := p.classify(e)
	if !ok {
		return nil // not a reminder-arming transition
	}
	var pl eventPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	eventAt := e.CreatedAt // arm relative to WHEN the transition happened (durable)
	now := p.clk.Now()

	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		// Same-tx idempotency: a redelivered event is a true no-op.
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		armed, err := p.repo.FindArmedByEvent(txCtx, entityType, entityID, event)
		if err != nil {
			return err
		}
		for _, r := range armed {
			// Permission gate: only arm a reminder for an event in its OWN project.
			// The reminder's project is the remindee's project (fixed at creation under
			// the cross-project guard); watching an entity in a DIFFERENT project must
			// not fire (no cross-project timing leak). Skip when project_id is unknown
			// on the event (old/plan events may omit org but always carry project_id).
			if strings.TrimSpace(pl.ProjectID) != "" && r.ProjectID() != pl.ProjectID {
				continue
			}
			if err := r.Arm(eventAt, now); err != nil {
				// A concurrently-cancelled/terminal reminder is skipped (never fatal).
				continue
			}
			if err := p.repo.Update(txCtx, r); err != nil {
				return err
			}
			p.writeAudit(txCtx, r, entityType, entityID, event, now)
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// classify derives the reminder on_event triple (entity_type, entity_id, event)
// from a pm outbox event. ok=false when the event/transition is not in the on_event
// vocabulary (plan: completed/failed/stopped; task: completed/blocked/reopened/
// discarded; issue: closed/reopened).
func (p *ReminderEventProjector) classify(e outbox.Event) (reminder.EntityType, string, string, bool) {
	var pl eventPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return "", "", "", false
	}
	switch e.EventType {
	case evtTaskStateChanged:
		event := taskEvent(pl.Status, pl.Reason)
		if event == "" || strings.TrimSpace(pl.TaskID) == "" {
			return "", "", "", false
		}
		return reminder.EntityTask, pl.TaskID, event, true
	case evtIssueStateChanged:
		event := issueEvent(pl.Status)
		if event == "" || strings.TrimSpace(pl.IssueID) == "" {
			return "", "", "", false
		}
		return reminder.EntityIssue, pl.IssueID, event, true
	case evtPlanCompleted, evtPlanStopped, evtPlanFailed:
		if strings.TrimSpace(pl.PlanID) == "" {
			return "", "", "", false
		}
		return reminder.EntityPlan, pl.PlanID, planEvent(e.EventType), true
	default:
		return "", "", "", false
	}
}

// taskEvent maps a pm.task.state_changed payload to the on_event vocabulary.
//
// ADR-0054: a block now moves the status to "blocked", so that maps directly — WITHOUT
// this arm every on_event=blocked reminder would silently stop firing, since a blocked
// task no longer arrives here as "running". The old running+reason detection is KEPT as a
// second arm for LEGACY events (pre-ADR-0054 rows/replays where a block left the status
// running); it cannot double-fire, because Block clears neither field in a way that lets
// a single payload match both arms.
//
// `delivered` deliberately maps to NOTHING: the on_event vocabulary is about outcomes
// people subscribe to, and a delivery is not one — it is a hand-off awaiting a verdict.
// Firing "completed" on it would be the same false green ADR-0054 exists to abolish, and
// inventing a "delivered" event is a separate product decision, not this change's call.
func taskEvent(status, reason string) string {
	switch status {
	case "completed":
		return "completed"
	case "discarded":
		return "discarded"
	case "reopened":
		return "reopened"
	case "blocked":
		return "blocked"
	case "running":
		if strings.TrimSpace(reason) != "" {
			return "blocked" // legacy ADR-0046 shape: parked as a running+reason annotation
		}
	}
	return ""
}

// issueEvent maps a pm.issue.state_changed payload status to the on_event vocabulary.
func issueEvent(status string) string {
	switch status {
	case "closed":
		return "closed"
	case "reopened":
		return "reopened"
	}
	return ""
}

// planEvent maps a plan lifecycle event type to the on_event vocabulary.
func planEvent(eventType string) string {
	switch eventType {
	case evtPlanCompleted:
		return "completed"
	case evtPlanStopped:
		return "stopped"
	case evtPlanFailed:
		return "failed"
	}
	return ""
}

// writeAudit records the reminder_armed change on the triggering entity's ledger
// (best-effort; a nil sink or an audit error never aborts the arming).
func (p *ReminderEventProjector) writeAudit(ctx context.Context, r *reminder.Reminder, entityType reminder.EntityType, entityID, event string, now time.Time) {
	if p.audit == nil {
		return
	}
	delaySeconds := int64(0)
	if oe := r.OnEvent(); oe != nil {
		delaySeconds = int64(oe.Delay / time.Second)
	}
	_ = p.audit.AppendReminderAudit(ctx, ReminderAuditEntry{
		ProjectID:  r.ProjectID(),
		ObjectType: string(entityType),
		ObjectID:   entityID,
		ChangeType: string(auditReminderArmed),
		ReminderID: r.ID().String(),
		Event:      event,
		Detail: map[string]any{
			"reminder_id":       r.ID().String(),
			"remindee_agent_id": r.RemindeeAgentID(),
			"event":             event,
			"delay_seconds":     delaySeconds,
		},
		OccurredAt: now,
	})
}

// audit change-type strings (mirrored from pm.AuditReminderArmed / .AuditReminderFired
// — the cli adapter maps them to the pm ChangeType enum).
const (
	auditReminderArmed = "reminder_armed"
	auditReminderFired = "reminder_fired"
)

var _ outbox.Projector = (*ReminderEventProjector)(nil)
