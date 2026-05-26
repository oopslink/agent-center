package cli

import (
	"errors"
	"io"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
)

// MapDomainError translates a workforce/conversation domain error to a
// (reason, exit-code) pair suitable for PrintError. Returns (false, …) if
// the error is not a known sentinel — caller emits a generic
// `internal_error` line and exits ExitBusinessError.
func MapDomainError(err error) (reason string, code ExitCode, ok bool) {
	switch {
	// Workforce — Worker
	case errors.Is(err, workforce.ErrWorkerNotFound):
		return "worker_not_found", ExitNotFound, true
	case errors.Is(err, workforce.ErrWorkerAlreadyExists):
		return "worker_already_exists", ExitBusinessError, true
	case errors.Is(err, workforce.ErrWorkerVersionConflict):
		return "worker_version_conflict", ExitVersionConflict, true
	case errors.Is(err, workforce.ErrWorkerInvalidStatus):
		return "worker_invalid_status", ExitUsage, true

	// Workforce — Mapping
	case errors.Is(err, workforce.ErrMappingNotFound):
		return "mapping_not_found", ExitNotFound, true
	case errors.Is(err, workforce.ErrMappingAlreadyActive):
		return "mapping_already_active", ExitInvariantViolation, true
	case errors.Is(err, workforce.ErrMappingNotActive):
		return "mapping_not_active", ExitInvalidTransition, true

	// Workforce — Proposal
	case errors.Is(err, workforce.ErrProposalNotFound):
		return "proposal_not_found", ExitNotFound, true
	case errors.Is(err, workforce.ErrProposalAlreadyTerminated):
		return "proposal_already_terminated", ExitInvalidTransition, true
	case errors.Is(err, workforce.ErrProposalInvalidTransition):
		return "proposal_invalid_transition", ExitInvalidTransition, true
	case errors.Is(err, workforce.ErrProposalAlreadyExists):
		return "proposal_already_exists", ExitBusinessError, true
	case errors.Is(err, workforce.ErrProposalVersionConflict):
		return "proposal_version_conflict", ExitVersionConflict, true

	// Workforce — Project
	case errors.Is(err, workforce.ErrProjectNotFound):
		return "project_not_found", ExitNotFound, true
	case errors.Is(err, workforce.ErrProjectAlreadyExists):
		return "project_already_exists", ExitBusinessError, true
	case errors.Is(err, workforce.ErrProjectVersionConflict):
		return "project_version_conflict", ExitVersionConflict, true
	case errors.Is(err, workforce.ErrProjectHasActiveDeps):
		return "project_has_active_deps", ExitInvariantViolation, true
	case errors.Is(err, workforce.ErrProjectInvalidID):
		return "project_invalid_id", ExitUsage, true

	// Conversation
	case errors.Is(err, conversation.ErrConversationNotFound):
		return "conversation_not_found", ExitNotFound, true
	case errors.Is(err, conversation.ErrConversationAlreadyExists):
		return "conversation_already_exists", ExitBusinessError, true
	case errors.Is(err, conversation.ErrConversationClosed):
		return "conversation_closed", ExitBusinessError, true
	case errors.Is(err, conversation.ErrConversationInvalidKind):
		return "conversation_invalid_kind", ExitUsage, true
	case errors.Is(err, conversation.ErrConversationInvalidStatus):
		return "conversation_invalid_status", ExitUsage, true
	case errors.Is(err, conversation.ErrConversationVersionConflict):
		return "conversation_version_conflict", ExitVersionConflict, true

	// Message
	case errors.Is(err, conversation.ErrMessageNotFound):
		return "message_not_found", ExitNotFound, true
	case errors.Is(err, conversation.ErrMessageImmutable):
		return "message_immutable", ExitInvalidTransition, true
	case errors.Is(err, conversation.ErrMessageInvalidSender):
		return "message_invalid_sender", ExitUsage, true
	case errors.Is(err, conversation.ErrConversationArchived):
		return "conversation_archived", ExitInvalidTransition, true

	// TaskRuntime — Task
	case errors.Is(err, task.ErrTaskNotFound):
		return "task_not_found", ExitNotFound, true
	case errors.Is(err, task.ErrTaskAlreadyExists):
		return "task_already_exists", ExitBusinessError, true
	case errors.Is(err, task.ErrTaskInvalidTransition):
		return "task_invalid_transition", ExitInvalidTransition, true
	case errors.Is(err, task.ErrTaskVersionConflict):
		return "task_version_conflict", ExitVersionConflict, true
	case errors.Is(err, task.ErrTaskInvariantViolation):
		return "task_invariant_violation", ExitInvariantViolation, true
	case errors.Is(err, task.ErrCannotUnbindConversation):
		return "task_cannot_unbind_conversation", ExitInvalidTransition, true
	case errors.Is(err, task.ErrInvalidPriority):
		return "task_invalid_priority", ExitUsage, true
	case errors.Is(err, task.ErrInvalidStatus):
		return "task_invalid_status", ExitUsage, true

	// TaskRuntime — TaskExecution
	case errors.Is(err, execution.ErrTaskExecutionNotFound):
		return "execution_not_found", ExitNotFound, true
	case errors.Is(err, execution.ErrTaskExecutionAlreadyTerminated):
		return "execution_already_terminated", ExitInvalidTransition, true
	case errors.Is(err, execution.ErrTaskExecutionVersionConflict):
		return "execution_version_conflict", ExitVersionConflict, true
	case errors.Is(err, execution.ErrSingleActiveViolation):
		return "single_active_violation", ExitInvariantViolation, true
	case errors.Is(err, execution.ErrInvalidTransition):
		return "execution_invalid_transition", ExitInvalidTransition, true
	case errors.Is(err, execution.ErrUnknownReason):
		return "unknown_reason", ExitUsage, true
	case errors.Is(err, execution.ErrUnknownWorkspaceMode):
		return "unknown_workspace_mode", ExitUsage, true
	case errors.Is(err, execution.ErrArtifactNotFound):
		return "artifact_not_found", ExitNotFound, true
	case errors.Is(err, execution.ErrArtifactImmutable):
		return "artifact_immutable", ExitInvalidTransition, true

	// TaskRuntime — InputRequest
	case errors.Is(err, inputrequest.ErrInputRequestNotFound):
		return "input_request_not_found", ExitNotFound, true
	case errors.Is(err, inputrequest.ErrInputRequestAlreadyResolved):
		return "input_request_already_resolved", ExitInvalidTransition, true
	case errors.Is(err, inputrequest.ErrInputRequestVersionConflict):
		return "input_request_version_conflict", ExitVersionConflict, true
	case errors.Is(err, inputrequest.ErrInvalidTransition):
		return "input_request_invalid_transition", ExitInvalidTransition, true
	case errors.Is(err, inputrequest.ErrInvalidStatus):
		return "input_request_invalid_status", ExitUsage, true
	case errors.Is(err, inputrequest.ErrInvalidUrgency):
		return "input_request_invalid_urgency", ExitUsage, true

	// Discussion — Issue
	case errors.Is(err, discussion.ErrIssueNotFound):
		return "issue_not_found", ExitNotFound, true
	case errors.Is(err, discussion.ErrIssueAlreadyExists):
		return "issue_already_exists", ExitBusinessError, true
	case errors.Is(err, discussion.ErrIssueInvalidTransition):
		return "issue_invalid_transition", ExitInvalidTransition, true
	case errors.Is(err, discussion.ErrIssueVersionConflict):
		return "issue_version_conflict", ExitVersionConflict, true
	case errors.Is(err, discussion.ErrIssueAlreadyConcluded):
		return "issue_already_concluded", ExitInvalidTransition, true
	case errors.Is(err, discussion.ErrIssueWithdrawn):
		return "issue_withdrawn", ExitInvalidTransition, true
	case errors.Is(err, discussion.ErrIssueNoConversationBound):
		return "issue_no_conversation_bound", ExitInvariantViolation, true
	case errors.Is(err, discussion.ErrInvalidOrigin):
		return "issue_invalid_origin", ExitUsage, true
	case errors.Is(err, discussion.ErrResolutionInvalid):
		return "issue_invalid_resolution", ExitUsage, true
	case errors.Is(err, disservice.ErrProjectNotFound):
		return "project_not_found", ExitNotFound, true
	}
	return "", 0, false
}

