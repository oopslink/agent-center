package projectmanager

import "time"

// AuditObjectType names the kind of domain object an AuditEntry records a change
// for. The generic pm_audit_log table (0099) keys entries by (ObjectType, ObjectID)
// so issue / task / plan share one ledger, one write path, and one read API — see
// docs/design/features/2026-07-03-change-log-audit-design.md §2.
type AuditObjectType string

const (
	AuditObjectIssue AuditObjectType = "issue"
	AuditObjectTask  AuditObjectType = "task"
	AuditObjectPlan  AuditObjectType = "plan"
)

// AuditChangeType is the semantic change enum (design §4.2). Only HIGH-VALUE
// semantic changes are recorded — state transitions, ownership, dependencies,
// gate/decision outcomes, close/reopen — NOT every field diff (title/description
// edits collapse to a coarse metadata_edited, no full-text diff).
type AuditChangeType string

const (
	// task
	AuditTaskCreated       AuditChangeType = "created"
	AuditTaskStatusChanged AuditChangeType = "status_changed"
	AuditTaskAssigned      AuditChangeType = "assigned"
	AuditTaskReassigned    AuditChangeType = "reassigned"
	AuditTaskUnassigned    AuditChangeType = "unassigned"
	AuditTaskClaimed       AuditChangeType = "claimed"
	AuditTaskAutoAssigned  AuditChangeType = "auto_assigned"
	AuditTaskReviewVerdict AuditChangeType = "review_verdict"

	// plan
	AuditPlanCreated         AuditChangeType = "created"
	AuditPlanStarted         AuditChangeType = "started"
	AuditPlanStopped         AuditChangeType = "stopped"
	AuditPlanDependencyAdded AuditChangeType = "dependency_added"
	AuditPlanDependencyRemvd AuditChangeType = "dependency_removed"
	AuditPlanNodeAdded       AuditChangeType = "node_added"
	AuditPlanNodeRemoved     AuditChangeType = "node_removed"
	AuditPlanDecisionOutcome AuditChangeType = "decision_outcome"
	AuditPlanLoopback        AuditChangeType = "loopback"
	// AuditPlanTopologyCommit is one whole edit_plan_topology batch (2026-07-05
	// live-topology design §5 layer 2): the from_version→to_version commit plus the
	// ops/diff, recorded ONCE per commit (not per-op) so a plan's edit history is
	// reconstructable/replayable from the audit ledger.
	AuditPlanTopologyCommit AuditChangeType = "topology_commit"

	// issue
	AuditIssueCreated        AuditChangeType = "created"
	AuditIssueStatusChanged  AuditChangeType = "status_changed"
	AuditIssueMetadataEdited AuditChangeType = "metadata_edited"
	AuditIssueAutoClosed     AuditChangeType = "auto_closed"

	// cognition reminder (event-driven). Recorded on the TRIGGERING pm object's
	// ledger (the plan/task/issue whose state change is watched) so the entity's
	// change-log shows that an on_event reminder was armed by, then fired from, its
	// transition (reminder-event feature). Actor is a system reconciler ref.
	AuditReminderArmed AuditChangeType = "reminder_armed"
	AuditReminderFired AuditChangeType = "reminder_fired"
)

// ActorSystem prefixes an actor ref for a SYSTEM-driven change (auto-assign,
// auto-close, loopback, lease-reclaim): `system:<reconciler>` — never blank, never
// misattributed to the object owner (design §5). Use SystemActor to build one.
const ActorSystemPrefix = "system:"

// SystemActor builds the actor ref for a system reconciler-driven change.
func SystemActor(reconciler string) IdentityRef {
	return IdentityRef(ActorSystemPrefix + reconciler)
}

// AuditEntry is one immutable, append-only row of the object-level change ledger
// (design §4). It is a plain value: the persistence layer mints the ULID id on
// insert (repo via idgen), keeping this domain type free of an infra id
// dependency — mirroring TaskActionLog.
//
// Field/FromValue/ToValue carry the "X→Y" diff when the change is a single-field
// transition (status/assignee/...); they are empty for changes better described by
// Detail (a JSON blob of structured extras: dependency kind/when, gate round, note).
// The human-readable sentence is composed on the FRONTEND from these structured
// fields — the backend stores structure, not prose.
type AuditEntry struct {
	ID         string
	ProjectID  ProjectID
	ObjectType AuditObjectType
	ObjectID   string
	ChangeType AuditChangeType
	Field      string
	FromValue  string
	ToValue    string
	ActorRef   IdentityRef
	// Detail is a JSON object ('{}' when empty) with structured extras. Never nil
	// on the wire (the repo defaults blank → '{}').
	Detail     string
	OccurredAt time.Time
}
