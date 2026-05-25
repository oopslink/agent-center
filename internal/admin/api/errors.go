package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/conversation"
	convidentity "github.com/oopslink/agent-center/internal/conversation/identity"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
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
		errors.Is(err, convidentity.ErrIdentityNotFound),
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
		errors.Is(err, cognition.ErrInvocationNotFound),
		errors.Is(err, cognition.ErrDecisionNotFound),
		errors.Is(err, admintoken.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())

	// ---- already_exists (409) -------------------------------------------
	case errors.Is(err, conversation.ErrConversationAlreadyExists),
		errors.Is(err, convservice.ErrParticipantAlreadyActive),
		errors.Is(err, convidentity.ErrIdentityAlreadyExists),
		errors.Is(err, workforce.ErrWorkerAlreadyExists),
		errors.Is(err, workforce.ErrProjectAlreadyExists),
		errors.Is(err, workforce.ErrAgentInstanceNameTaken),
		errors.Is(err, workforce.ErrMappingAlreadyActive),
		errors.Is(err, secretmgmt.ErrUserSecretAlreadyExists),
		errors.Is(err, secretmgmt.ErrUserSecretNameTaken),
		errors.Is(err, discussion.ErrIssueAlreadyExists),
		errors.Is(err, cognition.ErrScopeKeyRunningExists),
		errors.Is(err, cognition.ErrDecisionImmutable):
		writeError(w, http.StatusConflict, "already_exists", err.Error())

	// ---- version_conflict (409) -----------------------------------------
	case errors.Is(err, conversation.ErrConversationVersionConflict),
		errors.Is(err, conversation.ErrReadStateVersionConflict),
		errors.Is(err, convidentity.ErrIdentityVersionConflict),
		errors.Is(err, workforce.ErrWorkerVersionConflict),
		errors.Is(err, workforce.ErrProjectVersionConflict),
		errors.Is(err, workforce.ErrAgentInstanceVersionConflict),
		errors.Is(err, secretmgmt.ErrUserSecretVersionConflict),
		errors.Is(err, discussion.ErrIssueVersionConflict),
		errors.Is(err, cognition.ErrInvocationVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", err.Error())

	// ---- forbidden / terminal (403) -------------------------------------
	case errors.Is(err, conversation.ErrConversationArchived),
		errors.Is(err, conversation.ErrConversationClosed),
		errors.Is(err, workforce.ErrAgentInstanceArchived),
		errors.Is(err, discussion.ErrIssueWithdrawn),
		errors.Is(err, discussion.ErrIssueAlreadyConcluded),
		errors.Is(err, secretmgmt.ErrUserSecretRevoked),
		errors.Is(err, execution.ErrTaskExecutionAlreadyTerminated):
		writeError(w, http.StatusForbidden, "terminal", err.Error())

	// ---- invalid_transition (422) ---------------------------------------
	case errors.Is(err, conversation.ErrConversationInvalidKind),
		errors.Is(err, conversation.ErrMessageInvalidSender),
		errors.Is(err, conversation.ErrReadStateMessageNotInConversation),
		errors.Is(err, convidentity.ErrIdentityInvalidKind),
		errors.Is(err, convidentity.ErrIdentityKindImmutable),
		errors.Is(err, execution.ErrInvalidTransition),
		errors.Is(err, execution.ErrSingleActiveViolation),
		errors.Is(err, discussion.ErrIssueInvalidTransition),
		errors.Is(err, discussion.ErrIssueNoConversationBound),
		errors.Is(err, discussion.ErrInvalidOrigin),
		errors.Is(err, discussion.ErrResolutionInvalid),
		errors.Is(err, workforce.ErrProposalAlreadyTerminated),
		errors.Is(err, workforce.ErrProjectHasActiveDeps),
		errors.Is(err, cognition.ErrInvalidStatusTransition),
		errors.Is(err, cognition.ErrInvocationAlreadyTerminal),
		errors.Is(err, convservice.ErrParticipantNotActive),
		errors.Is(err, convservice.ErrParticipantNotOwner),
		errors.Is(err, convservice.ErrDerivationSourceNotActive),
		errors.Is(err, convservice.ErrDerivationCallerNotParticipant):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())

	// ---- bad_request (400) ----------------------------------------------
	case errors.Is(err, cognition.ErrRationaleRequired),
		errors.Is(err, cognition.ErrInvocationIDRequired),
		errors.Is(err, secretmgmt.ErrMasterKeyNotLoaded),
		errors.Is(err, trservice.ErrNoInputChannel),
		errors.Is(err, trservice.ErrProjectNotFound),
		errors.Is(err, disservice.ErrProjectNotFound):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())

	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
