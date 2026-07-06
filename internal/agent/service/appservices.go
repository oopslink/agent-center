package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

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
	Reasoning      string // T236: reasoning effort (minimal|low|medium|high, "" = default)
	Mode           string // T236: operating mode ("" = default)
	Provider       string // T236: LLM provider ("" = center default)
	// F3 model routing (design §5 & §10). All optional; carried like Model/Reasoning.
	OrchestratorModel    string   // orchestrator's own model (cheap/fast tier; "" = center default)
	DefaultExecutorModel string   // fallback executor model ("" = center default)
	MaxConcurrentTasks   int      // executor concurrency cap (0 ⇒ EffectiveMaxConcurrentTasks default)
	AllowedModels        []string // LEGACY model-only candidates (back-compat input; converted to AllowedExecutors)
	// AllowedExecutors is the authoritative {cli, model} candidate list (v2.18.1
	// BE-1). When set it wins; otherwise AllowedModels is lifted via the agent's cli.
	AllowedExecutors []agent.ExecutorProfile
	// AutoAssignable opts the agent in/out of the BE-2 auto-assign reconciler
	// (v2.18.3 BE-1). nil → the default (true = assignable).
	AutoAssignable *bool
	// IncludeDescriptionInSystemPrompt opts the agent in/out of injecting Description
	// into its system prompt (T728). nil → the default (true = inject).
	IncludeDescriptionInSystemPrompt *bool
	EnvVars                          map[string]string
	WorkerID                         string
	CreatedBy                        agent.IdentityRef
	// IdentityMemberID (optional, v2.7 #157) — the identity-member id this
	// execution Agent represents; set by the unified Members→Add Agent flow.
	IdentityMemberID string
}

// CreateAgent validates the chosen Worker belongs to the caller's org, then
// creates the Agent (stopped) + emits agent.created in one tx.
func (s *Service) CreateAgent(ctx context.Context, cmd CreateAgentCommand) (agent.AgentID, error) {
	// v2.7 #181 / FINDING-F: reject a cli the runtime can't execute (empty /
	// codex / opencode / unknown) — they are discovered (#147) + displayed
	// (#176) but their adapters are ErrNotImplemented stubs, so only
	// claude-code actually runs. Fail fast before touching the tx.
	if !agent.IsSupportedExecutionCLI(cmd.CLI) {
		return "", agent.ErrUnsupportedCLI
	}
	if !agent.IsSupportedReasoning(cmd.Reasoning) {
		return "", agent.ErrUnsupportedReasoning
	}
	execs, models, err := resolveAllowedExecutors(cmd.AllowedExecutors, cmd.AllowedModels, cmd.CLI)
	if err != nil {
		return "", err
	}
	id := agent.AgentID(s.idgen.NewULID())
	now := s.clock.Now()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID:             id,
		OrganizationID: cmd.OrganizationID,
		Profile: agent.Profile{
			Name: cmd.Name, Description: cmd.Description, Model: cmd.Model,
			CLI: cmd.CLI, Reasoning: cmd.Reasoning, Mode: cmd.Mode, Provider: cmd.Provider,
			OrchestratorModel: cmd.OrchestratorModel, DefaultExecutorModel: cmd.DefaultExecutorModel,
			MaxConcurrentTasks: cmd.MaxConcurrentTasks, AllowedModels: models, AllowedExecutors: execs,
			// v2.18.3 BE-1: a fresh agent is auto-assignable by default (nil → true);
			// the owner opts out by sending auto_assignable=false.
			AutoAssignable: cmd.AutoAssignable == nil || *cmd.AutoAssignable,
			// T728: a fresh agent injects its description into the system prompt by
			// default (nil → true); the owner opts out by sending false.
			IncludeDescriptionInSystemPrompt: cmd.IncludeDescriptionInSystemPrompt == nil || *cmd.IncludeDescriptionInSystemPrompt,
			EnvVars:                          cmd.EnvVars,
		},
		WorkerID:         cmd.WorkerID,
		CreatedBy:        cmd.CreatedBy,
		IdentityMemberID: cmd.IdentityMemberID,
		CreatedAt:        now,
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
func (s *Service) lifecycleOp(ctx context.Context, id agent.AgentID, mutate func(*agent.Agent) error, resetScope, verb string) error {
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
		// T338: record the user-triggered lifecycle action (start/stop/restart/
		// reset) into the agent's append-only activity stream so it shows up in the
		// AgentDetail Activity timeline (it renders EventTypeLifecycle's {event}).
		// Best-effort: an observational-log hiccup must not fail the action itself.
		s.recordLifecycleActivity(txCtx, id, verb, resetScope)
		return s.emit(txCtx, EvtAgentLifecycleChanged, a, resetScope)
	})
}

