package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

type aiSuite struct {
	*suite
	aiRepo *wfsqlite.AgentInstanceRepo
	mgmt   *AgentInstanceManagementService
	life   *AgentInstanceLifecycleService
}

func setupAISuite(t *testing.T) *aiSuite {
	t.Helper()
	s := setupSuite(t)
	repo := wfsqlite.NewAgentInstanceRepo(s.db)
	return &aiSuite{
		suite:  s,
		aiRepo: repo,
		mgmt:   NewAgentInstanceManagementService(s.db, repo, s.idgen, s.sink, s.clock),
		life:   NewAgentInstanceLifecycleService(s.db, repo, s.sink, s.clock),
	}
}

func intP(v int) *int       { return &v }
func strP(s string) *string { return &s }

// =============================================================================
// Management Service
// =============================================================================

func TestAIMgmt_Create_Happy(t *testing.T) {
	s := setupAISuite(t)
	res, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name:          "coder-mbp",
		AgentCLI:      "claude-code",
		WorkerID:      "W-1",
		MaxConcurrent: intP(3),
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(res.ID) == "" {
		t.Fatal("id missing")
	}
	if res.EventID == "" {
		t.Fatal("event id missing")
	}
	a, _ := s.aiRepo.FindByID(context.Background(), res.ID)
	if a.Name() != "coder-mbp" {
		t.Fatalf("name: %s", a.Name())
	}
	if a.State() != workforce.AgentInstanceIdle {
		t.Fatalf("state: %s", a.State())
	}
	if a.MaxConcurrent() == nil || *a.MaxConcurrent() != 3 {
		t.Fatalf("max_concurrent: %v", a.MaxConcurrent())
	}
}

func TestAIMgmt_Create_DuplicateName(t *testing.T) {
	s := setupAISuite(t)
	if _, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "codex", WorkerID: "W-2", ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrAgentInstanceNameTaken) {
		t.Fatalf("expected name taken, got %v", err)
	}
}

