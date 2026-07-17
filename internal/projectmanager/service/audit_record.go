package service

import (
	"context"
	"encoding/json"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// audit_record.go holds the typed thin wrappers over s.recordChange (design §5) that
// the semantic write points call紧挨 s.emit. Keeping the AuditEntry construction here
// keeps each write-point edit to a single line and centralizes the object-level
// change-ledger vocabulary. All are best-effort / nil-safe via recordChange (审计写
// 不阻塞主 mutation).

// auditDetail marshals a small map to the JSON detail blob; a marshal error or an
// empty map yields "" (recordChange/repo normalize "" → "{}").
func auditDetail(kv map[string]any) string {
	if len(kv) == 0 {
		return ""
	}
	b, err := json.Marshal(kv)
	if err != nil {
		return ""
	}
	return string(b)
}

// auditTaskStatusChange records a task status transition — ONLY when the status
// actually moved (prev != now), so a no-op SetStatus / a tags-only edit产生 nothing
// (只读不产 / no-op不产).
func (s *Service) auditTaskStatusChange(ctx context.Context, t *pm.Task, prev pm.TaskStatus, actor pm.IdentityRef) {
	if prev == t.Status() {
		return
	}
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: pm.AuditTaskStatusChanged,
		Field:      "status",
		FromValue:  string(prev),
		ToValue:    string(t.Status()),
		ActorRef:   actor,
	})
}

// auditTaskBlocked records a block as a human-facing status change running→blocked
// (mechanically "blocked" is a running-task annotation, not a status enum value, but
// the ledger presents it as a state — design §4.2). reasonType/reason ride detail.
// ADR-0054: prevStatus is passed in rather than hardcoded to `running`. Block can now
// also park a `delivered` task, and the audit trail is the human-facing record of what
// actually happened — a delivered→blocked park logged as "running→blocked" would put a
// transition that never occurred into the one place people go to reconstruct the truth.
func (s *Service) auditTaskBlocked(ctx context.Context, t *pm.Task, prevStatus pm.TaskStatus, reasonType pm.BlockReasonType, reason string, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: pm.AuditTaskStatusChanged,
		Field:      "status",
		FromValue:  string(prevStatus),
		ToValue:    string(pm.TaskBlocked),
		ActorRef:   actor,
		Detail:     auditDetail(map[string]any{"reason_type": string(reasonType), "reason": reason}),
	})
}

// auditTaskDelivered records a delivery as the human-facing status change
// running→delivered, carrying the summary the assignee handed to the acceptance (I107 ①).
func (s *Service) auditTaskDelivered(ctx context.Context, t *pm.Task, summary string, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: pm.AuditTaskStatusChanged,
		Field:      "status",
		FromValue:  string(pm.TaskRunning),
		ToValue:    string(pm.TaskDelivered),
		ActorRef:   actor,
		Detail:     auditDetail(map[string]any{"summary": summary}),
	})
}

// auditTaskRework records an acceptance REJECT as the status change delivered→running.
func (s *Service) auditTaskRework(ctx context.Context, t *pm.Task, comment string, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: pm.AuditTaskStatusChanged,
		Field:      "status",
		FromValue:  string(pm.TaskDelivered),
		ToValue:    string(pm.TaskRunning),
		ActorRef:   actor,
		Detail:     auditDetail(map[string]any{"comment": comment}),
	})
}

// auditTaskUnblocked records an unblock as the human-facing status change
// blocked→running.
func (s *Service) auditTaskUnblocked(ctx context.Context, t *pm.Task, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: pm.AuditTaskStatusChanged,
		Field:      "status",
		FromValue:  "blocked",
		ToValue:    string(pm.TaskRunning),
		ActorRef:   actor,
	})
}

// auditTaskAssign records an assign / reassign / unassign. prevAssignee is the
// assignee BEFORE the change (empty ⇒ was unassigned).
func (s *Service) auditTaskAssign(ctx context.Context, t *pm.Task, changeType pm.AuditChangeType, prevAssignee pm.IdentityRef, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: changeType,
		Field:      "assignee",
		FromValue:  string(prevAssignee),
		ToValue:    string(t.Assignee()),
		ActorRef:   actor,
	})
}

// auditTaskCreated records a task's creation.
func (s *Service) auditTaskCreated(ctx context.Context, t *pm.Task, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  t.ProjectID(),
		ObjectType: pm.AuditObjectTask,
		ObjectID:   string(t.ID()),
		ChangeType: pm.AuditTaskCreated,
		ToValue:    string(t.Status()),
		ActorRef:   actor,
		Detail:     auditDetail(map[string]any{"title": t.Title()}),
	})
}

// --- issue -----------------------------------------------------------------

func (s *Service) auditIssueStatusChange(ctx context.Context, i *pm.Issue, prev pm.IssueStatus, changeType pm.AuditChangeType, actor pm.IdentityRef) {
	if prev == i.Status() {
		return
	}
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  i.ProjectID(),
		ObjectType: pm.AuditObjectIssue,
		ObjectID:   string(i.ID()),
		ChangeType: changeType,
		Field:      "status",
		FromValue:  string(prev),
		ToValue:    string(i.Status()),
		ActorRef:   actor,
	})
}

func (s *Service) auditIssueCreated(ctx context.Context, i *pm.Issue, actor pm.IdentityRef) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  i.ProjectID(),
		ObjectType: pm.AuditObjectIssue,
		ObjectID:   string(i.ID()),
		ChangeType: pm.AuditIssueCreated,
		ToValue:    string(i.Status()),
		ActorRef:   actor,
		Detail:     auditDetail(map[string]any{"title": i.Title()}),
	})
}

// auditIssueMetadataEdited records a coarse title/description edit — NO full-text
// diff (design §2/§9). fields lists which of {title,description} changed.
func (s *Service) auditIssueMetadataEdited(ctx context.Context, i *pm.Issue, fields []string, actor pm.IdentityRef) {
	if len(fields) == 0 {
		return
	}
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  i.ProjectID(),
		ObjectType: pm.AuditObjectIssue,
		ObjectID:   string(i.ID()),
		ChangeType: pm.AuditIssueMetadataEdited,
		ActorRef:   actor,
		Detail:     auditDetail(map[string]any{"fields": fields}),
	})
}

// --- plan ------------------------------------------------------------------

func (s *Service) auditPlan(ctx context.Context, p *pm.Plan, changeType pm.AuditChangeType, actor pm.IdentityRef, detail map[string]any) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  p.ProjectID(),
		ObjectType: pm.AuditObjectPlan,
		ObjectID:   string(p.ID()),
		ChangeType: changeType,
		ActorRef:   actor,
		Detail:     auditDetail(detail),
	})
}

// auditPlanByID records a plan change when only the ids are in scope (a controlflow
// hook that does not hold the Plan aggregate). projectID may be empty when unknown
// at the call site — the ledger row still keys on the plan object_id.
func (s *Service) auditPlanByID(ctx context.Context, projectID pm.ProjectID, planID pm.PlanID, changeType pm.AuditChangeType, actor pm.IdentityRef, detail map[string]any) {
	s.recordChange(ctx, pm.AuditEntry{
		ProjectID:  projectID,
		ObjectType: pm.AuditObjectPlan,
		ObjectID:   string(planID),
		ChangeType: changeType,
		ActorRef:   actor,
		Detail:     auditDetail(detail),
	})
}
