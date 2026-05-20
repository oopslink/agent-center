package cli

import (
	"fmt"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

func dispatchInputFromArgs(taskID, workerID, agentCLI, baseBranch string, actor observability.Actor) dispatch.DispatchInput {
	return dispatch.DispatchInput{
		TaskID:     taskruntime.TaskID(taskID),
		WorkerID:   workerID,
		AgentCLI:   agentCLI,
		BaseBranch: baseBranch,
		Actor:      actor,
	}
}

func killReasonFromString(s string) execution.KilledReason {
	r := execution.KilledReason(s)
	if r.Validate() == nil {
		return r
	}
	// Default to user_request if unknown; the AR validator will catch
	// invalid ones, but most CLI paths accept user_request implicitly.
	return execution.KilledUserRequest
}

func parseUrgency(s string) (inputrequest.Urgency, error) {
	u, err := inputrequest.ParseUrgency(s)
	if err != nil {
		return "", fmt.Errorf("urgency %q: %w", s, err)
	}
	return u, nil
}
