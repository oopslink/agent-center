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

// btSuite extends the workforce service suite with a BootstrapTokenRepo +
// BootstrapTokenService wired up.
type btSuite struct {
	*suite
	tokenRepo *wfsqlite.BootstrapTokenRepo
	svc       *BootstrapTokenService
}

func setupBTSuite(t *testing.T) *btSuite {
	t.Helper()
	s := setupSuite(t)
	repo := wfsqlite.NewBootstrapTokenRepo(s.db)
	return &btSuite{
		suite:     s,
		tokenRepo: repo,
		svc:       NewBootstrapTokenService(s.db, repo, s.idgen, s.sink, s.clock, 0),
	}
}

func TestBootstrapTokenService_Issue_Happy(t *testing.T) {
	s := setupBTSuite(t)
	res, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if res.TokenValue == "" {
		t.Fatal("TokenValue plaintext should be returned")
	}
	if string(res.TokenID) == "" {
		t.Fatal("TokenID should be populated")
	}
	if res.ExpiresAt.Sub(s.clock.Now()) != DefaultBootstrapTokenTTL {
		t.Fatalf("expires_at TTL: %v", res.ExpiresAt.Sub(s.clock.Now()))
	}
	// Verify plaintext is NOT what was stored (only hash).
	tok, err := s.tokenRepo.FindByID(context.Background(), res.TokenID)
	if err != nil {
		t.Fatal(err)
	}
	if tok.ValueHash() == res.TokenValue {
		t.Fatal("DB stored plaintext (should be hash)")
	}
	if tok.ValueHash() != workforce.HashTokenValue(res.TokenValue) {
		t.Fatal("ValueHash does not match HashTokenValue(plaintext)")
	}
	// Event emitted.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-1"},
	})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type() != "workforce.worker.bootstrap_token.issued" {
		t.Fatalf("event type: %s", events[0].Type())
	}
}

func TestBootstrapTokenService_Issue_DuplicateActive(t *testing.T) {
	s := setupBTSuite(t)
	if _, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	// Second issue without reissue should fail (active-per-worker constraint).
	_, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrBootstrapTokenActiveExists) {
		t.Fatalf("expected active-exists, got %v", err)
	}
}

func TestBootstrapTokenService_Issue_BadActor(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID:      "W-1",
		ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestBootstrapTokenService_Issue_EmptyWorkerID(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID:      "",
		ActorIdentity: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestBootstrapTokenService_Reissue_RevokesOldAndMintsNew(t *testing.T) {
	s := setupBTSuite(t)
	// Initial issue.
	issued, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Reissue.
	re, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Reissue: %v", err)
	}
	if re.NewTokenID == issued.TokenID {
		t.Fatal("new token id should differ")
	}
	if re.OldTokenID != issued.TokenID {
		t.Fatalf("old_id mismatch: got %s want %s", re.OldTokenID, issued.TokenID)
	}
	if re.OldStatusAtReissue != workforce.BootstrapTokenActive {
		t.Fatalf("old_status: %s", re.OldStatusAtReissue)
	}
	if re.NewTokenValue == "" {
		t.Fatal("new plaintext should be returned")
	}
	// Old token is now revoked.
	oldTok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if oldTok.Status() != workforce.BootstrapTokenRevoked {
		t.Fatalf("old status: %s", oldTok.Status())
	}
	if oldTok.RevokedReason() != workforce.BootstrapTokenRevokedReasonReissueSuperseded {
		t.Fatalf("revoked reason: %s", oldTok.RevokedReason())
	}
	// New token is active.
	newTok, _ := s.tokenRepo.FindByID(context.Background(), re.NewTokenID)
	if newTok.Status() != workforce.BootstrapTokenActive {
		t.Fatalf("new status: %s", newTok.Status())
	}
}

func TestBootstrapTokenService_Reissue_RejectsUsed(t *testing.T) {
	s := setupBTSuite(t)
	issued, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mark token used via direct repo path.
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if err := tok.MarkUsed(s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.tokenRepo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive); err != nil {
		t.Fatal(err)
	}
	// Reissue should reject.
	_, err = s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrBootstrapTokenAlreadyUsed) {
		t.Fatalf("expected already-used, got %v", err)
	}
}

