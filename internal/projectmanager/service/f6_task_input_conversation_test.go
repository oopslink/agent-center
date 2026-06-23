package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// f6Harness wires the Service + the participant projector (creates the task
// Conversation) + the F6 TaskInputConversationProjector (posts input_request/
// input_reply via a REAL TaskInputDispatchAdapter over MessageWriter) + a relay,
// so a test can drive block(input_required) → outbox → input_request message and
// unblock → input_reply message end to end.
type f6Harness struct {
	svc      *Service
	convRepo *convsql.ConversationRepo
	msgRepo  *convsql.MessageRepo
	relay    *outbox.Relay
	ctx      context.Context
}

func f6Setup(t *testing.T) *f6Harness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	ob := outboxsql.NewOutboxRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsql.NewConversationRepo(db)
	msgRepo := convsql.NewMessageRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	writer := convservice.NewMessageWriter(db, convRepo, msgRepo, sink, gen, clk).WithOutbox(ob)

	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs:   pmsql.NewCodeRepoRefRepo(db),
		Outbox:         ob,
		AgentDir:       allOrgDir("org-1"),
		IDGen:          gen,
		Clock:          clk,
		TaskActionLogs: pmsql.NewTaskActionLogRepo(db, gen),
	})
	partProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	inputProj := NewTaskInputConversationProjector(db, convRepo, convservice.NewTaskInputDispatchAdapter(writer), applied, clk)
	relay := outbox.NewRelay(ob, applied, clk, partProj, inputProj)
	return &f6Harness{svc: svc, convRepo: convRepo, msgRepo: msgRepo, relay: relay, ctx: context.Background()}
}

func (h *f6Harness) drain(t *testing.T) {
	t.Helper()
	for i := 0; i < 16; i++ {
		n, err := h.relay.RunOnce(h.ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// messagesByKind lists the task conversation messages of a given content kind.
func (h *f6Harness) messagesByKind(t *testing.T, tid pm.TaskID, kind conversation.MessageContentKind) []*conversation.Message {
	t.Helper()
	conv, err := h.convRepo.FindByOwnerRef(h.ctx, conversation.NewTaskOwnerRef(string(tid)))
	if err != nil {
		t.Fatalf("task conversation not found: %v", err)
	}
	msgs, err := h.msgRepo.FindByConversationID(h.ctx, conv.ID(), conversation.MessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var out []*conversation.Message
	for _, m := range msgs {
		if m.ContentKind() == kind {
			out = append(out, m)
		}
	}
	return out
}

// runningTask creates → assigns an agent → starts a task so block/unblock are reachable.
func (h *f6Harness) runningTask(t *testing.T) pm.TaskID {
	t.Helper()
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AssignTask(h.ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartTask(h.ctx, tid, "user:a"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	return tid
}

// TestF6_InputRequired_SurfacesRequestAndReply is the F6 acceptance: an
// input_required block posts an input_request into the task conversation (sender =
// the assignee agent); the user's unblock posts a threaded input_reply (sender =
// the user) AND records the comment as the BlockedComment while clearing the block.
func TestF6_InputRequired_SurfacesRequestAndReply(t *testing.T) {
	h := f6Setup(t)
	tid := h.runningTask(t)

	if err := h.svc.BlockTask(h.ctx, tid, "which branch?", pm.BlockReasonInputRequired, "agent:AG1"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	reqs := h.messagesByKind(t, tid, conversation.MessageContentInputRequest)
	if len(reqs) != 1 {
		t.Fatalf("want 1 input_request message, got %d", len(reqs))
	}
	if reqs[0].Content() != "which branch?" {
		t.Fatalf("input_request body = %q, want %q", reqs[0].Content(), "which branch?")
	}
	if string(reqs[0].SenderIdentityID()) != "agent:AG1" {
		t.Fatalf("input_request sender = %q, want agent:AG1", reqs[0].SenderIdentityID())
	}
	requestID := string(reqs[0].ID())

	if err := h.svc.UnblockTask(h.ctx, UnblockTaskCommand{
		TaskID: tid, Comment: "use main", InputRequestMessageID: requestID, Actor: "user:a",
	}); err != nil {
		t.Fatal(err)
	}
	h.drain(t)

	replies := h.messagesByKind(t, tid, conversation.MessageContentInputReply)
	if len(replies) != 1 {
		t.Fatalf("want 1 input_reply message, got %d", len(replies))
	}
	if replies[0].Content() != "use main" {
		t.Fatalf("input_reply body = %q, want %q", replies[0].Content(), "use main")
	}
	if string(replies[0].SenderIdentityID()) != "user:a" {
		t.Fatalf("input_reply sender = %q, want user:a", replies[0].SenderIdentityID())
	}
	if string(replies[0].ParentMessageID()) != requestID {
		t.Fatalf("input_reply not threaded under request: parent=%q want=%q", replies[0].ParentMessageID(), requestID)
	}

	tk, _ := h.svc.tasks.FindByID(h.ctx, tid)
	if tk.BlockedReason() != "" || tk.BlockedReasonType() != "" {
		t.Fatalf("unblock must clear the block annotation, got %q/%q", tk.BlockedReason(), tk.BlockedReasonType())
	}
	if tk.BlockedComment() != "use main" {
		t.Fatalf("unblock must record the comment, got %q", tk.BlockedComment())
	}
}

// TestF6_Obstacle_WritesNoConversationMessage: an obstacle block/unblock surfaces
// NEITHER an input_request NOR an input_reply (owner/PM action, no user reply).
func TestF6_Obstacle_WritesNoConversationMessage(t *testing.T) {
	h := f6Setup(t)
	tid := h.runningTask(t)

	if err := h.svc.BlockTask(h.ctx, tid, "needs a prod key", pm.BlockReasonObstacle, "agent:AG1"); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if n := len(h.messagesByKind(t, tid, conversation.MessageContentInputRequest)); n != 0 {
		t.Fatalf("obstacle block must post NO input_request, got %d", n)
	}

	if err := h.svc.UnblockTask(h.ctx, UnblockTaskCommand{TaskID: tid, Comment: "key added", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	if n := len(h.messagesByKind(t, tid, conversation.MessageContentInputReply)); n != 0 {
		t.Fatalf("obstacle unblock must post NO input_reply, got %d", n)
	}
	tk, _ := h.svc.tasks.FindByID(h.ctx, tid)
	if tk.BlockedReason() != "" {
		t.Fatalf("obstacle unblock must clear the block, got %q", tk.BlockedReason())
	}
}
