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
	}
	m := refsLikeMap(f)
	expectedKeys := []string{
		"worker_id", "project_id", "proposal_id", "mapping_id",
		"conversation_id", "message_id", "task_id", "execution_id",
		"input_request_id", "issue_id",
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
