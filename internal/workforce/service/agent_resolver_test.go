package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// resSuite wires the resolver against real SQLite repos.
type resSuite struct {
	*suite
	aiRepo   *wfsqlite.AgentInstanceRepo
	mgmt     *AgentInstanceManagementService
	resolver *AgentResolver
}

func setupResolverSuite(t *testing.T) *resSuite {
	t.Helper()
	s := setupSuite(t)
	aiRepo := wfsqlite.NewAgentInstanceRepo(s.db)
	return &resSuite{
		suite:    s,
		aiRepo:   aiRepo,
		mgmt:     NewAgentInstanceManagementService(s.db, aiRepo, s.idgen, s.sink, s.clock),
		resolver: NewAgentResolver(aiRepo, s.workerRepo),
	}
}

func seedWorkerWithCap(t *testing.T, s *resSuite, id workforce.WorkerID, caps []workforce.Capability) {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             id,
		CapabilityList: caps,
		EnrolledAt:     s.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.workerRepo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

func TestAgentResolver_HappyPath_NoMCP(t *testing.T) {
	s := setupResolverSuite(t)
	seedWorkerWithCap(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true},
	})
	created, err := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.resolver.Resolve(context.Background(), string(created.ID))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.FeatureOK {
		t.Fatalf("expected FeatureOK, got %+v", res)
	}
	if res.AgentCLI != "claude-code" || res.WorkerID != "W-1" {
		t.Fatalf("resolution mismatch: %+v", res)
	}
	if res.HomeDir == "" {
		t.Fatal("home dir should be set")
	}
}

func TestAgentResolver_FeatureUnsupported_MCP(t *testing.T) {
	s := setupResolverSuite(t)
	seedWorkerWithCap(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true, SupportsMCP: false},
	})
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1",
		Config:        `{"mcp_config":{"servers":{}}}`,
		ActorIdentity: "user:hayang",
	})
	res, err := s.resolver.Resolve(context.Background(), string(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if res.FeatureOK {
		t.Fatal("expected feature check to fail")
	}
	if res.FeatureReason != "feature_unsupported" {
		t.Fatalf("reason: %s", res.FeatureReason)
	}
}

func TestAgentResolver_AgentNotFound(t *testing.T) {
	s := setupResolverSuite(t)
	_, err := s.resolver.Resolve(context.Background(), "01H-MISSING")
	if !errors.Is(err, dispatch.ErrAgentResolutionUnknownAgent) {
		t.Fatalf("expected unknown agent, got %v", err)
	}
}

func TestAgentResolver_CapabilityMissing(t *testing.T) {
	s := setupResolverSuite(t)
	// Worker has only codex; agent expects claude-code.
	seedWorkerWithCap(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "codex", Detected: true, Enabled: true},
	})
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1",
		ActorIdentity: "user:hayang",
	})
	res, _ := s.resolver.Resolve(context.Background(), string(created.ID))
	if res.FeatureOK {
		t.Fatal()
	}
	if res.FeatureReason != "capability_missing" {
		t.Fatalf("reason: %s", res.FeatureReason)
	}
}

func TestAgentResolver_CapabilityDetectedButNotEnabled(t *testing.T) {
	s := setupResolverSuite(t)
	seedWorkerWithCap(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: false},
	})
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-1",
		ActorIdentity: "user:hayang",
	})
	res, _ := s.resolver.Resolve(context.Background(), string(created.ID))
	if res.FeatureOK {
		t.Fatal()
	}
	if res.FeatureReason != "capability_missing" {
		t.Fatalf("reason: %s", res.FeatureReason)
	}
}

func TestAgentResolver_WorkerNotFound(t *testing.T) {
	s := setupResolverSuite(t)
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "coder", AgentCLI: "claude-code", WorkerID: "W-NOPE",
		ActorIdentity: "user:hayang",
	})
	res, _ := s.resolver.Resolve(context.Background(), string(created.ID))
	if res.FeatureOK {
		t.Fatal()
	}
	if res.FeatureReason != "agent_unavailable" {
		t.Fatalf("reason: %s", res.FeatureReason)
	}
}

func TestAgentResolver_BuiltinAgentRejected(t *testing.T) {
	s := setupResolverSuite(t)
	id, err := s.mgmt.EnsureBuiltinSupervisor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	res, _ := s.resolver.Resolve(context.Background(), string(id))
	if res.FeatureOK {
		t.Fatal("builtin agent should not be a valid dispatch target")
	}
	if res.FeatureReason != "agent_unavailable" {
		t.Fatalf("reason: %s", res.FeatureReason)
	}
}

func TestAgentResolver_NilDeps(t *testing.T) {
	var r *AgentResolver
	if _, err := r.Resolve(context.Background(), "x"); err == nil {
		t.Fatal()
	}
	r2 := NewAgentResolver(nil, nil)
	if _, err := r2.Resolve(context.Background(), "x"); err == nil {
		t.Fatal()
	}
}

// HomeDir is wired through.
func TestAgentResolver_HomeDirPathInResolution(t *testing.T) {
	s := setupResolverSuite(t)
	seedWorkerWithCap(t, s, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true},
	})
	created, _ := s.mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "homed", AgentCLI: "claude-code", WorkerID: "W-1",
		ActorIdentity: "user:hayang",
	})
	res, _ := s.resolver.Resolve(context.Background(), string(created.ID))
	if res.HomeDir == "" {
		t.Fatal()
	}
	// Should embed the agent id per ADR-0029 § 3
	want := "~/.agent-center-worker/agents/" + string(created.ID) + "/"
	if res.HomeDir != want {
		t.Fatalf("home dir: %s want %s", res.HomeDir, want)
	}
	// Suppress unused import warning since we only use ai.HomeDirPath() through
	// the resolver, not directly.
	_ = time.Time{}
}
