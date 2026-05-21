package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

type phase3Stack struct {
	clk        *clock.FakeClock
	sink       *observability.EventSink
	er         *obsqlite.EventRepo
	issueRepo  discussion.IssueRepository
	taskRepo   task.Repository
	convRepo   conversation.ConversationRepository
	msgRepo    conversation.MessageRepository
	writer     *convservice.MessageWriter
	lifecycle  *disservice.IssueLifecycleService
	commentSvc *disservice.IssueCommentService
	bindSvc    *disservice.IssueBindConversationService
	linkSvc    *disservice.IssueLinkConversationService
}

func setupPhase3(t *testing.T) *phase3Stack {
	t.Helper()
	rawDB, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(rawDB).Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx,
		`INSERT INTO projects (id, name, created_at, updated_at, created_by_identity_id)
		 VALUES ('p-1','P','2026-05-21T12:00:00Z','2026-05-21T12:00:00Z','user:h')`); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(ctx, rawDB)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	issueRepo := disqlite.NewIssueRepo(rawDB)
	convRepo := convsqlite.NewConversationRepo(rawDB)
	msgRepo := convsqlite.NewMessageRepo(rawDB)
	taskRepo := trsqlite.NewTaskRepo(rawDB)
	writer := convservice.NewMessageWriter(rawDB, convRepo, msgRepo, sink, gen, clk)
	opener := disservice.NewIssueConversationOpener(convRepo, sink, gen, clk)
	spawner := dispatch.NewIssueConcludeSpawn(rawDB, taskRepo, sink, gen, clk)
	lifecycle := disservice.NewIssueLifecycleService(rawDB, issueRepo, opener, writer, sink, gen, clk).
		WithSpawnerAndCommenter(spawner, writer)
	commentSvc := disservice.NewIssueCommentService(issueRepo, convRepo, msgRepo, writer, lifecycle, clk)
	bindSvc := disservice.NewIssueBindConversationService(rawDB, issueRepo, convRepo, opener, sink, clk)
	linkSvc := disservice.NewIssueLinkConversationService(rawDB, issueRepo, convRepo, clk)
	return &phase3Stack{
		clk: clk, sink: sink, er: er,
		issueRepo: issueRepo, taskRepo: taskRepo,
		convRepo: convRepo, msgRepo: msgRepo, writer: writer,
		lifecycle: lifecycle, commentSvc: commentSvc, bindSvc: bindSvc, linkSvc: linkSvc,
	}
}

func eventsByType(t *testing.T, er *obsqlite.EventRepo) map[string]int {
	t.Helper()
	events, _ := er.Find(context.Background(), observability.EventQueryFilter{Limit: 1000})
	got := map[string]int{}
	for _, e := range events {
		got[e.Type().String()]++
	}
	return got
}

// I-01: Issue + Conversation 同事务建（同步建路径）
func TestINT_P3_IssueAndConversationSameTx(t *testing.T) {
	s := setupPhase3(t)
	res, err := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID:          "p-1",
		Title:              "feishu issue",
		OpenedByIdentityID: "user:h",
		Origin:             discussion.OriginFeishuAt,
		Actor:              observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" {
		t.Fatal("sync-build must produce conv id")
	}
	got := eventsByType(t, s.er)
	if got["issue.opened"] != 1 || got["conversation.opened"] != 1 {
		t.Fatalf("events: %+v", got)
	}
	iss, _ := s.issueRepo.FindByID(context.Background(), res.IssueID)
	if iss.ConversationID() != res.ConversationID {
		t.Fatal("conversation_id not propagated")
	}
	conv, _ := s.convRepo.FindByID(context.Background(), res.ConversationID)
	if conv.Kind() != conversation.ConversationKindIssue {
		t.Fatal("conv kind wrong")
	}
}

// I-03: Comment facade → Conversation Message + discussion_started
func TestINT_P3_IssueCommentTriggersDiscussionStarted(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:opener",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:opener"),
	})
	if _, err := s.commentSvc.Comment(context.Background(), disservice.CommentInput{
		IssueID:          res.IssueID,
		Content:          "hi",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:            observability.Actor("user:peer"),
	}); err != nil {
		t.Fatal(err)
	}
	got := eventsByType(t, s.er)
	if got["conversation.message_added"] != 1 || got["issue.discussion_started"] != 1 {
		t.Fatalf("events: %+v", got)
	}
}

// I-04 + I-05: IssueConcludeSpawn + Conclude full events
func TestINT_P3_ConcludeSpawnFullEventChain(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	out, err := s.lifecycle.Conclude(context.Background(), disservice.ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionClosedWithTasks,
			Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{
				{LocalID: "a", Title: "T-A"},
				{LocalID: "b", Title: "T-B", DependsOnLocalIDs: []string{"a"}},
				{LocalID: "c", Title: "T-C", DependsOnLocalIDs: []string{"b"}},
			},
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.TaskIDs) != 3 {
		t.Fatalf("expected 3 tasks: %d", len(out.TaskIDs))
	}
	got := eventsByType(t, s.er)
	if got["issue.concluded"] != 1 || got["issue.tasks_spawned"] != 1 || got["task.created"] != 3 {
		t.Fatalf("events: %+v", got)
	}
}

