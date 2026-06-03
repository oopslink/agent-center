package cli

import (
	"errors"
	"io"

	"github.com/oopslink/agent-center/internal/conversation"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
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

	// ProjectManager — Project (v2.7 #131 PR-3: CLI project READ handlers
	// read the pm model, so the pm not-found sentinel must map too).
	case errors.Is(err, pm.ErrProjectNotFound):
		return "project_not_found", ExitNotFound, true

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
