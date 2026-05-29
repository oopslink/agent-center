package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// fullSetup wires the producer Service + the B2-b ParticipantProjector + a
// relay over one shared DB, so a test can drive the end-to-end
// AppService→outbox→projector→Conversation path.
func fullSetup(t *testing.T) (*Service, *convsql.ConversationRepo, *outbox.Relay, context.Context) {
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
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: ob, IDGen: gen, Clock: clk,
	})
	proj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, proj)
	return svc, convRepo, relay, context.Background()
}

func participantIDs(c *conversation.Conversation) []string {
	parts := c.Participants()
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, string(p.IdentityID))
	}
	return out
}

func hasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestParticipantProjector_CreatesConvAndSyncsParticipants(t *testing.T) {
	svc, convRepo, relay, ctx := fullSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	ownerRef := conversation.NewTaskOwnerRef(string(tid))

	// Drain the outbox: the projector creates the task Conversation.
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	conv, err := convRepo.FindByOwnerRef(ctx, ownerRef)
	if err != nil {
		t.Fatalf("task Conversation should exist by owner_ref: %v", err)
	}
	if conv.Kind() != conversation.ConversationKindTask {
		t.Fatalf("conv kind = %s, want task", conv.Kind())
	}
	if got := participantIDs(conv); len(got) != 1 || got[0] != "user:a" {
		t.Fatalf("participants should be {creator}, got %v", got)
	}

	// Subscribe a watcher → another event → participants become {creator, watcher}.
	if err := svc.SubscribeTask(ctx, tid, "user:watcher", "user:a"); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	conv2, _ := convRepo.FindByOwnerRef(ctx, ownerRef)
	ids := participantIDs(conv2)
	if len(ids) != 2 || !hasID(ids, "user:a") || !hasID(ids, "user:watcher") {
		t.Fatalf("participants should be {creator, watcher}, got %v", ids)
	}

	// Replay is a no-op (idempotent): rerunning changes nothing.
	if n, err := relay.RunOnce(ctx, 100); err != nil || n != 0 {
		t.Fatalf("replay RunOnce = %d, %v; want 0 (all processed)", n, err)
	}
	conv3, _ := convRepo.FindByOwnerRef(ctx, ownerRef)
	if len(participantIDs(conv3)) != 2 {
		t.Fatalf("replay must not change participants, got %d", len(participantIDs(conv3)))
	}
}

func TestParticipantProjector_IgnoresNonConversationEvents(t *testing.T) {
	svc, convRepo, relay, ctx := fullSetup(t)
	// CreateProject emits pm.project.created (+ pm.member.added). The projector
	// must ignore both — no task/issue conversation created.
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := relay.RunOnce(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := convRepo.FindByOwnerRef(ctx, conversation.NewProjectOwnerRef(string(pid))); err == nil {
		t.Fatal("project must not get a projected conversation")
	}
}