// recordLifecycleActivity appends a "lifecycle" activity event ({event:<verb>},
// plus {scope} for reset) for a user-triggered lifecycle action. Best-effort.
func (s *Service) recordLifecycleActivity(ctx context.Context, id agent.AgentID, verb, scope string) {
	if verb == "" || s.activity == nil {
		return
	}
	p := map[string]any{"event": verb}
	if verb == "reset" && scope != "" {
		p["scope"] = scope
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID:         s.idgen.NewULID(),
		AgentID:    id,
		EventType:  agent.EventTypeLifecycle,
		Payload:    string(b),
		OccurredAt: s.clock.Now(),
	})
	if err != nil {
		return
	}
	if aerr := s.activity.Append(ctx, ev); aerr != nil {
		slog.Warn("agent: lifecycle activity append failed", "agent_id", id, "event", verb, "err", aerr)
	}
}

// StartAgent moves stopped/error → running (intent; D2 reconciles the process).
func (s *Service) StartAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Start(now) }, "", "started")
}

// StopAgent moves running → stopping (operational stop; does NOT touch WorkItems).
func (s *Service) StopAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Stop(now) }, "", "stopped")
}

// RestartAgent requests a restart while keeping the running intent (version bump).
func (s *Service) RestartAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Restart(now) }, "", "restarted")
}

// UpdateAgentConfigCommand carries the editable runtime config (T236 + later
// profile knobs). Name / Description are preserved from the existing
// profile. Empty string fields are written as-is (empty = the runtime/center
// default).
type UpdateAgentConfigCommand struct {
	Model     string
	CLI       string
	Reasoning string
	Mode      string
	Provider  string
	// EnvVars is the per-agent environment overlay for agent CLI processes. nil means
	// the PATCH omitted env_vars and the existing map is preserved; non-nil replaces it
	// (including an empty map to clear).
	EnvVars *map[string]string
	// F3 model routing (design §5 & §10). Edited alongside the LLM tuning; empty /
	// zero stores the column as NULL/0 (= center default / EffectiveMaxConcurrentTasks).
	OrchestratorModel    string
	DefaultExecutorModel string
	MaxConcurrentTasks   int
	AllowedModels        []string // LEGACY input (converted to AllowedExecutors via the agent's cli)
	// AllowedExecutors is the authoritative {cli, model} candidate list (v2.18.1
	// BE-1); wins over AllowedModels when set.
	AllowedExecutors []agent.ExecutorProfile
	// AutoAssignable opts the agent in/out of the BE-2 auto-assign reconciler
	// (v2.18.3 BE-1). nil → preserve the existing value (a config edit that omits the
	// field must not silently flip it).
	AutoAssignable *bool
	// Description is the agent's human-readable description. nil → preserve the
	// existing value; non-nil replaces it (including empty string to clear).
	Description *string
	// IncludeDescriptionInSystemPrompt opts the agent in/out of injecting Description
	// into its system prompt (T728). nil → preserve the existing value (a config edit
	// that omits the field must not silently flip it).
	IncludeDescriptionInSystemPrompt *bool
}

