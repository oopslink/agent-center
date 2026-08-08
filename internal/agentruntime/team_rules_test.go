package agentruntime

import (
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

func TestRulePhaseForTask_ExecuteReviewRecoveryUnchanged(t *testing.T) {
	tests := []struct {
		name string
		task *centerTaskDetail
		want string
	}{
		{name: "nil defaults execute", want: rulePhaseExecute},
		{name: "ordinary task executes", task: &centerTaskDetail{Title: "Build feature"}, want: rulePhaseExecute},
		{name: "evidence-only reviews", task: &centerTaskDetail{DeliveryContract: executor.DeliveryContractEvidenceOnly}, want: rulePhaseReview},
		{name: "supervisor-inline reviews", task: &centerTaskDetail{DispatchMode: executor.DispatchModeSupervisorInline}, want: rulePhaseReview},
		{name: "review title reviews", task: &centerTaskDetail{Title: "Review implementation"}, want: rulePhaseReview},
		{name: "follow-up recovers", task: &centerTaskDetail{FollowsTaskID: "task-prev"}, want: rulePhaseRecovery},
		{name: "origin verdict recovers", task: &centerTaskDetail{OriginVerdictID: "verdict-1"}, want: rulePhaseRecovery},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rulePhaseForTask(tc.task); got != tc.want {
				t.Fatalf("rulePhaseForTask() = %q, want %q", got, tc.want)
			}
		})
	}
}
