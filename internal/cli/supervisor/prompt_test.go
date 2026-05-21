package supervisor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cli/supervisor"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
)

var (
	promptTestClock = clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	promptTestGen   = idgen.NewGeneratorWithReader(promptTestClock, idgen.DeterministicReader(99))
)

func mkEvent(t *testing.T, _ observability.EventID, etype observability.EventType, refs observability.EventRefs) *observability.Event {
	t.Helper()
	e, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(promptTestGen.NewULID()),
		OccurredAt: promptTestClock.Now(),
		Seq:        1,
		EventType:  etype,
		Refs:       refs,
		Actor:      "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAssemble_TaskScope(t *testing.T) {
	dir := t.TempDir()
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	ev := mkEvent(t, "ZA", "task.created", observability.EventRefs{TaskID: "T-1", ProjectID: "demo"})
	r, err := supervisor.Assemble(supervisor.AssembleInput{
		Scope:         scope,
		TriggerEvents: []*observability.Event{ev},
		MemoryDir:     dir,
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if !strings.Contains(r.Prompt, "task:T-1") {
		t.Errorf("prompt missing scope tag")
	}
	if !strings.Contains(r.Prompt, "task.created") {
		t.Errorf("prompt missing event")
	}
	if !strings.Contains(r.Prompt, "supervisor.md") {
		t.Errorf("prompt missing supervisor.md instruction")
	}
	if !strings.Contains(r.WorkDir, "projects/demo/tasks/T-1") && !strings.Contains(r.WorkDir, dir) {
		t.Errorf("workdir = %q", r.WorkDir)
	}
}

func TestAssemble_GlobalScope(t *testing.T) {
	dir := t.TempDir()
	scope := cognition.MustNewInvocationScope(cognition.ScopeGlobal, "")
	r, err := supervisor.Assemble(supervisor.AssembleInput{
		Scope:     scope,
		MemoryDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Prompt, "global") {
		t.Errorf("missing global")
	}
}

func TestAssemble_ManyEventsBuilds(t *testing.T) {
	dir := t.TempDir()
	scope := cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-1")
	var events []*observability.Event
	for i := 0; i < 50; i++ {
		// distinct ULIDs by varying suffix
		suf := string(rune('A' + i%26))
		events = append(events, mkEvent(t,
			observability.EventID("Z"+suf),
			"issue.commented",
			observability.EventRefs{IssueID: "I-1", ProjectID: "demo"}))
	}
	r, err := supervisor.Assemble(supervisor.AssembleInput{
		Scope: scope, TriggerEvents: events, MemoryDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Prompt, "issue:I-1") {
		t.Error("missing scope")
	}
}

func TestAssemble_ZeroScope(t *testing.T) {
	if _, err := supervisor.Assemble(supervisor.AssembleInput{MemoryDir: t.TempDir()}); err == nil {
		t.Error("expected scope-required err")
	}
}

func TestAssemble_AllInvocationScopes(t *testing.T) {
	dir := t.TempDir()
	cases := []cognition.InvocationScope{
		cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1"),
		cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-1"),
		cognition.MustNewInvocationScope(cognition.ScopeConversation, "C-1"),
		cognition.MustNewInvocationScope(cognition.ScopeWorker, "W-1"),
		cognition.MustNewInvocationScope(cognition.ScopeGlobal, ""),
	}
	for _, s := range cases {
		r, err := supervisor.Assemble(supervisor.AssembleInput{
			Scope:     s,
			MemoryDir: dir,
		})
		if err != nil {
			t.Errorf("%s: %v", s, err)
			continue
		}
		if r.Prompt == "" {
			t.Errorf("%s: empty prompt", s)
		}
	}
}