// resolveAllowedExecutors canonicalizes the executor-candidate input into the
// authoritative list + its derived model mirror (v2.18.1 BE-1). AllowedExecutors
// wins when provided; otherwise the legacy AllowedModels is lifted into {cli, model}
// via the agent's cli (the migration-0085 backfill rule). The result is validated +
// deduped by the domain (NormalizeAllowedExecutors → ErrInvalidExecutorProfile on a
// bad cli / empty model), and the returned models slice is the distinct-models
// mirror persisted into the legacy column for model-only readers.
func resolveAllowedExecutors(execs []agent.ExecutorProfile, models []string, cli string) ([]agent.ExecutorProfile, []string, error) {
	in := execs
	if len(in) == 0 {
		// Back-compat: an old caller sent only the model-only allowed_models. A
		// {cli, model} profile needs a CLI, and the model list carries none — so we
		// pair every model with THIS agent's own cli (cmd.CLI; empty → claude-code),
		// exactly the migration-0085 backfill rule. The agent's cli is the only
		// defensible default: it is the runtime the supervisor already runs.
		in = agent.ExecutorsFromModels(models, cli)
	}
	norm, err := agent.NormalizeAllowedExecutors(in)
	if err != nil {
		return nil, nil, err
	}
	return norm, agent.ModelsOf(norm), nil
}

// UpdateAgentConfig edits an agent's LLM config (model/cli/reasoning/mode/
// provider) and persists it. Validates CLI + reasoning up front (→ 400).
//
// ADR-0049 §5 update (k8s live-config, oopslink 2026-07-04): a config edit to a
// RUNNING agent now propagates LIVE. It emits agent.lifecycle_changed so the
// Environment AgentControlProjector pushes a reconcile carrying the now-persisted
// profile; the agent-runtime refreshes its model routing IN PLACE (no restart, no
// dropped in-flight executors). This is required for k8s process isolation — the
// reconcile command is the only channel from center to the isolated agent-runtime, so
// a restart-only propagation is unacceptable (bouncing a pod drops live work).
// UpdateProfile bumps the version, so the reconcile is not deduped by the projector's
// version-keyed idempotency; rt.Start's version guard no-ops the live session (no
// session restart). A STOPPED/terminal agent keeps the old persist-only semantics
// (config applies on next spawn) — emitting for it could spawn an intentionally-stopped
// agent.
func (s *Service) UpdateAgentConfig(ctx context.Context, id agent.AgentID, cmd UpdateAgentConfigCommand) error {
	if !agent.IsSupportedExecutionCLI(cmd.CLI) {
		return agent.ErrUnsupportedCLI
	}
	if !agent.IsSupportedReasoning(cmd.Reasoning) {
		return agent.ErrUnsupportedReasoning
	}
	execs, models, err := resolveAllowedExecutors(cmd.AllowedExecutors, cmd.AllowedModels, cmd.CLI)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		p := a.Profile() // preserve Name/Description; edit runtime fields only.
		p.Model = cmd.Model
		p.CLI = cmd.CLI
		p.Reasoning = cmd.Reasoning
		p.Mode = cmd.Mode
		p.Provider = cmd.Provider
		if cmd.EnvVars != nil {
			p.EnvVars = normalizeEnvVars(*cmd.EnvVars)
		}
		p.OrchestratorModel = cmd.OrchestratorModel
		p.DefaultExecutorModel = cmd.DefaultExecutorModel
		p.MaxConcurrentTasks = cmd.MaxConcurrentTasks
		p.AllowedExecutors = execs
		p.AllowedModels = models // derived mirror (distinct models) for legacy readers
		// v2.18.3 BE-1: a.Profile() already carries the current AutoAssignable; only
		// override it when the PATCH explicitly sent the field (nil → preserve).
		if cmd.AutoAssignable != nil {
			p.AutoAssignable = *cmd.AutoAssignable
		}
		if cmd.Description != nil {
			p.Description = *cmd.Description
		}
		// T728: same preserve-unless-sent rule as AutoAssignable.
		if cmd.IncludeDescriptionInSystemPrompt != nil {
			p.IncludeDescriptionInSystemPrompt = *cmd.IncludeDescriptionInSystemPrompt
		}
		if err := a.UpdateProfile(p, now); err != nil {
			return err
		}
		if err := s.agents.Update(txCtx, a); err != nil {
			return err
		}
		// Live-propagate to a RUNNING agent (see doc comment). Stopped/terminal agents
		// keep persist-only semantics (config applies on next spawn).
		if a.Lifecycle() == agent.LifecycleRunning {
			return s.emit(txCtx, EvtAgentLifecycleChanged, a, "")
		}
		return nil
	})
}

func normalizeEnvVars(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k == "" || strings.Contains(k, "=") {
			continue
		}
		out[k] = v
	}
	return out
}