// I-06: Spawn 失败 → tx 全回滚
func TestINT_P3_ConcludeSpawnFailureRollsBackEverything(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	_, err := s.lifecycle.Conclude(context.Background(), disservice.ConcludeIssueCommand{
		IssueID: res.IssueID,
		Resolution: discussion.Resolution{
			Kind:    discussion.ResolutionClosedWithTasks,
			Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{
				{LocalID: "a", Title: "T-A"},
				{LocalID: "b", Title: "T-B", DependsOnTaskIDs: []taskruntime.TaskID{"ghost-task"}},
			},
		},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected err on bad dep")
	}
	iss, _ := s.issueRepo.FindByID(context.Background(), res.IssueID)
	if iss.Status() != discussion.StatusOpen {
		t.Fatalf("status: %s", iss.Status())
	}
	got := eventsByType(t, s.er)
	if got["issue.concluded"] != 0 || got["task.created"] != 0 || got["issue.tasks_spawned"] != 0 {
		t.Fatalf("rollback failed: %+v", got)
	}
}

// I-07: BindAuto 同事务
func TestINT_P3_BindAutoOneTx(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := s.bindSvc.BindAuto(context.Background(), disservice.BindAutoInput{
		IssueID: res.IssueID, Channel: "web", Actor: observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	iss, _ := s.issueRepo.FindByID(context.Background(), res.IssueID)
	if !iss.HasConversation() {
		t.Fatal("bind failed")
	}
	conv, _ := s.convRepo.FindByID(context.Background(), iss.ConversationID())
	if conv.Kind() != conversation.ConversationKindIssue {
		t.Fatal("conv kind wrong")
	}
}

// I-08: BindTo when target conv is owned by another issue
func TestINT_P3_BindToConflictsWithOwningIssue(t *testing.T) {
	s := setupPhase3(t)
	resA, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "A", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	resB, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "B", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	err := s.bindSvc.BindTo(context.Background(), disservice.BindToInput{
		IssueID: resB.IssueID, ConversationID: resA.ConversationID, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("expected ConvAlreadyExists, got %v", err)
	}
}

// I-09: Link dedupes across calls
func TestINT_P3_LinkDedupesAcrossCalls(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	dm, err := s.writer.OpenConversation(context.Background(), convservice.OpenCommand{
		Kind: conversation.ConversationKindDM, Title: "dm", Actor: observability.Actor("user:h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.linkSvc.Link(context.Background(), disservice.LinkInput{
			IssueID: res.IssueID, ConversationID: dm.ConversationID,
			Actor: observability.Actor("user:h"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	iss, _ := s.issueRepo.FindByID(context.Background(), res.IssueID)
	if len(iss.RelatedConversationIDs()) != 1 {
		t.Fatalf("expected 1 link after dedupe: %d", len(iss.RelatedConversationIDs()))
	}
}

// I-10: terminal blocks comment
func TestINT_P3_TerminalIssueRejectsComment(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	if _, err := s.lifecycle.Conclude(context.Background(), disservice.ConcludeIssueCommand{
		IssueID:     res.IssueID,
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "skip"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.commentSvc.Comment(context.Background(), disservice.CommentInput{
		IssueID:          res.IssueID,
		Content:          "after terminal",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:            observability.Actor("user:peer"),
	})
	if !errors.Is(err, discussion.ErrIssueInvalidTransition) {
		t.Fatalf("expected terminal block, got %v", err)
	}
}

// I-12: events.seq strictly monotonic
func TestINT_P3_EventsSeqMonotonic(t *testing.T) {
	s := setupPhase3(t)
	res, _ := s.lifecycle.Open(context.Background(), disservice.OpenIssueCommand{
		ProjectID: "p-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginFeishuAt, Actor: observability.Actor("user:h"),
	})
	if _, err := s.commentSvc.Comment(context.Background(), disservice.CommentInput{
		IssueID:          res.IssueID,
		Content:          "hi",
		ContentKind:      conversation.MessageContentText,
		SenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:            observability.Actor("user:peer"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.lifecycle.Conclude(context.Background(), disservice.ConcludeIssueCommand{
		IssueID:     res.IssueID,
		Resolution:  discussion.Resolution{Kind: discussion.ResolutionClosedNoAction, Summary: "x"},
		ConcludedBy: "user:h",
		Actor:       observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	events, _ := s.er.Find(context.Background(), observability.EventQueryFilter{Limit: 100})
	if len(events) < 4 {
		t.Fatalf("expected ≥ 4 events, got %d", len(events))
	}
	var last int64
	for _, e := range events {
		if e.Seq() <= last {
			t.Fatalf("seq not strictly monotonic: %d after %d", e.Seq(), last)
		}
		last = e.Seq()
	}
}
