// Package service hosts the Agent bounded-context AppServices (v2.7 C3,
// ADR-0049). Every mutating AppService writes ONLY Agent-BC state + an outbox
// event in ONE local transaction (OQ1 = outbox purity): the cross-BC effect —
// the Environment BC (D2 AgentController) reconciling the lifecycle INTENT onto
// a real worker process — is driven by an idempotent projector consuming these
// events, never inline here. C3 only EMITS; D2 consumes.
//
// Lifecycle gating lives in the Agent AR (agent.Start/Stop/Restart/Reset reject
// illegal transitions) — the AppService never bare-writes the lifecycle field.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// Outbox event types (the C3 producer set; D2 AgentController consumes them).
const (
	EvtAgentCreated          = "agent.created"
	EvtAgentLifecycleChanged = "agent.lifecycle_changed"
)

// Sentinel errors surfaced to the HTTP layer.
var (
	// ErrWorkerNotInOrg is returned when CreateAgent references a worker that
	// does not exist or belongs to a different organization.
	ErrWorkerNotInOrg = errors.New("agent service: worker not found in this organization")
	// ErrResetNotConfirmed guards the destructive reset (ADR-0049 §5 requires a
	// second confirmation; the AppService enforces the flag).
	ErrResetNotConfirmed = errors.New("agent service: reset requires explicit confirmation")
)

// Service is the Agent-BC AppService facade.
type Service struct {
	db        *sql.DB
	agents    agent.Repository
	workItems agent.WorkItemRepository
	activity  agent.ActivityEventRepository
	workers   workforce.WorkerRepository
	outbox    outbox.Repository
	idgen     idgen.Generator
	clock     clock.Clock
}

// Deps bundles the Service dependencies.
type Deps struct {
	DB        *sql.DB
	Agents    agent.Repository
	WorkItems agent.WorkItemRepository
	Activity  agent.ActivityEventRepository
	Workers   workforce.WorkerRepository
	Outbox    outbox.Repository
	IDGen     idgen.Generator
	Clock     clock.Clock
}

// New constructs the Service.
func New(d Deps) *Service {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Service{
		db: d.DB, agents: d.Agents, workItems: d.WorkItems, activity: d.Activity,
		workers: d.Workers, outbox: d.Outbox, idgen: d.IDGen, clock: clk,
	}
}

// agentEventPayload is the JSON payload for agent lifecycle/creation events the
// D2 AgentController consumes to drive the real worker process.
type agentEventPayload struct {
	AgentID    string `json:"agent_id"`
	OrgID      string `json:"organization_id"`
	WorkerID   string `json:"worker_id"`
	Lifecycle  string `json:"lifecycle"`
	ResetScope string `json:"reset_scope,omitempty"`
}

// emit appends an outbox event inside the current transaction. Mutating
// AppServices call this within runInTx so the Agent-BC state write + the event
// commit atomically (OQ1).
func (s *Service) emit(ctx context.Context, eventType string, a *agent.Agent, resetScope string) error {
	pb, err := json.Marshal(agentEventPayload{
		AgentID:    string(a.ID()),
		OrgID:      a.OrganizationID(),
		WorkerID:   a.WorkerID(),
		Lifecycle:  string(a.Lifecycle()),
		ResetScope: resetScope,
	})
	if err != nil {
		return err
	}
	refs, _ := json.Marshal(map[string]string{
		"agent_id": string(a.ID()), "worker_id": a.WorkerID(), "organization_id": a.OrganizationID(),
	})
	return s.outbox.Append(ctx, outbox.Event{
		ID:        s.idgen.NewULID(),
		EventType: eventType,
		Refs:      string(refs),
		Payload:   string(pb),
		CreatedAt: s.clock.Now(),
	})
}

func (s *Service) runInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return persistence.RunInTx(ctx, s.db, fn)
}

// workerOnline reports whether the Agent's bound Worker is currently online —
// the Environment input to availability derivation (OQ2). C3 reads it from the
// legacy workforce.Worker; D1 switches the source to the Environment Worker.
func (s *Service) workerOnline(ctx context.Context, workerID string) bool {
	w, err := s.workers.FindByID(ctx, workforce.WorkerID(workerID))
	if err != nil || w == nil {
		return false
	}
	return w.Status() == workforce.WorkerOnline
}

// Availability computes the derived availability for an Agent (OQ2): the bound
// worker's online status + the Agent lifecycle + whether it has an active or
// waiting_input WorkItem.
func (s *Service) Availability(ctx context.Context, a *agent.Agent) (agent.Availability, error) {
	hasActive, err := s.workItems.HasActiveWorkItem(ctx, a.ID())
	if err != nil {
		return "", err
	}
	return a.Availability(s.workerOnline(ctx, a.WorkerID()), hasActive), nil
}
