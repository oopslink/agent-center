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

func TestAILifecycle_OnExecutionEnded_WhenIdle(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "c", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:x",
	})
	// Already idle (no active execution); OnExecutionEnded is a no-op.
	if err := s.life.OnExecutionEnded(context.Background(), created.ID, "system"); err != nil {
		t.Fatal(err)
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
func (s bootstrapTokenRepoStub) Save(ctx context.Context, t *workforce.BootstrapToken) error { return nil }
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