func TestAIMgmt_Create_BadActor(t *testing.T) {
	s := setupAISuite(t)
	_, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestAIMgmt_Create_NoWorkerID(t *testing.T) {
	s := setupAISuite(t)
	_, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "", ActorIdentity: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestAIMgmt_UpdateConfig_Happy(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	a, _ := s.aiRepo.FindByID(context.Background(), created.ID)
	evID, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID:            created.ID,
		Config:        strP(`{"instructions_ref":"v2"}`),
		Version:       a.Version(),
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if evID == "" {
		t.Fatal("event id missing")
	}
	got, _ := s.aiRepo.FindByID(context.Background(), created.ID)
	if got.Config() != `{"instructions_ref":"v2"}` {
		t.Fatalf("config: %s", got.Config())
	}
}

func TestAIMgmt_UpdateConfig_NoFields(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	_, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: created.ID, Version: 1, ActorIdentity: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected error for no fields")
	}
}

func TestAIMgmt_UpdateConfig_VersionConflict(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	_, err := s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: created.ID, Config: strP("{}"), Version: 99, ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrAgentInstanceVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestAIMgmt_Archive_Happy(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	evID, err := s.mgmt.Archive(context.Background(), ArchiveAgentInstanceCommand{
		ID:            created.ID,
		Reason:        workforce.AgentInstanceArchivedReasonManual,
		Message:       "user retired the agent",
		Version:       1,
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if evID == "" {
		t.Fatal("event id missing")
	}
	got, _ := s.aiRepo.FindByID(context.Background(), created.ID)
	if got.State() != workforce.AgentInstanceArchived {
		t.Fatalf("state: %s", got.State())
	}
}

func TestAIMgmt_Archive_RejectsBuiltin(t *testing.T) {
	s := setupAISuite(t)
	id, err := s.mgmt.EnsureBuiltinSupervisor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.mgmt.Archive(context.Background(), ArchiveAgentInstanceCommand{
		ID: id, Reason: workforce.AgentInstanceArchivedReasonManual,
		Message: "no", Version: 1, ActorIdentity: "user:hayang",
	})
	if !errors.Is(err, workforce.ErrAgentInstanceIsBuiltin) {
		t.Fatalf("expected is-builtin, got %v", err)
	}
}

func TestAIMgmt_EnsureBuiltinSupervisor_Idempotent(t *testing.T) {
	s := setupAISuite(t)
	id1, err := s.mgmt.EnsureBuiltinSupervisor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.mgmt.EnsureBuiltinSupervisor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("ids should match across calls: %s vs %s", id1, id2)
	}
	all, _ := s.aiRepo.FindAll(context.Background(), workforce.AgentInstanceFilter{})
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 row, got %d", len(all))
	}
	if !all[0].IsBuiltin() {
		t.Fatal("expected builtin")
	}
	if all[0].Name() != workforce.BuiltinSupervisorName {
		t.Fatalf("name: %s", all[0].Name())
	}
}

// =============================================================================
// Lifecycle Service
// =============================================================================

func TestAILifecycle_OnExecutionStarted_IdleToActive(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	if err := s.life.OnExecutionStarted(context.Background(), created.ID, "system"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.aiRepo.FindByID(context.Background(), created.ID)
	if got.State() != workforce.AgentInstanceActive {
		t.Fatalf("state: %s", got.State())
	}
	// Idempotent.
	if err := s.life.OnExecutionStarted(context.Background(), created.ID, "system"); err != nil {
		t.Fatalf("idempotent call: %v", err)
	}
}

func TestAILifecycle_OnWorkerOffline_BulkToSleeping(t *testing.T) {
	s := setupAISuite(t)
	for _, name := range []string{"a1", "a2"} {
		if _, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
			Name: name, AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.life.OnWorkerOffline(context.Background(), "W-1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 sleeping, got %d", n)
	}
	all, _ := s.aiRepo.FindAll(context.Background(), workforce.AgentInstanceFilter{WorkerID: aiWIDptr("W-1")})
	for _, a := range all {
		if a.State() != workforce.AgentInstanceSleeping {
			t.Fatalf("%s should be sleeping, got %s", a.Name(), a.State())
		}
	}
}

func TestAILifecycle_OnWorkerOnline_BulkAwakened(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "a1", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	_, _ = s.life.OnWorkerOffline(context.Background(), "W-1", "system")
	n, err := s.life.OnWorkerOnline(context.Background(), "W-1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 awakened, got %d", n)
	}
	got, _ := s.aiRepo.FindByID(context.Background(), created.ID)
	if got.State() != workforce.AgentInstanceIdle {
		t.Fatalf("state: %s", got.State())
	}
}

// Events end-to-end: Create → Archive emit appropriate events.
func TestAIMgmt_EventChain(t *testing.T) {
	s := setupAISuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1", ActorIdentity: "user:hayang",
	})
	_, _ = s.mgmt.UpdateConfig(context.Background(), UpdateAgentInstanceConfigCommand{
		ID: created.ID, Config: strP(`{"x":1}`), Version: 1, ActorIdentity: "user:hayang",
	})
	_, _ = s.mgmt.Archive(context.Background(), ArchiveAgentInstanceCommand{
		ID: created.ID, Reason: workforce.AgentInstanceArchivedReasonManual,
		Message: "test", Version: 2, ActorIdentity: "user:hayang",
	})
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	types := map[observability.EventType]int{}
	for _, e := range events {
		types[e.Type()]++
	}
	if types[observability.EventType("workforce.agent_instance.created")] != 1 ||
		types[observability.EventType("workforce.agent_instance.config_updated")] != 1 ||
		types[observability.EventType("workforce.agent_instance.archived")] != 1 {
		t.Fatalf("event types: %+v", types)
	}
}

func aiWIDptr(s string) *workforce.WorkerID { v := workforce.WorkerID(s); return &v }
