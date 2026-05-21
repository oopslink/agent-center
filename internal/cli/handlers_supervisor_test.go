package cli

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestRecordDecisionCommand_Happy(t *testing.T) {
	a := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV1")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	out, errs, code := runHandler(t, a.RecordDecisionCommand(), []string{
		"--invocation=INV1", "--kind=no_op", "--rationale=just thinking", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("code = %d errs=%s", code, errs)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v %s", err, out)
	}
	if got["decision_id"] == "" {
		t.Error("missing decision_id")
	}
}

func TestRecordDecisionCommand_NoEnv(t *testing.T) {
	a := newTestApp(t)
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	_, _, code := runHandler(t, a.RecordDecisionCommand(), []string{
		"--invocation=INV1", "--kind=no_op", "--rationale=x",
	})
	if code == ExitOK {
		t.Fatal("expected non-OK")
	}
}

func TestRecordDecisionCommand_BadKind(t *testing.T) {
	a := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV1")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	_, _, code := runHandler(t, a.RecordDecisionCommand(), []string{
		"--invocation=INV1", "--kind=dispatch", "--rationale=x",
	})
	if code == ExitOK {
		t.Fatal("expected kind_not_allowed")
	}
}

func TestRecordDecisionCommand_MissingRationale(t *testing.T) {
	a := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV1")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	_, _, code := runHandler(t, a.RecordDecisionCommand(), []string{
		"--invocation=INV1", "--kind=no_op",
	})
	if code == ExitOK {
		t.Fatal("expected rationale_required")
	}
}

func TestRecordDecisionCommand_InvocationMismatch(t *testing.T) {
	a := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV1")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	_, _, code := runHandler(t, a.RecordDecisionCommand(), []string{
		"--invocation=DIFFERENT", "--kind=no_op", "--rationale=x",
	})
	if code == ExitOK {
		t.Fatal("expected mismatch")
	}
}

func TestSupervisorRetriggerCommand_NotFound(t *testing.T) {
	a := newTestApp(t)
	_, _, code := runHandler(t, a.SupervisorRetriggerCommand(), []string{"DOES_NOT_EXIST"})
	if code != ExitNotFound {
		t.Errorf("code = %d", code)
	}
}

func TestSupervisorRetriggerCommand_NoArgs(t *testing.T) {
	a := newTestApp(t)
	_, _, code := runHandler(t, a.SupervisorRetriggerCommand(), []string{})
	if code != ExitUsage {
		t.Errorf("code = %d", code)
	}
}

func TestSupervisorRetriggerCommand_InvalidStatus(t *testing.T) {
	a := newTestApp(t)
	// seed a running invocation
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INV9", Scope: scope, TriggerEvents: tes, StartedAt: time.Now().UTC()})
	if err := a.InvocationRepo.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	_, _, code := runHandler(t, a.SupervisorRetriggerCommand(), []string{"INV9"})
	if code != ExitInvalidTransition {
		t.Errorf("code = %d", code)
	}
}

func TestEscalateInputRequestCommand_MissingArgs(t *testing.T) {
	a := newTestApp(t)
	_, _, code := runHandler(t, a.EscalateInputRequestCommand(), []string{})
	if code != ExitUsage {
		t.Errorf("code = %d", code)
	}
	_, _, code = runHandler(t, a.EscalateInputRequestCommand(), []string{"IR-1"})
	// no rationale
	if code != ExitUsage {
		t.Errorf("code = %d", code)
	}
}

func TestTargetJSON(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"task:T-1", `{"task_id":"T-1"}`},
		{"issue:I-9", `{"issue_id":"I-9"}`},
		{"execution:E-2", `{"execution_id":"E-2"}`},
		{"conversation:C-3", `{"conversation_id":"C-3"}`},
		{"worker:W-1", `{"worker_id":"W-1"}`},
		{"input_request:IR-4", `{"input_request_id":"IR-4"}`},
		{"foo:bar", `{"foo":"bar"}`},
		{"", "{}"},
	}
	for _, tc := range cases {
		got := targetJSON(tc.in)
		if got != tc.want {
			t.Errorf("targetJSON(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRefsForScope_Mapping(t *testing.T) {
	for _, tc := range []struct {
		scope cognition.InvocationScope
		want  observability.EventRefs
	}{
		{cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1"), observability.EventRefs{TaskID: "T-1"}},
		{cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-1"), observability.EventRefs{IssueID: "I-1"}},
		{cognition.MustNewInvocationScope(cognition.ScopeConversation, "C-1"), observability.EventRefs{ConversationID: "C-1"}},
		{cognition.MustNewInvocationScope(cognition.ScopeWorker, "W-1"), observability.EventRefs{WorkerID: "W-1"}},
		{cognition.MustNewInvocationScope(cognition.ScopeGlobal, ""), observability.EventRefs{}},
	} {
		got := refsForScope(tc.scope)
		if got != tc.want {
			t.Errorf("refsForScope(%v) = %+v, want %+v", tc.scope, got, tc.want)
		}
	}
}
