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
	// v2.14.0 F7 (issue I14): EvtAgentWorkItemTransitioned removed — AgentWorkItem
	// retired (no AR emits work-item transitions any more).
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

// TaskRunGate authorizes a work item's task to enter running at start_work
// (T130). Implemented by the projectmanager Service (which owns the plan/pool
// knowledge: a task runs ONLY as a real-plan node or a DISPATCHED Assignment-Pool
// member). Wired at the composition root via Service.SetTaskRunGate; the Agent BC
// depends only on this PORT, never on projectmanager → no import cycle. A nil gate
// (test fixtures / pre-T130 wiring) skips the check, preserving prior behavior.
type TaskRunGate interface {
	// EnsureTaskRunnable returns nil when the work item's task may enter
	// running, or ErrTaskNotRunnable when it is backlog. taskRef is the
	// work item's "pm://tasks/{id}" owner ref.
	EnsureTaskRunnable(ctx context.Context, taskRef string) error
}

// Service is the Agent-BC AppService facade.
type Service struct {
	db       *sql.DB
	agents   agent.Repository
	activity agent.ActivityEventRepository
	workers  workforce.WorkerRepository
	outbox   outbox.Repository
	idgen    idgen.Generator
	clock    clock.Clock
	// taskRunGate (T130) authorizes a work item's task to enter running at
	// start_work; nil → the check is skipped (set at the composition root).
	taskRunGate TaskRunGate
}

// SetTaskRunGate wires the T130 open→running authorization port (the pm Service
// adapter). Called once at the composition root after both services exist
// (mirrors pm Service.SetPausedTaskProvider). nil-safe: leaving it unset keeps
// the pre-T130 behavior (no gate), which the test fixtures rely on.
func (s *Service) SetTaskRunGate(g TaskRunGate) { s.taskRunGate = g }

// Deps bundles the Service dependencies.
type Deps struct {
	DB       *sql.DB
	Agents   agent.Repository
	Activity agent.ActivityEventRepository
	Workers  workforce.WorkerRepository
	Outbox   outbox.Repository
	IDGen    idgen.Generator
	Clock    clock.Clock
}

// New constructs the Service.
func New(d Deps) *Service {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Service{
		db: d.DB, agents: d.Agents, activity: d.Activity,
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
	Version    int    `json:"version"`
	ResetScope string `json:"reset_scope,omitempty"`
	// Model is the agent's configured claude --model (Profile.Model), carried so the
	// Environment projector can pass it into the reconcile command and the daemon can
	// spawn claude with it (v2.7 control-loop Model plumbing). ADDITIVE: empty/absent →
	// the daemon omits --model → claude default. Snapshotted at the (re)start that
	// emitted this lifecycle event ("change model → restart to apply" semantics).
	Model string `json:"model,omitempty"`
	// DisplayName is the agent's human-readable display_name (Profile.Name), carried
	// the SAME way as Model so the Environment projector passes it into the reconcile
	// command and the supervisor injects it as GIT_{AUTHOR,COMMITTER}_NAME via the ②
	// AgentEnv seam (commit authorship reads as the display_name, not the ULID — T469).
	// ADDITIVE: empty/absent → the supervisor falls back to the ULID AgentID.
	DisplayName string `json:"display_name,omitempty"`
	// CLI is the agent's configured execution CLI (Profile.CLI: "claude-code" /
	// "codex"), carried the SAME way as Model so the Environment projector can pass it
	// into the reconcile command and the daemon can pick the per-CLI session starter
	// (claude supervisor vs codex exec). ADDITIVE: empty/absent → the daemon defaults
	// to the claude path.
	CLI string `json:"cli,omitempty"`
	// T236 LLM tuning, carried the SAME way as Model/CLI (snapshotted at the
	// (re)start that emitted this event → "edit config + restart to apply"). All
	// ADDITIVE: empty/absent → the daemon omits the corresponding flag → runtime
	// default.
	Reasoning string `json:"reasoning,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Provider  string `json:"provider,omitempty"`
	// F3 model routing (design §5 & §10), carried the SAME way as Model/Reasoning
	// (snapshotted at the (re)start that emitted this event). All ADDITIVE.
	OrchestratorModel    string                  `json:"orchestrator_model,omitempty"`
	DefaultExecutorModel string                  `json:"default_executor_model,omitempty"`
	MaxConcurrentTasks   int                     `json:"max_concurrent_tasks,omitempty"`
	AllowedModels        []string                `json:"allowed_models,omitempty"`
	AllowedExecutors     []agent.ExecutorProfile `json:"allowed_executors,omitempty"`
}

// emit appends an outbox event inside the current transaction. Mutating
// AppServices call this within runInTx so the Agent-BC state write + the event
// commit atomically (OQ1).
func (s *Service) emit(ctx context.Context, eventType string, a *agent.Agent, resetScope string) error {
	pb, err := json.Marshal(agentEventPayload{
		AgentID:              string(a.ID()),
		OrgID:                a.OrganizationID(),
		WorkerID:             a.WorkerID(),
		Lifecycle:            string(a.Lifecycle()),
		Version:              a.Version(),
		ResetScope:           resetScope,
		Model:                a.Profile().Model,
		DisplayName:          a.Profile().Name,
		CLI:                  a.Profile().CLI,
		Reasoning:            a.Profile().Reasoning,
		Mode:                 a.Profile().Mode,
		Provider:             a.Profile().Provider,
		OrchestratorModel:    a.Profile().OrchestratorModel,
		DefaultExecutorModel: a.Profile().DefaultExecutorModel,
		MaxConcurrentTasks:   a.Profile().MaxConcurrentTasks,
		AllowedModels:        a.Profile().AllowedModels,
		AllowedExecutors:     a.Profile().AllowedExecutors,
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
// worker's online status + the Agent lifecycle.
//
// v2.14.0 F7 (issue I14): the hasActiveTask input was retired with the
// AgentWorkItem world — availability no longer reflects an in-flight work item
// (busy is now an observable of the pm Task model, surfaced by the read layer, not
// by this lifecycle-derived field). Passed as false here so the derivation reduces
// to worker-online × lifecycle.
func (s *Service) Availability(ctx context.Context, a *agent.Agent) (agent.Availability, error) {
	return a.Availability(s.workerOnline(ctx, a.WorkerID()), false), nil
}
