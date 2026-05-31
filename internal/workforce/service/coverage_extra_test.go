package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

// =============================================================================
// WorkerConfigService extra paths
// =============================================================================

func TestNewWorkerConfigService_NilClock(t *testing.T) {
	s := setupSuite(t)
	cfg := NewWorkerConfigService(s.db, s.workerRepo, s.sink, nil)
	if cfg == nil {
		t.Fatal()
	}
}

func TestSetCapabilityEnabled_BadActor(t *testing.T) {
	s := setupCfgSuite(t)
	_, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID: "W-1", AgentCLI: "x", Enabled: true, Version: 1,
		ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestSetCapabilityEnabled_EmptyFields(t *testing.T) {
	s := setupCfgSuite(t)
	_, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID: "", AgentCLI: "x", Enabled: true, Version: 1,
		ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
	_, err = s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID: "W-1", AgentCLI: "", Enabled: true, Version: 1,
		ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestSetCapabilityEnabled_NotFound(t *testing.T) {
	s := setupCfgSuite(t)
	_, err := s.cfg.SetCapabilityEnabled(context.Background(), SetCapabilityEnabledCommand{
		WorkerID: "W-NEVER", AgentCLI: "x", Enabled: true, Version: 1,
		ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// =============================================================================
// AgentInstanceManagementService extra paths
// =============================================================================

func TestNewAgentInstanceManagementService_NilClock(t *testing.T) {
	s := setupSuite(t)
	mgmt := NewAgentInstanceManagementService(s.db, nil, s.idgen, s.sink, nil)
	if mgmt == nil {
		t.Fatal()
	}
}

func TestUpdateConfig_NotFound(t *testing.T) {
	s := setupAISuite(t)
	cfg := "{}"
	_, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: "01H-NEVER", Config: &cfg, Version: 1, ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestUpdateConfig_BadActor(t *testing.T) {
	s := setupAISuite(t)
	cfg := "{}"
	_, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: "01H", Config: &cfg, Version: 1, ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestUpdateConfig_AfterArchive(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "c", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	_, _ = s.mgmt.Archive(context.Background(), ArchiveAgentInstanceCommand{
		ID: created.ID, Reason: workforce.AgentInstanceArchivedReasonManual,
		Message: "x", Version: 1, ActorIdentity: "user:x",
	})
	cfg := "{}"
	_, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: created.ID, Config: &cfg, Version: 2, ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchive_BadActor(t *testing.T) {
	s := setupAISuite(t)
	_, err := s.mgmt.Archive(context.Background(), ArchiveAgentInstanceCommand{
		ID: "x", Reason: workforce.AgentInstanceArchivedReasonManual,
		Message: "m", Version: 1, ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCreate_BadInputs(t *testing.T) {
	s := setupAISuite(t)
	_, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "x", AgentCLI: "", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
	_, err = s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "", AgentCLI: "x", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// =============================================================================
// AgentInstanceLifecycleService extra paths
// =============================================================================

func TestNewAgentInstanceLifecycleService_NilClock(t *testing.T) {
	s := setupSuite(t)
	life := NewAgentInstanceLifecycleService(s.db, nil, s.sink, nil)
	if life == nil {
		t.Fatal()
	}
}

func TestAILifecycle_OnWorkerOffline_NoAgents(t *testing.T) {
	s := setupAISuite(t)
	// No agents on W-NONE; bulk update returns 0 affected.
	n, err := s.life.OnWorkerOffline(context.Background(), "W-NONE", "system")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestAILifecycle_OnWorkerOnline_NoAgents(t *testing.T) {
	s := setupAISuite(t)
	n, err := s.life.OnWorkerOnline(context.Background(), "W-NONE", "system")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal()
	}
}

// =============================================================================
// BootstrapTokenService extra paths
// =============================================================================

func TestNewBootstrapTokenService_NilClockAndZeroTTL(t *testing.T) {
	s := setupSuite(t)
	repo := bootstrapTokenRepoStub{}
	_ = NewBootstrapTokenService(s.db, repo, s.idgen, s.sink, nil, 0)
}

func TestRevoke_NotFound(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID: "01H-NEVER", Reason: workforce.BootstrapTokenRevokedReasonManual,
		Message: "x", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRevoke_BadActor(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID: "01H", Reason: workforce.BootstrapTokenRevokedReasonManual,
		Message: "x", ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestReissue_BadActor(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestReissue_EmptyWorkerID(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestScanExpired_BadActor(t *testing.T) {
	s := setupBTSuite(t)
	_, err := s.svc.ScanExpired(context.Background(), "bogus:x")
	if err == nil {
		t.Fatal()
	}
}

// AILifecycle: OnWorkerOnline tx failure via trigger.
func TestAILifecycle_OnWorkerOnline_TxFailure(t *testing.T) {
	s := setupAISuite(t)
	_, _ = s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "c2", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	_, _ = s.life.OnWorkerOffline(context.Background(), "W-1", "system")
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_ai_on BEFORE UPDATE ON agent_instances BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.life.OnWorkerOnline(context.Background(), "W-1", "system")
	if err == nil {
		t.Fatal()
	}
}

// =============================================================================
// Trigger-based tx failure injection for service emit/UpdateStatus paths
// =============================================================================

func TestBootstrapTokenService_Issue_TxFailure(t *testing.T) {
	s := setupBTSuite(t)
	// Block writes to bootstrap_tokens.
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_bt BEFORE INSERT ON bootstrap_tokens BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal("expected tx failure")
	}
}

func TestBootstrapTokenService_Revoke_TxFailure(t *testing.T) {
	s := setupBTSuite(t)
	issued, _ := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_bt_update BEFORE UPDATE ON bootstrap_tokens BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID: issued.TokenID, Reason: workforce.BootstrapTokenRevokedReasonManual,
		Message: "x", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestWorkerEnrollService_Exchange_SaveFailure(t *testing.T) {
	s := setupExSuite(t)
	issued, _ := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	// Block writes to workers (forces Save to fail).
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_w BEFORE INSERT ON workers BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: issued.TokenValue, WorkerID: "W-1", ActorIdentity: "worker:W-1",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestAIMgmt_Create_SaveFailure(t *testing.T) {
	s := setupAISuite(t)
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_ai BEFORE INSERT ON agent_instances BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "x", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestAILifecycle_OnExecutionStarted_TxFailure(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "x", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_ai_update BEFORE UPDATE ON agent_instances BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	err := s.life.OnExecutionStarted(context.Background(), created.ID, "system")
	if err == nil {
		t.Fatal()
	}
}

func TestWorkerConfigService_SetConfig_TxFailure(t *testing.T) {
	s := setupCfgSuite(t)
	w := seedActiveWorker(t, s, "W-TX")
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_w_update BEFORE UPDATE ON workers BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	newC := workforce.WorkerConcurrency{PerAgentType: 3}
	_, err := s.cfg.SetConfig(context.Background(), SetConfigCommand{
		WorkerID: "W-TX", Concurrency: &newC, Version: w.Version(),
		ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// Reissue happy path against expired token (oldStatus=expired path).
func TestReissue_FromRevokedPath(t *testing.T) {
	s := setupBTSuite(t)
	issued, _ := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	// Manually revoke via service (separate path so a non-used terminal
	// status exists for the next Reissue).
	_, _ = s.svc.Revoke(context.Background(), RevokeCommand{
		TokenID: issued.TokenID, Reason: workforce.BootstrapTokenRevokedReasonManual,
		Message: "x", ActorIdentity: "user:x",
	})
	// Reissue should succeed and record oldStatus=revoked.
	re, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if re.OldStatusAtReissue != workforce.BootstrapTokenRevoked {
		t.Fatalf("old_status: %s", re.OldStatusAtReissue)
	}
}

// Reissue tx failure during Save of new token.
func TestReissue_TxFailureOnSave(t *testing.T) {
	s := setupBTSuite(t)
	_, _ = s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_bt_insert BEFORE INSERT ON bootstrap_tokens BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// ScanExpired tx failure when emit fails (via trigger block on events table).
func TestScanExpired_EmitFailure(t *testing.T) {
	s := setupBTSuite(t)
	_, _ = s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	s.clock.Advance(DefaultBootstrapTokenTTL + time.Second)
	// Block events table inserts so emit fails inside the scan loop.
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_events BEFORE INSERT ON events BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.svc.ScanExpired(context.Background(), "system")
	if err == nil {
		t.Fatal()
	}
}

// AIMgmt UpdateConfig tx failure: trigger on agent_instances UPDATE.
func TestAIMgmt_UpdateConfig_TxFailure(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "c", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_ai_upd BEFORE UPDATE ON agent_instances BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	cfg := `{"x":2}`
	_, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: created.ID, Config: &cfg, Version: 1, ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// Exchange events table failure.
func TestExchange_EventEmitFailure(t *testing.T) {
	s := setupExSuite(t)
	issued, _ := s.tokenSvc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	// After Issue created its event, block further events table writes.
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_events BEFORE INSERT ON events BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.enrollV2.Exchange(context.Background(), ExchangeRequest{
		TokenValue: issued.TokenValue, WorkerID: "W-1", ActorIdentity: "worker:W-1",
	})
	if err == nil {
		t.Fatal()
	}
}

// Issue emit failure: block events table.
func TestIssue_EmitFailure(t *testing.T) {
	s := setupBTSuite(t)
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_events BEFORE INSERT ON events BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// OnWorkerOffline tx failure via trigger on agent_instances.
func TestAILifecycle_OnWorkerOffline_TxFailure(t *testing.T) {
	s := setupAISuite(t)
	_, _ = s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "c", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if _, err := s.db.ExecContext(context.Background(),
		`CREATE TEMP TRIGGER block_ai_off BEFORE UPDATE ON agent_instances BEGIN
		   SELECT RAISE(ABORT, 'blocked');
		 END`); err != nil {
		t.Fatal(err)
	}
	_, err := s.life.OnWorkerOffline(context.Background(), "W-1", "system")
	if err == nil {
		t.Fatal()
	}
}

// ScanExpired race: token externally moved to `used` between FindExpired and
// UpdateStatus; scanner should skip silently (continue).
func TestScanExpired_RaceToUsed(t *testing.T) {
	s := setupBTSuite(t)
	issued, _ := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	s.clock.Advance(DefaultBootstrapTokenTTL + time.Second)
	// Externally mark the token as used (concurrent exchange).
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if err := tok.MarkUsed(s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	_ = s.tokenRepo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive)
	// Now run ScanExpired — the token is no longer active; UpdateStatus
	// inside scanner should detect status conflict and skip.
	res, err := s.svc.ScanExpired(context.Background(), "system")
	if err != nil {
		t.Fatalf("ScanExpired: %v", err)
	}
	// 0 expired in result (race skipped the token).
	if len(res.ExpiredTokenIDs) != 0 {
		t.Fatalf("expected 0 expired, got %d", len(res.ExpiredTokenIDs))
	}
}

// bootstrapTokenRepoStub satisfies the workforce.BootstrapTokenRepository
// interface for constructor wiring tests only.
type bootstrapTokenRepoStub struct{}

func (s bootstrapTokenRepoStub) FindByID(ctx context.Context, id workforce.BootstrapTokenID) (*workforce.BootstrapToken, error) {
	return nil, nil
}
func (s bootstrapTokenRepoStub) FindByValueHash(ctx context.Context, hash string) (*workforce.BootstrapToken, error) {
	return nil, nil
}
func (s bootstrapTokenRepoStub) FindByWorkerID(ctx context.Context, wid workforce.WorkerID, statuses ...workforce.BootstrapTokenStatus) ([]*workforce.BootstrapToken, error) {
	return nil, nil
}
func (s bootstrapTokenRepoStub) FindActiveByWorkerForUpdate(ctx context.Context, wid workforce.WorkerID) (*workforce.BootstrapToken, error) {
	return nil, nil
}
func (s bootstrapTokenRepoStub) Save(ctx context.Context, t *workforce.BootstrapToken) error {
	return nil
}
func (s bootstrapTokenRepoStub) UpdateStatus(ctx context.Context, t *workforce.BootstrapToken, from workforce.BootstrapTokenStatus) error {
	return nil
}
func (s bootstrapTokenRepoStub) FindExpired(ctx context.Context, before time.Time) ([]*workforce.BootstrapToken, error) {
	return nil, nil
}

// =============================================================================
// Exchange extra paths
// =============================================================================

func TestExchange_NilClock(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollServiceV2(s.db, s.workerRepo, nil, s.sink, nil)
	if enroll == nil {
		t.Fatal()
	}
}
