package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/discussion"
	disqlite "github.com/oopslink/agent-center/internal/discussion/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type testHarness struct {
	db        *sql.DB
	issueRepo *disqlite.IssueRepo
	convRepo  *convsqlite.ConversationRepo
	msgRepo   *convsqlite.MessageRepo
	eventRepo *obsqlite.EventRepo
	sink      *observability.EventSink
	clk       *clock.FakeClock
	gen       idgen.Generator
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	return &testHarness{
		db:        db,
		issueRepo: disqlite.NewIssueRepo(db),
		convRepo:  convsqlite.NewConversationRepo(db),
		msgRepo:   convsqlite.NewMessageRepo(db),
		eventRepo: er,
		sink:      sink,
		clk:       clk,
		gen:       gen,
	}
}

func (h *testHarness) lifecycle(t *testing.T) *IssueLifecycleService {
	t.Helper()
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	return NewIssueLifecycleService(h.db, h.issueRepo, opener, nil, h.sink, h.gen, h.clk)
}

func (h *testHarness) countEvents(t *testing.T, eventType string) int {
	t.Helper()
	row := h.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE event_type = ?`, eventType)
	var c int
	if err := row.Scan(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

// stubProjectChecker is a minimal ProjectExistenceChecker for tests.
type stubProjectChecker struct {
	known map[string]bool
	err   error
}

func (s stubProjectChecker) ProjectExists(_ context.Context, id string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.known[id], nil
}

func TestOpen_LazyCreate_CLI(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t).WithProjectExistenceChecker(stubProjectChecker{known: map[string]bool{"P-1": true}})
	res, err := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID:          "P-1",
		Title:              "hello",
		OpenedByIdentityID: "user:hayang",
		Origin:             discussion.OriginCLI,
		Actor:              observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID != "" {
		t.Fatal("CLI path should NOT create conversation")
	}
	got, err := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasConversation() {
		t.Fatal("issue should not be bound")
	}
	if h.countEvents(t, "issue.opened") != 1 {
		t.Fatal("issue.opened not emitted")
	}
	if h.countEvents(t, "conversation.opened") != 0 {
		t.Fatal("conversation.opened should not fire on lazy path")
	}
}

func TestOpen_SyncBuild_WebConsole(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t)
	res, err := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID:          "P-1",
		Title:              "web console issue",
		OpenedByIdentityID: "user:hayang",
		Origin:             discussion.OriginWebConsole,
		PrimaryChannelHint: "web",
		Actor:              observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" {
		t.Fatal("sync-build must populate conversation_id")
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.ConversationID() != res.ConversationID {
		t.Fatal("issue.conversation_id should match")
	}
	if got.Origin() != discussion.OriginWebConsole {
		t.Fatal("origin mismatch")
	}
	if h.countEvents(t, "issue.opened") != 1 || h.countEvents(t, "conversation.opened") != 1 {
		t.Fatal("expected both events emitted")
	}
}

func TestOpen_SyncBuild_NilOpener_FailsAndRolls(t *testing.T) {
	h := newHarness(t)
	svc := NewIssueLifecycleService(h.db, h.issueRepo, nil, nil, h.sink, h.gen, h.clk) // nil opener
	_, err := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID:          "P-1",
		Title:              "x",
		OpenedByIdentityID: "user:h",
		Origin:             discussion.OriginWebConsole,
		Actor:              observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected err on nil opener")
	}
	// Both events + rows must be absent (rolled back)
	if h.countEvents(t, "issue.opened") != 0 {
		t.Fatal("issue.opened should not be emitted")
	}
}

// failingOpener simulates a downstream Conversation BC failure inside the
// tx to verify Issue + emit both roll back.
type failingOpener struct{}

func (failingOpener) OpenIssueConversation(_ context.Context, _ OpenIssueConversationInput) (conversation.ConversationID, error) {
	return "", errors.New("conv build failed")
}

func TestOpen_SyncBuild_ConvFails_RollsBack(t *testing.T) {
	h := newHarness(t)
	svc := NewIssueLifecycleService(h.db, h.issueRepo, failingOpener{}, nil, h.sink, h.gen, h.clk)
	_, err := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID:          "P-1",
		Title:              "x",
		OpenedByIdentityID: "user:h",
		Origin:             discussion.OriginWebConsole,
		Actor:              observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected err")
	}
	// Verify no issue row + no event row
	row := h.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM issues`)
	var c int
	_ = row.Scan(&c)
	if c != 0 {
		t.Fatalf("expected 0 issues, got %d", c)
	}
	if h.countEvents(t, "issue.opened") != 0 {
		t.Fatal("issue.opened should not have committed")
	}
}

