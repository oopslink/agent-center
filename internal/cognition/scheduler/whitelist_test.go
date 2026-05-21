package scheduler_test

import (
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestIsWakeEvent_PositiveAndNegative(t *testing.T) {
	wake := []observability.EventType{
		"task.created", "task.dispatched", "task.dispatch_failed",
		"task.priority_changed", "task_execution.failed",
		"task_execution.input_required",
		"issue.opened", "issue.commented",
		"conversation.opened", "conversation.message_added",
		"worker.enrolled", "worker.online", "worker.offline",
		"supervisor.periodic_review_ticker",
		"input_request.escalated",
		"worker_project_proposal.proposed",
	}
	for _, t1 := range wake {
		if !scheduler.IsWakeEvent(t1) {
			t.Errorf("%s !wake", t1)
		}
	}
	notWake := []observability.EventType{
		"supervisor.invocation_started",
		"supervisor.invocation_succeeded",
		"supervisor.invocation_failed_alert",
		"supervisor.invocation_timed_out",
		"task_execution.progress_reported",
		"random.junk",
		"",
	}
	for _, t1 := range notWake {
		if scheduler.IsWakeEvent(t1) {
			t.Errorf("%s should NOT wake", t1)
		}
	}
}

func TestRouteToScope_HappyPaths(t *testing.T) {
	cases := []struct {
		event observability.EventType
		refs  observability.EventRefs
		kind  cognition.ScopeKind
		key   string
	}{
		{"task.created", observability.EventRefs{TaskID: "T-1"}, cognition.ScopeTask, "T-1"},
		{"issue.opened", observability.EventRefs{IssueID: "I-9"}, cognition.ScopeIssue, "I-9"},
		{"conversation.message_added", observability.EventRefs{ConversationID: "C-2"}, cognition.ScopeConversation, "C-2"},
		{"worker.online", observability.EventRefs{WorkerID: "W-1"}, cognition.ScopeWorker, "W-1"},
		{"supervisor.periodic_review_ticker", observability.EventRefs{}, cognition.ScopeGlobal, cognition.GlobalScopeKey},
		{"input_request.escalated", observability.EventRefs{}, cognition.ScopeGlobal, cognition.GlobalScopeKey},
	}
	for _, tc := range cases {
		s, ok := scheduler.RouteToScope(tc.event, tc.refs)
		if !ok {
			t.Errorf("%s: expected ok", tc.event)
			continue
		}
		if s.Kind() != tc.kind || s.Key() != tc.key {
			t.Errorf("%s: got %+v", tc.event, s)
		}
	}
}

func TestRouteToScope_MissingRefs(t *testing.T) {
	cases := []struct {
		event observability.EventType
		refs  observability.EventRefs
	}{
		{"task.created", observability.EventRefs{}},
		{"issue.opened", observability.EventRefs{TaskID: "T"}},
		{"conversation.opened", observability.EventRefs{}},
		{"worker.enrolled", observability.EventRefs{}},
	}
	for _, tc := range cases {
		if _, ok := scheduler.RouteToScope(tc.event, tc.refs); ok {
			t.Errorf("%s: expected !ok", tc.event)
		}
	}
}

func TestRouteToScope_NotInWhitelist(t *testing.T) {
	if _, ok := scheduler.RouteToScope("nope", observability.EventRefs{}); ok {
		t.Error("non-whitelist should not route")
	}
}

func TestAllWakeEventTypes(t *testing.T) {
	all := scheduler.AllWakeEventTypes()
	if len(all) < 16 {
		t.Errorf("expected >=16 wake types, got %d", len(all))
	}
	// sorted asc
	for i := 1; i < len(all); i++ {
		if all[i-1] > all[i] {
			t.Errorf("not sorted: %s > %s", all[i-1], all[i])
		}
	}
}