// SetAgentCapabilityTags replaces an agent's dispatch capability tags (T461) and
// persists them. Pure metadata for PD dispatch (find_org_agent) — it does NOT
// restart the process and emits nothing on the outbox (no runtime effect, like
// UpdateAgentConfig's persist-only contract). Tags are normalized in the domain
// (trim / drop-blank / case-insensitive de-dupe).
func (s *Service) SetAgentCapabilityTags(ctx context.Context, id agent.AgentID, tags []string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		a.SetCapabilityTags(tags, now)
		return s.agents.Update(txCtx, a)
	})
}

// ResetAgent moves the Agent to resetting for the given scope. The destructive
// op requires explicit confirmation (ADR-0049 §5 second confirmation).
func (s *Service) ResetAgent(ctx context.Context, id agent.AgentID, scope agent.ResetScope, confirm bool) error {
	if !confirm {
		return ErrResetNotConfirmed
	}
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Reset(scope, now) }, string(scope), "reset")
}

// --- D2-c-i controller→center lifecycle feedback (persist-only) -------------
//
// These are RESULT feedback from the (future) daemon AgentController reporting
// that a process settled to stopped or errored. They are NOT intent changes, so
// they MUST NOT emit agent.lifecycle_changed — that event is consumed by the
// Environment AgentControlProjector, which would enqueue a NEW reconcile command
// and create a feedback loop. We therefore PERSIST the AR state ONLY (no outbox
// emit). The lifecycle gating still lives in the AR (MarkStopped rejects an
// illegal precondition), so ErrIllegalLifecycle surfaces to the caller (→ 409).

// MarkAgentStopped records that a stopping/resetting Agent has settled to
// stopped (Environment feedback). Persist-only: NO outbox emit (loop-avoidance).
// Returns agent.ErrIllegalLifecycle if the Agent is not stopping/resetting.
func (s *Service) MarkAgentStopped(ctx context.Context, id agent.AgentID, at time.Time) error {
	return s.feedbackPersist(ctx, id, func(a *agent.Agent) error { return a.MarkStopped(at) })
}

// MarkAgentError records an Environment-reported error state for the Agent.
// Persist-only: NO outbox emit (loop-avoidance). MarkError has no precondition.
func (s *Service) MarkAgentError(ctx context.Context, id agent.AgentID, msg string, at time.Time) error {
	return s.feedbackPersist(ctx, id, func(a *agent.Agent) error { a.MarkError(msg, at); return nil })
}

// MarkAgentRecovered clears a CRASHED agent (error → running) when the daemon
// reports its session is back up (issue I13 auto-recovery). Persist-only: NO outbox
// emit (RESULT feedback, not an intent change — loop-avoidance). It is a NO-OP on any
// non-error lifecycle (see agent.MarkRecovered), so a stale/racing "running" feedback
// can never resurrect a deliberately-stopped or terminal agent.
func (s *Service) MarkAgentRecovered(ctx context.Context, id agent.AgentID, at time.Time) error {
	return s.feedbackPersist(ctx, id, func(a *agent.Agent) error { a.MarkRecovered(at); return nil })
}

// MarkAgentFailed records the TERMINAL crash-loop circuit-breaker state (v2.7
// GATE-7 Mode-B self-heal cap exhausted). Persist-only: NO outbox emit (result
// feedback, not an intent change). Returns agent.ErrIllegalLifecycle (→ 409) if
// the Agent is not running/error.
//
// v2.14.0 F7 (issue I14): the in-flight-WorkItem cascade was removed — AgentWorkItem
// retired. A dead agent's stuck tasks are now recovered by the F3 execution-lease
// checker (Task.Block on lease expiry / reassign), not an inline WorkItem cascade.
func (s *Service) MarkAgentFailed(ctx context.Context, id agent.AgentID, msg string, at time.Time) error {
	return s.feedbackPersist(ctx, id, func(a *agent.Agent) error { return a.MarkFailed(msg, at) })
}

