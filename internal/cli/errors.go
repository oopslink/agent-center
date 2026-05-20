package cli

import (
	"errors"
	"io"

	"github.com/oopslink/agent-center/internal/conversation"
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
	case errors.Is(err, workforce.ErrProjectInvalidSlug):
		return "project_invalid_slug", ExitUsage, true
	case errors.Is(err, workforce.ErrProjectInvalidKind):
		return "project_invalid_kind", ExitUsage, true

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
	case errors.Is(err, conversation.ErrMessageDuplicate):
		return "message_duplicate", ExitBusinessError, true
	case errors.Is(err, conversation.ErrMessageImmutable):
		return "message_immutable", ExitInvalidTransition, true
	case errors.Is(err, conversation.ErrMessageInvalidSender):
		return "message_invalid_sender", ExitUsage, true
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
