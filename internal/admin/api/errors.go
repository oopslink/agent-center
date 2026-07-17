package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	"github.com/oopslink/agent-center/internal/workforce"
)

// mapDomainError translates BC sentinel errors into HTTP status + envelope
// codes. Parallel to webconsole/api.mapDomainError but covers every BC the
// CLI surface touches (workforce, conversation, projectmanager, agent,
// secretmgmt, identity).
//
// Anything that doesn't match falls through to 500 internal — callers can
// still see the underlying error text in the message field.
func mapDomainError(w http.ResponseWriter, err error) {
	switch {
	// ---- not_found (404) -------------------------------------------------
	case errors.Is(err, conversation.ErrConversationNotFound),
		errors.Is(err, conversation.ErrMessageNotFound),
		// 引用 (quote): a missing/cross-conversation quoted target reads as not-found
		// at the edge (existence non-disclosure, §5.7).
		errors.Is(err, conversation.ErrMessageInvalidQuote),
		errors.Is(err, workforce.ErrWorkerNotFound),
		errors.Is(err, workforce.ErrBootstrapTokenNotFound),
		errors.Is(err, secretmgmt.ErrUserSecretNotFound),
		errors.Is(err, pm.ErrTaskNotFound),
		errors.Is(err, pm.ErrProjectNotFound),
		errors.Is(err, pm.ErrIssueNotFound),
		errors.Is(err, pm.ErrCodeRepoRefNotFound),
		errors.Is(err, agent.ErrAgentNotFound),
		errors.Is(err, admintoken.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())

	// ---- illegal_transition (409) — agent-BC lifecycle feedback ---
	// D2-c-i controller→center feedback: a rejected AR transition (MarkStopped
	// precondition) is a conflict with the current state, surfaced as 409 so the
	// daemon can re-read + retry.
	// v2.14.0 F7 (issue I14): the WorkItem move sentinels (ErrWorkItemIllegalMove /
	// ErrWorkItemBadStatus) were removed — AgentWorkItem retired.
	// ---- reset_requires_stopped (409) — v2.16 W5 (design §3.1) — a Reset
	// issued while the agent is not settled (running/stopping/resetting). The
	// operator must stop it first; distinct code so the surface can show a precise
	// "stop the agent before resetting" message instead of a generic conflict.
	case errors.Is(err, agent.ErrResetRequiresStopped):
		writeError(w, http.StatusConflict, "reset_requires_stopped", err.Error())

	case errors.Is(err, agent.ErrIllegalLifecycle):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error())

	// ---- agent_busy (409) — v2.8.1 #278 single-active: an activate (push
	// report-active or pull start_task) lost to the single-active UNIQUE index /
	// pre-check; the agent already has an in-flight work item. Benign conflict
	// (not a 500); the work item stays queued. ----
	case errors.Is(err, agent.ErrAgentHasActiveWork),
		// v2.14.0 I14/F5 (§13.B single-active on Task): start_task lost to the
		// idx_tasks_one_active_per_agent UNIQUE index — the agent already has a
		// running, non-blocked task. Benign conflict; the task stays open.
		errors.Is(err, pm.ErrAgentHasActiveTask):
		writeError(w, http.StatusConflict, "agent_busy", err.Error())

	// ---- task_not_runnable (409) — v2.14.0 I14/F5 §13.A run-ahead gate: start_task
	// on a task whose blockedBy dependencies are not yet satisfied (or a backlog /
	// not-dispatched pool member). The task stays open; it becomes startable once its
	// upstream completes (or it is added to a plan / dispatched into the pool). ----
	case errors.Is(err, pm.ErrTaskNotRunnable):
		writeError(w, http.StatusConflict, "task_not_runnable", err.Error())

	// ---- task_blocked (409) — v2.14.0 I14/F5 §2.5: heartbeat on a blocked task. A
	// blocked task is a lease-free legal pause, so there is no lease to renew. ----
	case errors.Is(err, pm.ErrTaskBlocked):
		writeError(w, http.StatusConflict, "task_blocked", err.Error())

	// ---- task_parked (409) — ADR-0054: a start/dispatch op on a PARKED task
	// (delivered/blocked). Non-terminal but nothing is in flight, so starting it would
	// fork a fresh empty-context executor onto work already handed over or deliberately
	// paused. Distinct from task_blocked (a lease op on a legal pause) and from
	// invalid_transition (a nonsense move): the move is refused because the task must be
	// un-parked through its own door first — unblock_task, or the acceptance verdict
	// (complete_task / rework_task). ----
	case errors.Is(err, pm.ErrTaskParked):
		writeError(w, http.StatusConflict, "task_parked", err.Error())

	// ---- task_description_frozen (409) — I109 ①: a description edit on a RUNNING task.
	// The in-flight executor's prompt was rendered from the description at spawn and is
	// never re-fed, so accepting the write would change what the task SAYS while the
	// executor keeps working from the old text — the caller would believe it re-scoped a
	// run it did not. Refused with the reason in the body (NOT a 500: this is a
	// well-formed request against a state that cannot honor it, and it must not be
	// retried blindly). Re-scope via the judge gate, or discard + re-dispatch. ----
	case errors.Is(err, pm.ErrTaskDescriptionFrozen):
		writeError(w, http.StatusConflict, "task_description_frozen", err.Error())

	// ---- lease_still_live (409) — T862 reset_task mis-fire guard: reset_task on a task
	// whose execution lease has NOT yet lapsed. A live lease means the agent may be alive
	// and must be NUDGED (续租 + @-owner), never reset. The caller must wait for the lease
	// to lapse (or the runtime's tier-3 confirmation) before retrying. ----
	case errors.Is(err, pm.ErrLeaseStillLive):
		writeError(w, http.StatusConflict, "lease_still_live", err.Error())

	// v2.14.0 F7 (issue I14): the work_item_reassigned (409) mapping for
	// agent.ErrWorkItemReassigned was removed — AgentWorkItem retired (the agent
	// optimistic-lock race now lives on the Task model via pm version conflicts).

	// ---- already_exists (409) -------------------------------------------
	case errors.Is(err, conversation.ErrConversationAlreadyExists),
		errors.Is(err, convservice.ErrParticipantAlreadyActive),
		errors.Is(err, workforce.ErrWorkerAlreadyExists),
		errors.Is(err, secretmgmt.ErrUserSecretAlreadyExists),
		errors.Is(err, secretmgmt.ErrUserSecretNameTaken):
		writeError(w, http.StatusConflict, "already_exists", err.Error())

	// ---- version_conflict (409) -----------------------------------------
	case errors.Is(err, conversation.ErrConversationVersionConflict),
		errors.Is(err, conversation.ErrReadStateVersionConflict),
		errors.Is(err, workforce.ErrWorkerVersionConflict),
		errors.Is(err, secretmgmt.ErrUserSecretVersionConflict),
		errors.Is(err, pm.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())

	// ---- project_archived (409) — v2.9 #297 -----------------------------
	case errors.Is(err, pm.ErrProjectArchived):
		// archived project is read-only (irreversible, @oopslink) — every project-child
		// mutation rejects 409, cross-surface (mirrors webconsole mapPMError + the
		// plan_conflict class in mapPlanToolError).
		writeError(w, http.StatusConflict, "project_archived", err.Error())

	// ---- task_backlog_not_actionable (409) — T190 unified backlog-inert error.
	// A backlog task (planID=="") is inert: not claimable / startable / status-
	// changeable until added to a plan or dispatched into the pool. The agent tools
	// detect backlog up front (rejectIfBacklog / writeBacklogNotActionable); this
	// case keeps the SAME code for any domain path that returns the sentinel. The
	// message is BacklogNotActionableHint (no "projectmanager:" prefix), surfaced
	// verbatim to agents. ----
	case errors.Is(err, pm.ErrTaskBacklogNotActionable):
		writeError(w, http.StatusConflict, "task_backlog_not_actionable", err.Error())

	// ---- forbidden / terminal (403) -------------------------------------
	case errors.Is(err, conversation.ErrConversationArchived),
		errors.Is(err, conversation.ErrConversationClosed),
		errors.Is(err, secretmgmt.ErrUserSecretRevoked),
		errors.Is(err, pmservice.ErrNotMember),
		errors.Is(err, pm.ErrCrossProject),
		// v2.14.0 I14/F5: heartbeat/own-task ops by a non-assignee agent.
		errors.Is(err, pm.ErrNotTaskAssignee):
		writeError(w, http.StatusForbidden, "terminal", err.Error())

	// ---- invalid_transition (422) ---------------------------------------
	case errors.Is(err, conversation.ErrConversationInvalidKind),
		errors.Is(err, conversation.ErrMessageInvalidSender),
		errors.Is(err, conversation.ErrReadStateMessageNotInConversation),
		errors.Is(err, convservice.ErrParticipantNotActive),
		errors.Is(err, convservice.ErrParticipantNotOwner),
		errors.Is(err, pm.ErrIllegalTransition),
		errors.Is(err, pm.ErrInvalidStatus):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())

	// ---- derived_issue_project_mismatch (409) — T192: a task may only be derived
	// from an Issue in its OWN project. A scope conflict, surfaced as 409. ----
	case errors.Is(err, pm.ErrDerivedIssueProjectMismatch):
		writeError(w, http.StatusConflict, "derived_issue_project_mismatch", err.Error())

	// ---- bad_request (400) ----------------------------------------------
	case errors.Is(err, secretmgmt.ErrMasterKeyNotLoaded),
		errors.Is(err, pm.ErrBlockReasonRequired),
		errors.Is(err, pm.ErrInvalidBlockReasonType),
		// I105: create_task with an unknown dispatch_mode — rejected loudly rather
		// than silently coerced, so a typo'd mark can never masquerade as a fork.
		errors.Is(err, pm.ErrInvalidDispatchMode),
		// ADR-0054: deliver_task without a summary — an unexplained delivery cannot be
		// judged by the acceptance it is waiting on.
		errors.Is(err, pm.ErrDeliverySummaryRequired),
		// issue-4a45e9cc: a reported installed-skill with an unknown layer.
		errors.Is(err, agent.ErrInvalidSkillLayer):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())

	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
