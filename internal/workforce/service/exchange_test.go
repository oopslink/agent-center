package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// exSuite extends the suite with a v2 enroll service (with tokenRepo) +
// BootstrapTokenService for setting up issued tokens.
type exSuite struct {
	*suite
	tokenRepo *wfsqlite.BootstrapTokenRepo
	tokenSvc  *BootstrapTokenService
	enrollV2  *WorkerEnrollService
}

func setupExSuite(t *testing.T) *exSuite {
	t.Helper()
	s := setupSuite(t)
	repo := wfsqlite.NewBootstrapTokenRepo(s.db)
	return &exSuite{
		suite:     s,
		tokenRepo: repo,
		tokenSvc:  NewBootstrapTokenService(s.db, repo, s.idgen, s.sink, s.clock, 0),
		enrollV2:  NewWorkerEnrollServiceV2(s.db, s.workerRepo, repo, s.sink, s.clock),
	}
}

func TestExchange_Happy(t *testing.T) {
	s := setupExSuite(t)
	issued, err := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: issued.TokenValue,
		WorkerID:   "W-1",
		Capabilities: []workforce.Capability{
			{AgentCLI: "claude-code", Detected: true, Enabled: true},
		},
		ActorIdentity: "worker:W-1",
	})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if resp.WorkerID != "W-1" {
		t.Fatalf("worker_id: %s", resp.WorkerID)
	}
	if resp.SessionToken == "" {
		t.Fatal("session_token must be returned")
	}
	if resp.EnrolledEventID == "" || resp.UsedEventID == "" {
		t.Fatal("events should be emitted")
	}
	// Worker saved with capabilities.
	w, err := s.workerRepo.FindByID(context.Background(), "W-1")
	if err != nil {
		t.Fatal(err)
	}
	if caps := w.CapabilityList(); len(caps) != 1 || caps[0].AgentCLI != "claude-code" {
		t.Fatalf("caps: %v", caps)
	}
	// Token now used.
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if tok.Status() != workforce.BootstrapTokenUsed {
		t.Fatalf("token status: %s", tok.Status())
	}
	// Both events present in events table.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-1"},
	})
	types := map[observability.EventType]int{}
	for _, e := range events {
		types[e.Type()]++
	}
	if types[observability.EventType("workforce.worker.bootstrap_token.issued")] != 1 ||
		types[observability.EventType("workforce.worker.bootstrap_token.used")] != 1 ||
		types[observability.EventType("workforce.worker.enrolled")] != 1 {
		t.Fatalf("events: %+v", types)
	}
}

func TestExchange_TokenNotFound(t *testing.T) {
	s := setupExSuite(t)
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue:    "no-such-plain-text",
		WorkerID:      "W-1",
		ActorIdentity: "worker:W-1",
	})
	if !errors.Is(err, workforce.ErrBootstrapTokenNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestExchange_TokenExpired(t *testing.T) {
	s := setupExSuite(t)
	issued, _ := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	// Fast-forward past TTL.
	s.clock.Advance(DefaultBootstrapTokenTTL + time.Second)
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue:    issued.TokenValue,
		WorkerID:      "W-1",
		ActorIdentity: "worker:W-1",
	})
	if !errors.Is(err, ErrExchangeTokenExpired) {
		t.Fatalf("expected token expired, got %v", err)
	}
}

func TestExchange_TokenAlreadyUsed(t *testing.T) {
	s := setupExSuite(t)
	issued, _ := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	// First exchange succeeds.
	if _, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue:    issued.TokenValue,
		WorkerID:      "W-1",
		ActorIdentity: "worker:W-1",
	}); err != nil {
		t.Fatal(err)
	}
	// Second exchange fails (token now used).
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue:    issued.TokenValue,
		WorkerID:      "W-1",
		ActorIdentity: "worker:W-1",
	})
	if !errors.Is(err, workforce.ErrBootstrapTokenNotActive) {
		t.Fatalf("expected not active, got %v", err)
	}
}

func TestExchange_WorkerIDMismatch(t *testing.T) {
	s := setupExSuite(t)
	issued, _ := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue:    issued.TokenValue,
		WorkerID:      "W-DIFFERENT",
		ActorIdentity: "worker:W-DIFFERENT",
	})
	if !errors.Is(err, ErrExchangeWorkerIDMismatch) {
		t.Fatalf("expected worker_id mismatch, got %v", err)
	}
	// And the token should still be active (not marked used since tx rolls back).
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if tok.Status() != workforce.BootstrapTokenActive {
		t.Fatalf("token should remain active after rollback, got %s", tok.Status())
	}
}

func TestExchange_WorkerAlreadyEnrolled(t *testing.T) {
	s := setupExSuite(t)
	issued, _ := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	// First exchange (worker enrolled).
	if _, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: issued.TokenValue, WorkerID: "W-1", ActorIdentity: "worker:W-1",
	}); err != nil {
		t.Fatal(err)
	}
	// Issue a fresh token for the same W-1 (allowed; first one is now `used`
	// so reissue rules don't apply — straight issue path).
	// But wait: the first issue's token is now `used`, so the worker has no
	// active token. We can directly Issue another one.
	issued2, err := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Now Exchange the second token — should fail because Worker W-1 row
	// already exists (single-enrollment per worker_id).
	_, err = s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: issued2.TokenValue, WorkerID: "W-1", ActorIdentity: "worker:W-1",
	})
	if !errors.Is(err, workforce.ErrWorkerAlreadyExists) {
		t.Fatalf("expected worker already exists, got %v", err)
	}
}

func TestExchange_NoTokenRepo(t *testing.T) {
	s := setupExSuite(t)
	// v1 service has no tokenRepo.
	v1 := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, s.clock)
	_, err := v1.Exchange(context.Background(), ExchangeRequest{
		TokenValue: "x", WorkerID: "W-1", ActorIdentity: "worker:W-1",
	})
	if !errors.Is(err, ErrEnrollServiceNoTokenRepo) {
		t.Fatalf("expected no-token-repo, got %v", err)
	}
}

func TestExchange_BadActor(t *testing.T) {
	s := setupExSuite(t)
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: "x", WorkerID: "W-1", ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExchange_EmptyToken(t *testing.T) {
	s := setupExSuite(t)
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: "", WorkerID: "W-1", ActorIdentity: "worker:W-1",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExchange_EmptyWorkerID(t *testing.T) {
	s := setupExSuite(t)
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: "x", WorkerID: "", ActorIdentity: "worker:W-1",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