func TestOpen_RejectsInvalidInputs(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t)
	cases := []struct {
		name string
		cmd  OpenIssueCommand
	}{
		{"bad_actor", OpenIssueCommand{
			ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
			Origin: discussion.OriginCLI, Actor: "BAD",
		}},
		{"bad_origin", OpenIssueCommand{
			ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
			Origin: "bogus", Actor: observability.Actor("user:h"),
		}},
		{"empty_proj", OpenIssueCommand{
			ProjectID: "", Title: "t", OpenedByIdentityID: "user:h",
			Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
		}},
		{"empty_title", OpenIssueCommand{
			ProjectID: "P-1", Title: "", OpenedByIdentityID: "user:h",
			Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
		}},
		{"empty_opener", OpenIssueCommand{
			ProjectID: "P-1", Title: "t", OpenedByIdentityID: "",
			Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.Open(context.Background(), c.cmd); err == nil {
				t.Fatal("expected err")
			}
		})
	}
}

func TestOpen_ProjectExistenceCheckFails(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t).WithProjectExistenceChecker(stubProjectChecker{known: map[string]bool{}})
	_, err := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-MISSING", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}

	// Checker raises error
	svc2 := h.lifecycle(t).WithProjectExistenceChecker(stubProjectChecker{err: errors.New("db down")})
	_, err = svc2.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected db err to surface")
	}
}

func TestWithdraw_HappyAndAlreadyWithdrawn(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t).WithProjectExistenceChecker(stubProjectChecker{known: map[string]bool{"P-1": true}})
	res, _ := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := svc.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID:     res.IssueID,
		Reason:      "dup",
		Message:     "of #5",
		WithdrawnBy: "user:h",
		Actor:       observability.Actor("user:h"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Status() != discussion.StatusWithdrawn {
		t.Fatalf("status: %s", got.Status())
	}
	if h.countEvents(t, "issue.withdrawn") != 1 {
		t.Fatal("issue.withdrawn not emitted")
	}
	// Re-withdraw blocked at AR
	_, err := svc.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID: res.IssueID, Reason: "x", Message: "y", WithdrawnBy: "user:h", Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, discussion.ErrIssueWithdrawn) {
		t.Fatalf("expected ErrIssueWithdrawn, got %v", err)
	}
}

func TestWithdraw_RejectsInvalidInputs(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t)
	cases := []WithdrawIssueCommand{
		{IssueID: "X", Reason: "", Message: "m", WithdrawnBy: "u", Actor: observability.Actor("user:h")},
		{IssueID: "X", Reason: "r", Message: "", WithdrawnBy: "u", Actor: observability.Actor("user:h")},
		{IssueID: "X", Reason: "r", Message: "m", WithdrawnBy: "", Actor: observability.Actor("user:h")},
		{IssueID: "", Reason: "r", Message: "m", WithdrawnBy: "u", Actor: observability.Actor("user:h")},
		{IssueID: "X", Reason: "r", Message: "m", WithdrawnBy: "u", Actor: "BAD"},
	}
	for i, c := range cases {
		if _, err := svc.Withdraw(context.Background(), c); err == nil {
			t.Errorf("case %d: expected err", i)
		}
	}
	// Withdraw on missing id
	_, err := svc.Withdraw(context.Background(), WithdrawIssueCommand{
		IssueID:     "ghost",
		Reason:      "r",
		Message:     "m",
		WithdrawnBy: "u",
		Actor:       observability.Actor("user:h"),
	})
	if !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected ErrIssueNotFound, got %v", err)
	}
}