// feedbackPersist is the shared load → AR-transition → persist path for the
// controller feedback verbs. CRITICAL: unlike lifecycleOp it does NOT emit any
// outbox event — these are result feedback, not intent changes, and emitting
// agent.lifecycle_changed would re-trigger the reconcile projector.
func (s *Service) feedbackPersist(ctx context.Context, id agent.AgentID, mutate func(*agent.Agent) error) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := mutate(a); err != nil {
			return err
		}
		return s.agents.Update(txCtx, a)
	})
}

// v2.14.0 F7 (issue I14): the agent-PULL WorkItem feedback verbs
// (StartWork/FailWork/PauseWork/ResumeWork/ResumeWorkByOperator/MarkWorkItemState),
// the WorkItemFeedbackState type/consts, and ErrWorkItemNotForAgent were removed
// here — AgentWorkItem retired. Agents now pull/advance work via the pm Task
// model (list_my_tasks / start_task / lease checker), not an AgentWorkItem queue.

// AppendActivity appends an observation event to the Agent's append-only
// activity stream (D2-c-i stdout→activity sink). It records an observation only
// — it does NOT post to any Conversation. Returns the new event id.
func (s *Service) AppendActivity(ctx context.Context, in agent.NewActivityEventInput) (string, error) {
	if in.ID == "" {
		in.ID = s.idgen.NewULID()
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = s.clock.Now()
	}
	e, err := agent.NewActivityEvent(in)
	if err != nil {
		return "", err
	}
	if err := s.activity.Append(ctx, e); err != nil {
		return "", err
	}
	return e.ID(), nil
}

// --- reads ------------------------------------------------------------------

// ListAgents returns every Agent in an Organization.
func (s *Service) ListAgents(ctx context.Context, orgID string) ([]*agent.Agent, error) {
	return s.agents.ListByOrg(ctx, orgID)
}

// GetAgent returns one Agent by its execution-entity id.
func (s *Service) GetAgent(ctx context.Context, id agent.AgentID) (*agent.Agent, error) {
	return s.agents.FindByID(ctx, id)
}

// ResolveAgent resolves an Agent by either its execution-entity id (internal /
// back-compat) OR its identity-member id ("agent-<ulid>", the business-layer id
// — v2.7 #185). The webconsole addresses agents by member id, so {id} path
// values are member ids; this bridges to the entity. Entity-id is tried first
// (cheap, no collision risk — member ids are "agent-"-prefixed, entity ids are
// bare ULIDs), then the member→entity bridge.
func (s *Service) ResolveAgent(ctx context.Context, idOrMemberID string) (*agent.Agent, error) {
	a, err := s.agents.FindByID(ctx, agent.AgentID(idOrMemberID))
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, agent.ErrAgentNotFound) {
		return nil, err
	}
	return s.agents.FindByIdentityMemberID(ctx, idOrMemberID)
}

// ArchiveAgent soft-deletes an agent (v2.8 #272) — the SOLE user-facing delete
// path. Guard (b strict): only a settled non-running agent (stopped/error/failed)
// may be archived; running/transitioning → ErrAgentNotStoppedForArchive (409).
// Idempotent: re-archiving an already-archived agent is a 200 no-op (no
// re-persist, no double version bump). Archiving sets lifecycle=archived + clears
// the worker binding (worker_id="") via the dedicated repo.Archive, so the worker
// is freed to re-bind while the agent row is RETAINED (history — #215 chip /
// assignee / GET-by-id). Persist-only (no reconcile emit): the agent is already
// stopped, so there is no running process to reconcile — only the binding is
// released.
func (s *Service) ArchiveAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := a.Archive(now); err != nil {
			if errors.Is(err, agent.ErrAgentAlreadyArchived) {
				return nil // idempotent no-op
			}
			return err
		}
		return s.agents.Archive(txCtx, a)
	})
}

// DeleteAgent hard-deletes an agent (v2.7 #197). When force is false the agent must
// be Stopped (else ErrAgentNotStopped). When force is true (v2.8.1 force-delete,
// @oopslink) the process is assumed dead and the stopped-guard is skipped. Deletes
// the agent row, releasing its worker binding. The webconsole wraps this in one tx
// with the identity-member delete for an atomic teardown (mirrors #157 — no orphan
// member left behind).
//
// v2.14.0 F7 (issue I14): the no-active-WorkItem guard (non-force) and the
// force-delete WorkItem orphan-sweep were removed — AgentWorkItem retired. A
// deleted agent's stuck tasks are recovered by the F3 execution-lease checker
// (Task.Block / reassign on lease expiry), not an inline WorkItem sweep.
func (s *Service) DeleteAgent(ctx context.Context, id agent.AgentID, force bool) error {
	a, err := s.agents.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !force {
		if a.Lifecycle() != agent.LifecycleStopped {
			return agent.ErrAgentNotStopped
		}
	}
	return s.agents.Delete(ctx, id)
}

