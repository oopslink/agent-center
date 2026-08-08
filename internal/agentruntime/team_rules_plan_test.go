package agentruntime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func planRulesBody(commit, body string) map[string]any {
	return map[string]any{
		"team_id": "team-plan", "phase": "plan", "commit": commit,
		"refresh_semantics": "snapshot at planning fork",
		"rules": []map[string]any{{
			"slug": "plan-dag", "title": "Plan DAG", "description": "plan with a DAG",
			"body": body, "enabled": true, "applies_to": []string{"plan"},
			"source_path": "rules/plan-dag.md",
		}},
	}
}

func TestNotifyWorkAvailable_PlanningTaskInjectsPlanRules(t *testing.T) {
	rt, _ := newTestRuntime(t)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{
			"id": "task-plan", "title": "Create a plan", "description": "Use edit_plan_topology for the DAG.",
			"status": "open", "dispatch_mode": "supervisor_inline",
		},
		teamRulesBody: planRulesBody("plan-sha-1", "Use explicit dependencies."),
	}
	setToolCaller(rt, sc)

	if err := rt.NotifyWorkAvailable(context.Background(), "task-plan"); err != nil {
		t.Fatalf("NotifyWorkAvailable: %v", err)
	}

	if seen := sc.toolsSeen(); len(seen) != 2 || seen[0] != "get_task" || seen[1] != "get_team_rules" {
		t.Fatalf("tool calls = %v, want [get_task get_team_rules]", seen)
	}
	if body, ok := sc.callFor("get_team_rules"); !ok || body["phase"] != "plan" || body["agent_id"] != "agent-x" {
		t.Fatalf("get_team_rules body = %v", body)
	}
	msgs := fs.msgs()
	if len(msgs) != 1 {
		t.Fatalf("injected messages = %d, want 1", len(msgs))
	}
	for _, want := range []string{
		"[work_available] Task task-plan is assigned to you.",
		"## Team Rules (plan)",
		"team=team-plan commit=plan-sha-1",
		"refresh_semantics: snapshot at planning fork",
		"enabled=true source_path=rules/plan-dag.md applies_to=plan",
		"Use explicit dependencies.",
	} {
		if !strings.Contains(msgs[0], want) {
			t.Fatalf("planning brief missing %q:\n%s", want, msgs[0])
		}
	}
}

func TestNotifyWorkAvailable_NonPlanningTaskDoesNotLoadPlanRules(t *testing.T) {
	rt, _ := newTestRuntime(t)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
	sc := &scriptedToolCaller{
		getTaskBody: map[string]any{
			"id": "task-code", "title": "Fix bug", "description": "Patch the code.",
			"status": "open", "dispatch_mode": "executor_fork",
		},
		teamRulesBody: planRulesBody("should-not-load", "Do not inject."),
	}
	setToolCaller(rt, sc)

	if err := rt.NotifyWorkAvailable(context.Background(), "task-code"); err != nil {
		t.Fatalf("NotifyWorkAvailable: %v", err)
	}
	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want only get_task", seen)
	}
	msgs := fs.msgs()
	if len(msgs) != 1 || strings.Contains(msgs[0], "## Team Rules") {
		t.Fatalf("non-planning nudge must not include team rules: %v", msgs)
	}
}

