package cli

import (
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

func TestMapDomainError_TaskRuntime(t *testing.T) {
	cases := []struct {
		err       error
		reasonExp string
		codeExp   ExitCode
	}{
		{task.ErrTaskNotFound, "task_not_found", ExitNotFound},
		{task.ErrTaskAlreadyExists, "task_already_exists", ExitBusinessError},
		{task.ErrTaskInvalidTransition, "task_invalid_transition", ExitInvalidTransition},
		{task.ErrTaskVersionConflict, "task_version_conflict", ExitVersionConflict},
		{task.ErrTaskInvariantViolation, "task_invariant_violation", ExitInvariantViolation},
		{task.ErrCannotUnbindConversation, "task_cannot_unbind_conversation", ExitInvalidTransition},
		{task.ErrInvalidPriority, "task_invalid_priority", ExitUsage},
		{task.ErrInvalidStatus, "task_invalid_status", ExitUsage},
		{execution.ErrTaskExecutionNotFound, "execution_not_found", ExitNotFound},
		{execution.ErrTaskExecutionAlreadyTerminated, "execution_already_terminated", ExitInvalidTransition},
		{execution.ErrTaskExecutionVersionConflict, "execution_version_conflict", ExitVersionConflict},
		{execution.ErrSingleActiveViolation, "single_active_violation", ExitInvariantViolation},
		{execution.ErrInvalidTransition, "execution_invalid_transition", ExitInvalidTransition},
		{execution.ErrUnknownReason, "unknown_reason", ExitUsage},
		{execution.ErrUnknownWorkspaceMode, "unknown_workspace_mode", ExitUsage},
		{execution.ErrArtifactNotFound, "artifact_not_found", ExitNotFound},
		{execution.ErrArtifactImmutable, "artifact_immutable", ExitInvalidTransition},
		{inputrequest.ErrInputRequestNotFound, "input_request_not_found", ExitNotFound},
		{inputrequest.ErrInputRequestAlreadyResolved, "input_request_already_resolved", ExitInvalidTransition},
		{inputrequest.ErrInputRequestVersionConflict, "input_request_version_conflict", ExitVersionConflict},
		{inputrequest.ErrInvalidTransition, "input_request_invalid_transition", ExitInvalidTransition},
		{inputrequest.ErrInvalidStatus, "input_request_invalid_status", ExitUsage},
		{inputrequest.ErrInvalidUrgency, "input_request_invalid_urgency", ExitUsage},
	}
	for _, c := range cases {
		t.Run(c.reasonExp, func(t *testing.T) {
			reason, code, ok := MapDomainError(c.err)
			if !ok {
				t.Fatalf("expected ok for %v", c.err)
			}
			if reason != c.reasonExp {
				t.Fatalf("reason: %s want %s", reason, c.reasonExp)
			}
			if code != c.codeExp {
				t.Fatalf("code: %d want %d", code, c.codeExp)
			}
		})
	}
}

func TestMapDomainError_UnknownPhase2(t *testing.T) {
	if _, _, ok := MapDomainError(errors.New("something random")); ok {
		t.Fatal("expected not mapped")
	}
}
