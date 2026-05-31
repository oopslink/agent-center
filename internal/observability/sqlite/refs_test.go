package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestRefsLikeMap_AllFieldsRoundtrip(t *testing.T) {
	f := observability.EventRefsFilter{
		WorkerID:       "w",
		ProjectID:      "p",
		ProposalID:     "pr",
		MappingID:      "m",
		ConversationID: "c",
		MessageID:      "msg",
		TaskID:         "t",
		ExecutionID:    "e",
		InputRequestID: "ir",
		IssueID:        "i",
		WorkItemID:     "wi",
	}
	m := refsLikeMap(f)
	expectedKeys := []string{
		"worker_id", "project_id", "proposal_id", "mapping_id",
		"conversation_id", "message_id", "task_id", "execution_id",
		"input_request_id", "issue_id", "work_item_id",
	}
	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %s", k)
		}
	}
}

func TestEventRepo_Find_FilterByDecisionID(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	g := idgen.NewGenerator(fc)
	for i, d := range []string{"D-1", "D-2"} {
		e, _ := observability.NewEvent(observability.NewEventInput{
			ID: observability.EventID(g.NewULID()), OccurredAt: fc.Now(),
			Seq: repo.NextSeq(), EventType: "x.y", Actor: "system",
			Payload: map[string]any{}, DecisionID: d,
		})
		_ = repo.Append(context.Background(), e)
		_ = i
	}
	d := "D-1"
	got, _ := repo.Find(context.Background(), observability.EventQueryFilter{DecisionID: &d})
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestEventRepo_Find_FilterByEachRef(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	g := idgen.NewGenerator(fc)
	// One event per ref kind.
	refs := []observability.EventRefs{
		{ProjectID: "p"},
		{ProposalID: "pr"},
		{MappingID: "m"},
		{ConversationID: "c"},
		{MessageID: "msg"},
		{TaskID: "t"},
		{ExecutionID: "e"},
		{InputRequestID: "ir"},
		{IssueID: "i"},
	}
	for _, r := range refs {
		e, _ := observability.NewEvent(observability.NewEventInput{
			ID: observability.EventID(g.NewULID()), OccurredAt: fc.Now(),
			Seq: repo.NextSeq(), EventType: "x.y", Actor: "system",
			Refs: r, Payload: map[string]any{},
		})
		_ = repo.Append(context.Background(), e)
	}
	// Each filter should match exactly one.
	for _, f := range []observability.EventRefsFilter{
		{ProjectID: "p"},
		{ProposalID: "pr"},
		{MappingID: "m"},
		{ConversationID: "c"},
		{MessageID: "msg"},
		{TaskID: "t"},
		{ExecutionID: "e"},
		{InputRequestID: "ir"},
		{IssueID: "i"},
	} {
		got, _ := repo.Find(context.Background(), observability.EventQueryFilter{Refs: f})
		if len(got) != 1 {
			t.Fatalf("filter %+v: got %d", f, len(got))
		}
	}
}

// TestEventRepo_Find_FilterByWorkItemID is the P1 fix (v2.7 #107 proj-A): the
// work-item transition events the #111 #3b observability fan-out records carry
// EventRefs.WorkItemID (+AgentID), NOT task_id — so inspect/query recent_events
// must be filterable by work_item_id (faithful repoint of the old execution_id
// filter). A task-keyed event must NOT match a work_item_id filter, and a
// work_item_id filter must surface exactly the subject WI's events (no siblings).
func TestEventRepo_Find_FilterByWorkItemID(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	g := idgen.NewGenerator(fc)
	refs := []observability.EventRefs{
		{WorkItemID: "WI-1", AgentID: "AG-1"},
		{WorkItemID: "WI-2", AgentID: "AG-2"}, // sibling WI: must not leak in
		{TaskID: "T-1"},                       // task-keyed: must not match a work_item_id filter
	}
	for _, r := range refs {
		e, _ := observability.NewEvent(observability.NewEventInput{
			ID: observability.EventID(g.NewULID()), OccurredAt: fc.Now(),
			Seq: repo.NextSeq(), EventType: "agent.work_item.transitioned", Actor: "system",
			Refs: r, Payload: map[string]any{},
		})
		_ = repo.Append(context.Background(), e)
	}
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkItemID: "WI-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Refs().WorkItemID != "WI-1" {
		t.Fatalf("work_item_id filter: want exactly WI-1, got %+v", got)
	}
}

func TestEventRepo_NextSeq_BeforeAppend(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	s1 := repo.NextSeq()
	s2 := repo.NextSeq()
	if s2 <= s1 {
		t.Fatalf("seq not monotonic: %d %d", s1, s2)
	}
}