// AgentsByWorker returns the agents bound to a worker (worker_id binding). Used by
// the worker force-delete handler to detect bound agents (busy-guard / unbind).
func (s *Service) AgentsByWorker(ctx context.Context, workerID string) ([]*agent.Agent, error) {
	return s.agents.ListByWorker(ctx, workerID)
}

// UnbindAgentsFromWorker clears the worker binding of every agent bound to a
// force-deleted worker (v2.8.1), returning the count unbound. The agents become
// worker-less (retained, NOT archived) — re-bindable later. Called by the worker
// force-delete handler (composition layer) so the agent BC owns the binding write.
func (s *Service) UnbindAgentsFromWorker(ctx context.Context, workerID string) (int, error) {
	return s.agents.ClearWorkerBindings(ctx, workerID, s.clock.Now())
}

// v2.14.0 F7 (issue I14): ListWorkItems removed — AgentWorkItem retired. The
// agent's work queue/history is now the pm Task model (list_my_tasks).

// ListActivity returns an Agent's activity events newest-first (id DESC). v2.8
// #274 cursor pagination: before="" = newest page, before=<event-id> = older
// than that cursor; limit>0 caps, limit<=0 = unlimited. The handler resolves the
// default (omitted → 50) and the next_cursor; the service/repo pass it through.
func (s *Service) ListActivity(ctx context.Context, id agent.AgentID, limit int, before string) ([]*agent.AgentActivityEvent, error) {
	return s.activity.ListByAgent(ctx, id, limit, before)
}

// LatestActivityByAgents returns the single most-recent activity event per agent
// across the whole input set in ONE batch query (NO N+1) — the v2.8.1 agents-list
// enrich uses it to render last_activity_at/last_activity_content for the whole
// page without a per-agent round-trip. Pass the execution-entity AgentIDs (the
// agent_activity_events partition key). Agents with no events have no map entry.
func (s *Service) LatestActivityByAgents(ctx context.Context, ids []agent.AgentID) (map[agent.AgentID]*agent.AgentActivityEvent, error) {
	return s.activity.LatestByAgents(ctx, ids)
}

// ReportInstalledSkills replaces an agent's OBSERVED installed-skill set with the
// runtime's latest report (issue-4a45e9cc). The set is normalized (blank names
// dropped, layer validated, shadowed recomputed from precedence — defense in depth)
// then swapped wholesale by the repo. collectedAt stamps the whole batch so an
// offline agent's panel can show "last collected X ago"; a zero value defaults to the
// service clock. nil Skills repo → a safe no-op (feature not wired). Reporting an
// empty set clears the agent's rows (the runtime resolved nothing).
func (s *Service) ReportInstalledSkills(ctx context.Context, id agent.AgentID, skills []agent.InstalledSkill, collectedAt time.Time) error {
	if s.skills == nil {
		return nil
	}
	if collectedAt.IsZero() {
		collectedAt = s.clock.Now()
	}
	collectedAt = collectedAt.UTC()
	norm, err := agent.NormalizeInstalledSkills(skills)
	if err != nil {
		return err
	}
	for i := range norm {
		norm[i].AgentRef = id
		norm[i].CollectedAt = collectedAt
	}
	return s.skills.ReplaceForAgent(ctx, id, norm)
}

// ListInstalledSkills returns an agent's OBSERVED installed skills ordered by layer
// precedence (built-in→project) then name. nil Skills repo → empty (feature not
// wired). The web console groups the result by layer and marks shadowed entries.
func (s *Service) ListInstalledSkills(ctx context.Context, id agent.AgentID) ([]agent.InstalledSkill, error) {
	if s.skills == nil {
		return nil, nil
	}
	return s.skills.ListByAgent(ctx, id)
}