func TestBootstrapTokenService_Reissue_AfterExpired_OK(t *testing.T) {
	s := setupBTSuite(t)
	// Issue and expire the token via scanner path.
	if _, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(DefaultBootstrapTokenTTL + time.Second)
	if _, err := s.svc.ScanExpired(context.Background(), "system"); err != nil {
		t.Fatal(err)
	}
	// Reissue after expired should succeed (per ADR-0023 reissue rules).
	re, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Reissue after expired: %v", err)
	}
	if re.OldStatusAtReissue != workforce.BootstrapTokenExpired {
		t.Fatalf("old_status: %s", re.OldStatusAtReissue)
	}
}

func TestBootstrapTokenService_Reissue_NoPriorToken(t *testing.T) {
	s := setupBTSuite(t)
	// No issue first; reissue on a fresh worker — succeeds (acts like issue).
	re, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-NEW", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Reissue no-prior: %v", err)
	}
	if re.OldTokenID != "" {
		t.Fatalf("expected empty OldTokenID, got %s", re.OldTokenID)
	}
}

func TestBootstrapTokenService_Revoke_Happy(t *testing.T) {
	s := setupBTSuite(t)
	issued, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	evID, err := s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID:       issued.TokenID,
		Reason:        workforce.BootstrapTokenRevokedReasonManual,
		Message:       "user requested",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if evID == "" {
		t.Fatal("revoke event id missing")
	}
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if tok.Status() != workforce.BootstrapTokenRevoked {
		t.Fatalf("status: %s", tok.Status())
	}
	if tok.RevokedMessage() != "user requested" {
		t.Fatalf("message: %s", tok.RevokedMessage())
	}
}

func TestBootstrapTokenService_Revoke_AlreadyTerminal(t *testing.T) {
	s := setupBTSuite(t)
	issued, _ := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	_, err := s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID: issued.TokenID, Reason: workforce.BootstrapTokenRevokedReasonManual,
		Message: "first", ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Second revoke on already-revoked must fail.
	_, err = s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID: issued.TokenID, Reason: workforce.BootstrapTokenRevokedReasonManual,
		Message: "second", ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrBootstrapTokenNotActive) {
		t.Fatalf("expected not-active, got %v", err)
	}
}

func TestBootstrapTokenService_ScanExpired(t *testing.T) {
	s := setupBTSuite(t)
	issued, _ := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	s.clock.Advance(DefaultBootstrapTokenTTL + time.Minute)
	res, err := s.svc.ScanExpired(context.Background(), "system")
	if err != nil {
		t.Fatalf("ScanExpired: %v", err)
	}
	if len(res.ExpiredTokenIDs) != 1 || res.ExpiredTokenIDs[0] != issued.TokenID {
		t.Fatalf("expired ids: %v", res.ExpiredTokenIDs)
	}
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if tok.Status() != workforce.BootstrapTokenExpired {
		t.Fatalf("status: %s", tok.Status())
	}
}

func TestBootstrapTokenService_ScanExpired_NoExpired(t *testing.T) {
	s := setupBTSuite(t)
	if _, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	// Don't advance clock; nothing to expire.
	res, err := s.svc.ScanExpired(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ExpiredTokenIDs) != 0 {
		t.Fatalf("expected 0, got %d", len(res.ExpiredTokenIDs))
	}
}

// Concurrency-style test: two reissues in parallel must result in exactly
// one new active token (DB unique index is the ultimate guard).
func TestBootstrapTokenService_Reissue_ConcurrentRace(t *testing.T) {
	s := setupBTSuite(t)
	if _, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	type out struct {
		res ReissueResult
		err error
	}
	ch := make(chan out, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r, err := s.svc.Reissue(context.Background(), ReissueCommand{
				WorkerID: "W-1", ActorIdentity: "user:hayang",
			})
			ch <- out{r, err}
		}()
	}
	results := []out{<-ch, <-ch}
	successes := 0
	for _, r := range results {
		if r.err == nil {
			successes++
		}
	}
	// At least one succeeds. We accept 1 or 2 — but if both succeed there
	// should still be exactly one active token at end.
	if successes == 0 {
		t.Fatalf("expected ≥1 reissue success, got 0: %+v / %+v", results[0], results[1])
	}
	active, _ := s.tokenRepo.FindByWorkerID(context.Background(), "W-1", workforce.BootstrapTokenActive)
	if len(active) != 1 {
		t.Fatalf("expected exactly 1 active token after race, got %d", len(active))
	}
}