func TestRecordDiscussionStart_Transitions(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t).WithProjectExistenceChecker(stubProjectChecker{known: map[string]bool{"P-1": true}})
	res, _ := svc.Open(context.Background(), OpenIssueCommand{
		ProjectID: "P-1", Title: "t", OpenedByIdentityID: "user:h",
		Origin: discussion.OriginCLI, Actor: observability.Actor("user:h"),
	})
	if _, err := svc.RecordDiscussionStart(context.Background(), RecordDiscussionStartCommand{
		IssueID:               res.IssueID,
		FirstMessageID:        "MSG-1",
		FirstSenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:                 observability.Actor("user:peer"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.issueRepo.FindByID(context.Background(), res.IssueID)
	if got.Status() != discussion.StatusUnderDiscussion {
		t.Fatalf("status: %s", got.Status())
	}
	if h.countEvents(t, "issue.discussion_started") != 1 {
		t.Fatal("event not emitted")
	}
	// Idempotent
	if _, err := svc.RecordDiscussionStart(context.Background(), RecordDiscussionStartCommand{
		IssueID:               res.IssueID,
		FirstMessageID:        "MSG-2",
		FirstSenderIdentityID: conversation.IdentityRef("user:peer"),
		Actor:                 observability.Actor("user:peer"),
	}); err != nil {
		t.Fatal("idempotent")
	}
	if h.countEvents(t, "issue.discussion_started") != 1 {
		t.Fatal("event should not double-emit on idempotent call")
	}
}

func TestRecordDiscussionStart_RejectsInvalidInputs(t *testing.T) {
	h := newHarness(t)
	svc := h.lifecycle(t)
	cases := []RecordDiscussionStartCommand{
		{IssueID: "", FirstMessageID: "M", FirstSenderIdentityID: "user:p", Actor: observability.Actor("user:h")},
		{IssueID: "X", FirstMessageID: "", FirstSenderIdentityID: "user:p", Actor: observability.Actor("user:h")},
		{IssueID: "X", FirstMessageID: "M", FirstSenderIdentityID: "", Actor: observability.Actor("user:h")},
		{IssueID: "X", FirstMessageID: "M", FirstSenderIdentityID: "user:p", Actor: "BAD"},
	}
	for i, c := range cases {
		if _, err := svc.RecordDiscussionStart(context.Background(), c); err == nil {
			t.Errorf("case %d: expected err", i)
		}
	}
}

func TestIssueConversationOpener_FailsWithoutDeps(t *testing.T) {
	// Construct with nils to verify defensive path
	opener := &IssueConversationOpener{}
	_, err := opener.OpenIssueConversation(context.Background(), OpenIssueConversationInput{
		Title: "x", IssueID: "X", Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal("expected dependency err")
	}
}

func TestIssueConversationOpener_RejectsBadActorAndEmptyIssueID(t *testing.T) {
	h := newHarness(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, h.clk)
	if _, err := opener.OpenIssueConversation(context.Background(), OpenIssueConversationInput{
		Title: "x", IssueID: "ID", Actor: "BAD",
	}); err == nil {
		t.Fatal("expected bad actor err")
	}
	if _, err := opener.OpenIssueConversation(context.Background(), OpenIssueConversationInput{
		Title: "x", IssueID: "", Actor: observability.Actor("user:h"),
	}); err == nil {
		t.Fatal("expected empty issue_id err")
	}
}

func TestIssueConversationOpener_NilClockDefaults(t *testing.T) {
	h := newHarness(t)
	opener := NewIssueConversationOpener(h.convRepo, h.sink, h.gen, nil)
	if opener.clock == nil {
		t.Fatal("nil clock should default")
	}
}

// TestIssueLifecycleService_NilClockDefaults verifies the lifecycle
// service nil-clock branch.
func TestIssueLifecycleService_NilClockDefaults(t *testing.T) {
	h := newHarness(t)
	s := NewIssueLifecycleService(h.db, h.issueRepo, nil, nil, h.sink, h.gen, nil)
	if s.clock == nil {
		t.Fatal("nil clock should default")
	}
}

// TestIssueCommentService_NilClockDefaults verifies the comment service
// nil-clock branch.
func TestIssueCommentService_NilClockDefaults(t *testing.T) {
	h := newHarness(t)
	lifecycle := NewIssueLifecycleService(h.db, h.issueRepo, nil, nil, h.sink, h.gen, nil)
	s := NewIssueCommentService(h.issueRepo, h.convRepo, h.msgRepo, nil, lifecycle, nil)
	if s.clock == nil {
		t.Fatal("nil clock should default")
	}
}

// TestIssueBindConversationService_NilClockDefaults verifies the bind
// service nil-clock branch.
func TestIssueBindConversationService_NilClockDefaults(t *testing.T) {
	h := newHarness(t)
	s := NewIssueBindConversationService(h.db, h.issueRepo, h.convRepo, nil, h.sink, nil)
	if s.clock == nil {
		t.Fatal("nil clock should default")
	}
}

// TestIssueLinkConversationService_NilClockDefaults verifies the link
// service nil-clock branch.
func TestIssueLinkConversationService_NilClockDefaults(t *testing.T) {
	h := newHarness(t)
	s := NewIssueLinkConversationService(h.db, h.issueRepo, h.convRepo, nil)
	if s.clock == nil {
		t.Fatal("nil clock should default")
	}
}
