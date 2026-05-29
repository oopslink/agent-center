package service

import (
	"context"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/workforce"
)

// CreateAgentCommand captures the create-agent inputs. The Worker binding is
// immutable after creation (ADR-0049 §5).
type CreateAgentCommand struct {
	OrganizationID string
	Name           string
	Description    string
	Model          string
	CLI            string
	EnvVars        map[string]string
	Skills         []string
	WorkerID       string
	CreatedBy      agent.IdentityRef
}

// CreateAgent validates the chosen Worker belongs to the caller's org, then
// creates the Agent (stopped) + emits agent.created in one tx.
func (s *Service) CreateAgent(ctx context.Context, cmd CreateAgentCommand) (agent.AgentID, error) {
	id := agent.AgentID(s.idgen.NewULID())
	now := s.clock.Now()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID:             id,
		OrganizationID: cmd.OrganizationID,
		Profile: agent.Profile{
			Name: cmd.Name, Description: cmd.Description, Model: cmd.Model,
			CLI: cmd.CLI, EnvVars: cmd.EnvVars,
		},
		Skills:    cmd.Skills,
		WorkerID:  cmd.WorkerID,
		CreatedBy: cmd.CreatedBy,
		CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	return id, s.runInTx(ctx, func(txCtx context.Context) error {
		// Worker must exist AND belong to the same org (cross-org binding hidden).
		w, werr := s.workers.FindByID(txCtx, workforce.WorkerID(cmd.WorkerID))
		if werr != nil || w == nil || w.OrganizationID() != cmd.OrganizationID {
			return ErrWorkerNotInOrg
		}
		if serr := s.agents.Save(txCtx, a); serr != nil {
			return serr
		}
		return s.emit(txCtx, EvtAgentCreated, a, "")
	})
}

// lifecycleOp is the shared load → AR-transition → persist → emit path for the
// lifecycle intent verbs. The transition function is an AR method, so illegal
// transitions are rejected by the aggregate (the AppService never bare-writes
// the lifecycle field).
func (s *Service) lifecycleOp(ctx context.Context, id agent.AgentID, mutate func(*agent.Agent) error, resetScope string) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := mutate(a); err != nil {
			return err
		}
		if err := s.agents.Update(txCtx, a); err != nil {
			return err
		}
		return s.emit(txCtx, EvtAgentLifecycleChanged, a, resetScope)
	})
}

// StartAgent moves stopped/error → running (intent; D2 reconciles the process).
func (s *Service) StartAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Start(now) }, "")
}

// StopAgent moves running → stopping (operational stop; does NOT touch WorkItems).
func (s *Service) StopAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Stop(now) }, "")
}

// RestartAgent requests a restart while keeping the running intent (version bump).
func (s *Service) RestartAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Restart(now) }, "")
}

// ResetAgent moves the Agent to resetting for the given scope. The destructive
// op requires explicit confirmation (ADR-0049 §5 second confirmation).
func (s *Service) ResetAgent(ctx context.Context, id agent.AgentID, scope agent.ResetScope, confirm bool) error {
	if !confirm {
		return ErrResetNotConfirmed
	}
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Reset(scope, now) }, string(scope))
}

// --- reads ------------------------------------------------------------------

// ListAgents returns every Agent in an Organization.
func (s *Service) ListAgents(ctx context.Context, orgID string) ([]*agent.Agent, error) {
	return s.agents.ListByOrg(ctx, orgID)
}

// GetAgent returns one Agent by id.
func (s *Service) GetAgent(ctx context.Context, id agent.AgentID) (*agent.Agent, error) {
	return s.agents.FindByID(ctx, id)
}

// ListWorkItems returns an Agent's work items (queue + history).
func (s *Service) ListWorkItems(ctx context.Context, id agent.AgentID) ([]*agent.AgentWorkItem, error) {
	return s.workItems.ListByAgent(ctx, id)
}

// ListActivity returns an Agent's recent activity events (newest first).
func (s *Service) ListActivity(ctx context.Context, id agent.AgentID, limit int) ([]*agent.AgentActivityEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.activity.ListByAgent(ctx, id, limit)
}