// HandleDomainError formats and prints err using format ("human" or
// "json") and returns the matching exit code. Falls back to
// internal_error / ExitBusinessError for unknown error types.
func HandleDomainError(w io.Writer, format string, err error) ExitCode {
	if err == nil {
		return ExitOK
	}
	if reason, code, ok := MapDomainError(err); ok {
		return PrintError(w, format, reason, err.Error(), code)
	}
	return PrintError(w, format, "internal_error", err.Error(), ExitBusinessError)
}

// HandleClientError translates an admin Client error to the same
// (reason, exit-code) shape as HandleDomainError. This is the
// post-v2.2-B handler error path: domain errors come back over the
// wire wrapped in *ClientError, with the server-side code in
// ClientError.Code mapping to the same reason strings handlers used
// pre-migration.
//
// Mapping rules:
//   - server-side codes (not_found, version_conflict, already_exists,
//     terminal, invalid_transition, invalid_input) map to exit-code
//     equivalents from the legacy domain table.
//   - ErrServerUnreachable / ErrClientNotConfigured surface a friendly
//     "is the server running?" hint with ExitBusinessError.
//   - everything else falls through to ExitBusinessError with the raw
//     error message preserved in the message field.
func HandleClientError(w io.Writer, format string, err error) ExitCode {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrClientNotConfigured) || errors.Is(err, ErrServerUnreachable) {
		return PrintError(w, format, "server_unreachable",
			err.Error()+" (start the server: agent-center server)",
			ExitBusinessError)
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		reason, code := mapClientErrorCode(ce)
		msg := ce.Message
		if msg == "" {
			msg = ce.Error()
		}
		return PrintError(w, format, reason, msg, code)
	}
	return PrintError(w, format, "internal_error", err.Error(), ExitBusinessError)
}

// mapClientErrorCode translates a *ClientError into the (reason, code)
// pair handlers expect. We mirror MapDomainError's bucketing using the
// server-side envelope code (admin/api/errors.go) rather than the wire
// status code so reason strings stay stable.
func mapClientErrorCode(ce *ClientError) (string, ExitCode) {
	switch ce.Code {
	case "not_found":
		return "not_found", ExitNotFound
	case "already_exists":
		return "already_exists", ExitBusinessError
	case "version_conflict":
		return "version_conflict", ExitVersionConflict
	case "terminal":
		return "terminal", ExitInvalidTransition
	case "invalid_transition":
		return "invalid_transition", ExitInvalidTransition
	case "invalid_input":
		return "invalid_input", ExitUsage
	case "invalid_json":
		return "invalid_input", ExitUsage
	}
	// Fall back on HTTP status when the server didn't emit a recognised
	// envelope code (e.g. 501 not_implemented from a stub handler).
	switch {
	case ce.IsNotFound():
		return "not_found", ExitNotFound
	case ce.IsConflict():
		return "already_exists", ExitBusinessError
	case ce.Status == 501:
		return "not_implemented", ExitNotImplemented
	default:
		return "internal_error", ExitBusinessError
	}
}
