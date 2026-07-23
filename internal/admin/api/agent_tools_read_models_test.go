package api

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
)

type readModelActivityRepo struct {
	events []*agent.AgentActivityEvent
}

func (r readModelActivityRepo) Append(context.Context, *agent.AgentActivityEvent) error { return nil }
func (r readModelActivityRepo) ListByAgent(context.Context, agent.AgentID, int, string) ([]*agent.AgentActivityEvent, error) {
	return nil, nil
}
func (r readModelActivityRepo) ListByTask(context.Context, string) ([]*agent.AgentActivityEvent, error) {
	return r.events, nil
}
func (r readModelActivityRepo) LatestByAgents(context.Context, []agent.AgentID) (map[agent.AgentID]*agent.AgentActivityEvent, error) {
	return nil, nil
}

func readModelEvent(t *testing.T, id, payload string, at time.Time) *agent.AgentActivityEvent {
	t.Helper()
	ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: id, AgentID: "agent-1", TaskRef: "task-1",
		InteractionRef: "executor:exec-1", EventType: agent.EventTypeLifecycle,
		Payload: payload, OccurredAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestTaskExecutionsProjectsPersistedLifecycle(t *testing.T) {
	start := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	repo := readModelActivityRepo{events: []*agent.AgentActivityEvent{
		readModelEvent(t, "01", `{"event":"executor.start","cli":"codex","model":"gpt-5"}`, start),
		readModelEvent(t, "02", `{"event":"executor.stop","outcome":"failed","reason":"repo_source_unavailable","detail":"token=must-not-leak","recovered":true}`, start.Add(time.Minute)),
	}}
	runs, err := taskExecutions(context.Background(), HandlerDeps{AgentActivityRepo: repo}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.ExecutionID != "exec-1" || got.CLI != "codex" || got.Model != "gpt-5" ||
		got.Outcome != "failed" || got.ErrorKind != "repo_source_unavailable" ||
		got.ErrorDetail != "[redacted]" || !got.Recovered {
		t.Fatalf("run = %+v", got)
	}
}

func TestRedactAuditNote(t *testing.T) {
	for _, note := range []string{"token=abc", "Authorization: Bearer abc", "PASSWORD=hunter2"} {
		if got := redactAuditNote(note); got != "[redacted]" {
			t.Errorf("redactAuditNote(%q) = %q", note, got)
		}
	}
	if got := redactAuditNote("normal operator note"); got != "normal operator note" {
		t.Fatalf("ordinary note changed: %q", got)
	}
}
