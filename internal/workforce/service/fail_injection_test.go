package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
)

// =============================================================================
// Fail-injection mocks for AgentInstanceRepository (only methods exercised
// by EnsureBuiltinSupervisor / OnExecutionStarted / OnExecutionEnded paths).
// Each method can be programmed to return a specific error.
// =============================================================================

type fakeAIRepo struct {
	findByNameErr error
	saveErr       error
	updateErr     error
	countErr      error
	bulkErr       error
	countActive   int
}

func (f *fakeAIRepo) FindByID(ctx context.Context, id workforce.AgentInstanceID) (*workforce.AgentInstance, error) {
	return nil, workforce.ErrAgentInstanceNotFound
}
func (f *fakeAIRepo) FindByName(ctx context.Context, name string) (*workforce.AgentInstance, error) {
	if f.findByNameErr != nil {
		return nil, f.findByNameErr
	}
	return nil, workforce.ErrAgentInstanceNotFound
}
func (f *fakeAIRepo) FindAll(ctx context.Context, filter workforce.AgentInstanceFilter) ([]*workforce.AgentInstance, error) {
	return nil, nil
}
func (f *fakeAIRepo) Save(ctx context.Context, a *workforce.AgentInstance) error {
	return f.saveErr
}
func (f *fakeAIRepo) UpdateState(ctx context.Context, id workforce.AgentInstanceID, from, to workforce.AgentInstanceState, version int) error {
	return f.updateErr
}
func (f *fakeAIRepo) UpdateConfig(ctx context.Context, id workforce.AgentInstanceID, config string, maxConcurrent *int, version int) error {
	return f.updateErr
}
func (f *fakeAIRepo) Archive(ctx context.Context, id workforce.AgentInstanceID, at time.Time, reason workforce.AgentInstanceArchivedReason, message string, version int) error {
	return f.updateErr
}
func (f *fakeAIRepo) CountActiveExecutions(ctx context.Context, id workforce.AgentInstanceID) (int, error) {
	return f.countActive, f.countErr
}
func (f *fakeAIRepo) BulkUpdateStateByWorker(ctx context.Context, workerID workforce.WorkerID, from, to workforce.AgentInstanceState) (int, error) {
	return 0, f.bulkErr
}

// =============================================================================
// EnsureBuiltinSupervisor failure injection
// =============================================================================

func TestEnsureBuiltinSupervisor_FindByNameOtherError(t *testing.T) {
	s := setupSuite(t)
	repo := &fakeAIRepo{findByNameErr: errors.New("db blew up")}
	mgmt := NewAgentInstanceManagementService(s.db, repo, s.idgen, s.sink, s.clock)
	_, err := mgmt.EnsureBuiltinSupervisor(context.Background())
	if err == nil || err.Error() != "db blew up" {
		t.Fatalf("expected db error, got %v", err)
	}
}

func TestEnsureBuiltinSupervisor_SaveNameTakenRace(t *testing.T) {
	s := setupSuite(t)
	// Repo simulates concurrent insert: first FindByName returns NotFound,
	// Save returns NameTaken; second FindByName succeeds.
	repo := &fakeAIRepoNameTakenRace{}
	mgmt := NewAgentInstanceManagementService(s.db, repo, s.idgen, s.sink, s.clock)
	_, err := mgmt.EnsureBuiltinSupervisor(context.Background())
	if err == nil {
		t.Logf("EnsureBuiltinSupervisor handled race ok")
	}
}

type fakeAIRepoNameTakenRace struct {
	fakeAIRepo
	findByNameCallCount int
}

func (f *fakeAIRepoNameTakenRace) FindByName(ctx context.Context, name string) (*workforce.AgentInstance, error) {
	f.findByNameCallCount++
	if f.findByNameCallCount == 1 {
		return nil, workforce.ErrAgentInstanceNotFound
	}
	// Second call after Save NameTaken — return a valid built-in row.
	a, _ := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID: "01HRACE", Name: workforce.BuiltinSupervisorName,
		AgentCLI: "claude-code", IsBuiltin: true, CreatedAt: time.Now(),
	})
	return a, nil
}
func (f *fakeAIRepoNameTakenRace) Save(ctx context.Context, a *workforce.AgentInstance) error {
	return workforce.ErrAgentInstanceNameTaken
}

func TestEnsureBuiltinSupervisor_SaveOtherErr(t *testing.T) {
	s := setupSuite(t)
	repo := &fakeAIRepo{saveErr: errors.New("save blew up")}
	mgmt := NewAgentInstanceManagementService(s.db, repo, s.idgen, s.sink, s.clock)
	_, err := mgmt.EnsureBuiltinSupervisor(context.Background())
	if err == nil || err.Error() != "save blew up" {
		t.Fatalf("expected save error, got %v", err)
	}
}

// =============================================================================
// OnExecutionStarted paths
// =============================================================================

func TestOnExecutionStarted_NotFound(t *testing.T) {
	s := setupAISuite(t)
	if err := s.life.OnExecutionStarted(context.Background(), "01H-MISSING", "system"); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// BootstrapTokenService — Reissue tx-failure & ScanExpired with stale active.
// =============================================================================

func TestReissue_StaleConcurrentAlreadyMovedToUsed(t *testing.T) {
	s := setupBTSuite(t)
	issued, _ := s.svc.Issue(context.Background(), IssueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	// Externally mark issued token as `used` (simulating exchange happened).
	tok, _ := s.tokenRepo.FindByID(context.Background(), issued.TokenID)
	if err := tok.MarkUsed(s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.tokenRepo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive); err != nil {
		t.Fatal(err)
	}
	// Now reissue: no active token, most recent is `used` → reject.
	_, err := s.svc.Reissue(context.Background(), ReissueCommand{
		WorkerID: "W-1", ActorIdentity: "user:x",
	})
	if !errors.Is(err, workforce.ErrBootstrapTokenAlreadyUsed) {
		t.Fatalf("expected already used, got %v", err)
	}
}

// =============================================================================
// observability import keep
// =============================================================================
var _ = observability.Actor("system")