func TestNotifyConverse_PlanChatInjectsFrozenRulesAndReplayDoesNotRefresh(t *testing.T) {
	home := t.TempDir()
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-x", WorkerID: "worker-1", AgentHomeBase: home, Reporter: &nopReporter{},
		Now: func() time.Time { return time.Unix(10, 0) },
		Log: func(string, ...any) {},
	}, &SessionState{})
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
	sc := &scriptedToolCaller{teamRulesBody: planRulesBody("plan-sha-1", "Initial planning rule.")}
	setToolCaller(rt, sc)

	req := ConverseRequest{
		AgentID: "agent-x", ConversationID: "conv-plan", ConvKind: "plan", ConvName: "Release Plan",
		SenderDisplay: "Ada", MessageID: "msg-1", MessageText: "Please update this plan.",
		OwnerRef: "pm://plans/plan-1",
	}
	if err := rt.NotifyConverse(context.Background(), req); err != nil {
		t.Fatalf("NotifyConverse: %v", err)
	}
	if body, ok := sc.callFor("get_team_rules"); !ok || body["phase"] != "plan" {
		t.Fatalf("get_team_rules body = %v", body)
	}
	first := fs.msgs()[0]
	if !strings.Contains(first, "commit=plan-sha-1") || !strings.Contains(first, "Initial planning rule.") {
		t.Fatalf("plan converse brief missing initial rules:\n%s", first)
	}

	// Simulate a runtime restart while the same converse turn is interrupted. The
	// replay must use the persisted brief, not a fresh get_team_rules call.
	sc.teamRulesBody = planRulesBody("plan-sha-2", "Refreshed planning rule.")
	rt2 := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-x", WorkerID: "worker-1", AgentHomeBase: home, Reporter: &nopReporter{},
		Now: func() time.Time { return time.Unix(11, 0) },
		Log: func(string, ...any) {},
	}, &SessionState{})
	fs2 := &fakeSession{}
	rt2.withState(func(s *SessionState) { s.Session = fs2 })
	if err := rt2.RecoverInterruptedConverse(context.Background()); err != nil {
		t.Fatalf("RecoverInterruptedConverse: %v", err)
	}
	replayed := fs2.msgs()[0]
	if !strings.Contains(replayed, "commit=plan-sha-1") || strings.Contains(replayed, "plan-sha-2") {
		t.Fatalf("interrupted replay refreshed rules instead of replaying frozen brief:\n%s", replayed)
	}
}

func TestNotifyConverse_NewPlanningTurnReloadsRules(t *testing.T) {
	rt, _ := newTestRuntime(t)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
	sc := &scriptedToolCaller{teamRulesBody: planRulesBody("plan-sha-1", "Rule v1.")}
	setToolCaller(rt, sc)

	req := ConverseRequest{
		AgentID: "agent-x", ConversationID: "conv-plan", ConvKind: "plan", ConvName: "Release Plan",
		SenderDisplay: "Ada", MessageText: "Edit the plan topology.", OwnerRef: "pm://plans/plan-1",
	}
	req.MessageID = "msg-1"
	if err := rt.NotifyConverse(context.Background(), req); err != nil {
		t.Fatalf("NotifyConverse first: %v", err)
	}
	sc.teamRulesBody = planRulesBody("plan-sha-2", "Rule v2.")
	req.MessageID = "msg-2"
	if err := rt.NotifyConverse(context.Background(), req); err != nil {
		t.Fatalf("NotifyConverse second: %v", err)
	}

	msgs := fs.msgs()
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	if !strings.Contains(msgs[0], "commit=plan-sha-1") || !strings.Contains(msgs[1], "commit=plan-sha-2") {
		t.Fatalf("new planning turns must reload current rules:\n--- first ---\n%s\n--- second ---\n%s", msgs[0], msgs[1])
	}
}

func TestRulePhaseForTaskExecuteReviewRecoveryUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		task *centerTaskDetail
		want string
	}{
		{name: "nil", task: nil, want: rulePhaseExecute},
		{name: "ordinary", task: &centerTaskDetail{Title: "Fix bug"}, want: rulePhaseExecute},
		{name: "origin verdict", task: &centerTaskDetail{OriginVerdictID: "verdict-1"}, want: rulePhaseRecovery},
		{name: "follows task", task: &centerTaskDetail{FollowsTaskID: "task-1"}, want: rulePhaseRecovery},
		{name: "supervisor inline", task: &centerTaskDetail{DispatchMode: "supervisor_inline"}, want: rulePhaseReview},
		{name: "evidence only", task: &centerTaskDetail{DeliveryContract: "evidence_only"}, want: rulePhaseReview},
		{name: "review title", task: &centerTaskDetail{Title: "Review delivery gate"}, want: rulePhaseReview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rulePhaseForTask(tc.task); got != tc.want {
				t.Fatalf("rulePhaseForTask = %q, want %q", got, tc.want)
			}
		})
	}
}
