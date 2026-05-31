package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
)

// mapDomainError translates BC sentinel errors into HTTP status + envelope
// codes. Parallel to webconsole/api.mapDomainError but covers every BC the
// CLI surface touches (workforce, conversation, discussion, taskruntime,
// secretmgmt, cognition, identity).
//
// The switch lists sentinels in BC order (Conversation → Identity →
// Workforce → TaskRuntime → SecretManagement → Discussion → Cognition).
// Anything that doesn't match falls through to 500 internal — callers can
// still see the underlying error text in the message field.
func mapDomainError(w http.ResponseWriter, err error) {
	switch {
	// ---- not_found (404) -------------------------------------------------
	case errors.Is(err, conversation.ErrConversationNotFound),
		errors.Is(err, conversation.ErrMessageNotFound),
		errors.Is(err, workforce.ErrAgentInstanceNotFound),
		errors.Is(err, workforce.ErrWorkerNotFound),
		errors.Is(err, workforce.ErrProjectNotFound),
		errors.Is(err, workforce.ErrProposalNotFound),
		errors.Is(err, workforce.ErrMappingNotFound),
		errors.Is(err, workforce.ErrBootstrapTokenNotFound),
		errors.Is(err, task.ErrTaskNotFound),
		errors.Is(err, execution.ErrTaskExecutionNotFound),
		errors.Is(err, execution.ErrArtifactNotFound),
		errors.Is(err, inputrequest.ErrInputRequestNotFound),
		errors.Is(err, secretmgmt.ErrUserSecretNotFound),
		errors.Is(err, discussion.ErrIssueNotFound),
		errors.Is(err, pm.ErrTaskNotFound),
		errors.Is(err, pm.ErrProjectNotFound),
		errors.Is(err, pm.ErrIssueNotFound),
		errors.Is(err, agent.ErrAgentNotFound),
		errors.Is(err, agent.ErrWorkItemNotFound),
		errors.Is(err, admintoken.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())

	// ---- illegal_transition (409) — agent-BC lifecycle/work-item feedback ---
	// D2-c-i controller→center feedback: a rejected AR transition (MarkStopped
	// precondition, WorkItem Activate/Done/Fail move) is a conflict with the
	// current state, surfaced as 409 so the daemon can re-read + retry.
	case errors.Is(err, agent.ErrIllegalLifecycle),
		errors.Is(err, agent.ErrWorkItemIllegalMove),
		errors.Is(err, agent.ErrWorkItemBadStatus):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error())

	// ---- already_exists (409) -------------------------------------------
	case errors.Is(err, conversation.ErrConversationAlreadyExists),
		errors.Is(err, convservice.ErrParticipantAlreadyActive),
		errors.Is(err, workforce.ErrWorkerAlreadyExists),
		errors.Is(err, workforce.ErrProjectAlreadyExists),
		errors.Is(err, workforce.ErrAgentInstanceNameTaken),
		errors.Is(err, workforce.ErrMappingAlreadyActive),
		errors.Is(err, secretmgmt.ErrUserSecretAlreadyExists),
		errors.Is(err, secretmgmt.ErrUserSecretNameTaken),
		errors.Is(err, discussion.ErrIssueAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", err.Error())

	// ---- version_conflict (409) -----------------------------------------
	case errors.Is(err, conversation.ErrConversationVersionConflict),
		errors.Is(err, conversation.ErrReadStateVersionConflict),
		errors.Is(err, workforce.ErrWorkerVersionConflict),
		errors.Is(err, workforce.ErrProjectVersionConflict),
		errors.Is(err, workforce.ErrAgentInstanceVersionConflict),
		errors.Is(err, secretmgmt.ErrUserSecretVersionConflict),
		errors.Is(err, discussion.ErrIssueVersionConflict),
		errors.Is(err, pm.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())

	// ---- forbidden / terminal (403) -------------------------------------
	case errors.Is(err, conversation.ErrConversationArchived),
		errors.Is(err, conversation.ErrConversationClosed),
		errors.Is(err, workforce.ErrAgentInstanceArchived),
		errors.Is(err, discussion.ErrIssueWithdrawn),
		errors.Is(err, discussion.ErrIssueAlreadyConcluded),
		errors.Is(err, secretmgmt.ErrUserSecretRevoked),
		errors.Is(err, execution.ErrTaskExecutionAlreadyTerminated),
		errors.Is(err, pmservice.ErrNotMember),
		errors.Is(err, pm.ErrCrossProject):
		writeError(w, http.StatusForbidden, "terminal", err.Error())

	// ---- invalid_transition (422) ---------------------------------------
	case errors.Is(err, conversation.ErrConversationInvalidKind),
		errors.Is(err, conversation.ErrMessageInvalidSender),
		errors.Is(err, conversation.ErrReadStateMessageNotInConversation),
		errors.Is(err, execution.ErrInvalidTransition),
		errors.Is(err, execution.ErrSingleActiveViolation),
		errors.Is(err, discussion.ErrIssueInvalidTransition),
		errors.Is(err, discussion.ErrIssueNoConversationBound),
		errors.Is(err, discussion.ErrInvalidOrigin),
		errors.Is(err, discussion.ErrResolutionInvalid),
		errors.Is(err, workforce.ErrProposalAlreadyTerminated),
		errors.Is(err, workforce.ErrProjectHasActiveDeps),
		errors.Is(err, convservice.ErrParticipantNotActive),
		errors.Is(err, convservice.ErrParticipantNotOwner),
		errors.Is(err, pm.ErrIllegalTransition),
		errors.Is(err, pm.ErrInvalidStatus),
		errors.Is(err, pm.ErrSelfVerify):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())

	// ---- bad_request (400) ----------------------------------------------
	case errors.Is(err, secretmgmt.ErrMasterKeyNotLoaded),
		errors.Is(err, trservice.ErrNoInputChannel),
		errors.Is(err, trservice.ErrProjectNotFound),
		errors.Is(err, disservice.ErrProjectNotFound),
		errors.Is(err, pm.ErrBlockReasonRequired):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())

	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
