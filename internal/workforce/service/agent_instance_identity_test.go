package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// stubIdentityRegistrar captures the in-tx call so we can assert the
// invariant.
type stubIdentityRegistrar struct {
	called   bool
	gotID    string
	gotName  string
	gotActor observability.Actor
	failWith error
}

func (s *stubIdentityRegistrar) RegisterAgentIdentityInTx(ctx context.Context, agentInstanceID string, displayName string, actor observability.Actor) error {
	s.called = true
	s.gotID = agentInstanceID
	s.gotName = displayName
	s.gotActor = actor
	return s.failWith
}

func TestAgentInstanceManagementService_RegistersIdentityInSameTx(t *testing.T) {
	s := setupSuite(t)
	mgmt := NewAgentInstanceManagementService(s.db, wfsqlite.NewAgentInstanceRepo(s.db), s.idgen, s.sink, s.clock)
	stub := &stubIdentityRegistrar{}
	mgmt.WithIdentityRegistrar(stub)
	_, err := mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "myAgent", AgentCLI: "claudecode", WorkerID: workforce.WorkerID("w-1"),
		ActorIdentity: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !stub.called {
		t.Fatal("expected RegisterAgentIdentityInTx to be called")
	}
	if stub.gotName != "myAgent" {
		t.Fatalf("got name=%s", stub.gotName)
	}
	if stub.gotActor != observability.Actor("user:hayang") {
		t.Fatalf("got actor=%s", stub.gotActor)
	}
}

func TestAgentInstanceManagementService_IdentityFailureRollsBack(t *testing.T) {
	s := setupSuite(t)
	repo := wfsqlite.NewAgentInstanceRepo(s.db)
	mgmt := NewAgentInstanceManagementService(s.db, repo, s.idgen, s.sink, s.clock)
	stub := &stubIdentityRegistrar{failWith: errors.New("boom")}
	mgmt.WithIdentityRegistrar(stub)
	res, err := mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "x", AgentCLI: "claudecode", WorkerID: workforce.WorkerID("w-1"),
		ActorIdentity: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	_, ferr := repo.FindByID(context.Background(), res.ID)
	if !errors.Is(ferr, workforce.ErrAgentInstanceNotFound) {
		t.Fatalf("expected rollback (not found), got %v", ferr)
	}
}

func TestAgentInstanceManagementService_NoIdentityRegistrar_StillCreates(t *testing.T) {
	s := setupSuite(t)
	mgmt := NewAgentInstanceManagementService(s.db, wfsqlite.NewAgentInstanceRepo(s.db), s.idgen, s.sink, s.clock)
	if _, err := mgmt.Create(context.Background(), CreateAgentInstanceCommand{
		Name: "x", AgentCLI: "claudecode", WorkerID: workforce.WorkerID("w-1"),
		ActorIdentity: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
}
