// Package agent is the Agent bounded context (v2.7, ADR-0049): a logically
// long-running Agent product entity — profile, skills, runtime config, and
// lifecycle INTENT. There is no AgentRun concept; observability is the Agent's
// status + activity stream, not a run.
//
// C1 (task #99) ships the Agent aggregate + lifecycle + derived availability +
// repository. AgentWorkItem and AgentActivityEvent are separate ARs/streams
// that land in C2 (#100); the outbox-driven AssignTask→EnqueueWorkItem wiring
// lands there too.
package agent

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// Typed identifiers.
type (
	AgentID     string
	IdentityRef string
)

func (id AgentID) String() string    { return string(id) }
func (r IdentityRef) String() string { return string(r) }

// Validate enforces the kind-prefixed identity vocabulary (ADR-0033).
func (r IdentityRef) Validate() error {
	s := string(r)
	if s == "" {
		return errors.New("agent: identity ref required")
	}
	if s == "system" {
		return nil
	}
	for _, p := range []string{"user:", "agent:"} {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("agent: identity ref must be 'system' or 'user:<id>' / 'agent:<id>' (ADR-0033)")
}

// AgentLifecycle is the lifecycle INTENT the Environment BC reconciles
// (ADR-0049 §5). Note there is no "starting" — availability (OQ2) keys on this
// set: {stopped, stopping, resetting, error} → unavailable; running →
// available/busy.
type AgentLifecycle string

const (
	LifecycleStopped   AgentLifecycle = "stopped"
	LifecycleRunning   AgentLifecycle = "running"
	LifecycleStopping  AgentLifecycle = "stopping"
	LifecycleResetting AgentLifecycle = "resetting"
	LifecycleError     AgentLifecycle = "error"
	// LifecycleFailed is the TERMINAL crash-loop circuit-breaker state (v2.7 GATE-7
	// Mode-B): the worker's self-heal exhausted its bounded relaunch attempts, so it
	// stopped auto-relaunching to avoid thrashing. UNLIKE "error" (transient — the
	// worker is still auto-retrying), "failed" is terminal and requires a MANUAL
	// recovery (Start/Reset) to leave. (Distinct aggregate from AgentWorkItem's
	// "failed" status — that is a unit of WORK; this is the AGENT runtime lifecycle.)
	LifecycleFailed AgentLifecycle = "failed"
	// LifecycleArchived is the TERMINAL soft-delete state (v2.8 #272): the agent is
	// retired from the user-facing surface but its row is RETAINED, so historical
	// task assignees / "(archived)" chips / GET-by-id still resolve (Tester hard
	// constraint, #215 deleted-peer pattern). No un-archive in v2.8 (deferred
	// follow-up). Archiving clears the worker binding so the worker is freed to
	// re-bind. Reachable only from a settled non-running state (stopped/error/
	// failed) — running/transitioning agents must stop first (#272 (b) strict).
	LifecycleArchived AgentLifecycle = "archived"
)

// IsValid reports enum membership.
func (l AgentLifecycle) IsValid() bool {
	switch l {
	case LifecycleStopped, LifecycleRunning, LifecycleStopping, LifecycleResetting, LifecycleError, LifecycleFailed, LifecycleArchived:
		return true
	}
	return false
}

// ResetScope bounds a ResetAgent operation (ADR-0049 §5: reset has scopes and
// requires a second confirmation — the confirmation is enforced by the
// AppService/UI, the scope is the domain input).
type ResetScope string

const (
	ResetMemory    ResetScope = "memory"    // wipe {home}/memory
	ResetWorkspace ResetScope = "workspace" // design §3.1: wipe {home}/tasks + {home}/plans
	ResetAll       ResetScope = "all"       // wipe the whole runtime home
)

// IsValid reports enum membership.
func (s ResetScope) IsValid() bool {
	return s == ResetMemory || s == ResetWorkspace || s == ResetAll
}

// Sentinel errors.
var (
	ErrAgentNotFound = errors.New("agent: agent not found")
	ErrAgentExists   = errors.New("agent: agent already exists")
	// ErrAgentNotStopped rejects deleting an agent that is not in the Stopped
	// terminal-ish state (v2.7 #197) — the operator must stop it first.
	ErrAgentNotStopped = errors.New("agent: agent must be stopped before delete")
	// ErrAgentHasActiveWork rejects deleting an agent with a non-terminal work
	// item (v2.7 #197) — it is actively working; don't delete out from under it.
	ErrAgentHasActiveWork = errors.New("agent: agent has an active work item")
	ErrWorkerRequired     = errors.New("agent: a worker is required at creation (immutable binding, ADR-0049)")
	ErrInvalidLifecycle   = errors.New("agent: invalid lifecycle state")
	ErrIllegalLifecycle   = errors.New("agent: illegal lifecycle transition")
	ErrInvalidResetScope  = errors.New("agent: invalid reset scope")
	// ErrResetRequiresStopped rejects a Reset issued while the agent is not in a
	// SETTLED state (v2.16 W5 / design §3.1 "Reset 前置条件"). Reset wipes runtime
	// state (memory / plans / tasks per scope), so it is only legal from a state
	// where nothing is executing: stopped / error / failed. A running / stopping /
	// resetting agent must settle first. Distinct from ErrIllegalLifecycle so the
	// API can surface a precise "stop the agent before resetting" message. Maps to 409.
	ErrResetRequiresStopped = errors.New("agent: reset requires the agent to be stopped, errored, or failed first")
	ErrVersionConflict      = errors.New("agent: version conflict (optimistic lock)")
	// ErrInvalidExecutorProfile rejects an allowed_executors entry with an
	// unsupported cli or an empty model (v2.18.1 BE-1). Distinct sentinel so the API
	// maps the validation failure to 400. The CLI must be in SupportedExecutorCLIs
	// {claude-code, codex}; the model is free text but must be non-empty.
	ErrInvalidExecutorProfile = errors.New("agent: invalid executor profile (cli must be claude-code|codex and model must be non-empty)")
	// ErrUnsupportedCLI rejects creating an agent bound to a cli the runtime
	// cannot execute end-to-end (#181 / FINDING-F). Only "claude-code" is
	// runtime-dispatchable today; codex/opencode are probe-only (discovered +
	// shown in the Environment view, but agent.cli is not yet plumbed to the
	// worker's session starter — see IMPLEMENTATION_PLAN.md). empty / codex /
	// opencode / unknown are rejected.
	ErrUnsupportedCLI = errors.New("agent: unsupported cli (only claude-code is runtime-executable; codex/opencode are probe-only)")
	// ErrUnsupportedReasoning rejects a reasoning effort outside the allowlist
	// (T236). Empty is accepted (= runtime default). Maps to 400.
	ErrUnsupportedReasoning = errors.New("agent: unsupported reasoning effort (want one of minimal|low|medium|high)")
	// ErrAgentNotStoppedForArchive rejects archiving a running/transitioning agent
	// (v2.8 #272 (b) strict two-step) — the operator must stop it first. Maps to 409.
	ErrAgentNotStoppedForArchive = errors.New("agent: agent must be stopped before archive")
	// ErrAgentAlreadyArchived signals an idempotent re-archive (already archived).
	// The service treats it as a no-op success (200), not an error to the caller.
	ErrAgentAlreadyArchived = errors.New("agent: agent already archived")
	// ErrAgentArchived rejects an operation on a terminally-archived agent (e.g.
	// Start). Archived is terminal in v2.8 (no un-archive) — maps to 400, not 409,
	// because it is a fundamentally invalid request, not a transient state conflict.
	ErrAgentArchived = errors.New("agent: agent is archived (terminal)")
	// ErrTaskNotRunnable is the TaskRunGate sentinel (T130): start_work is
	// refused because the work item's task may not enter running (e.g. it is still
	// backlog / not yet dispatched). Kept as the agent BC's port contract even
	// though AgentWorkItem itself was retired (v2.14.0 F7 issue I14) — the gate +
	// the run-runnable surface survive (see runnable_gate.go / TaskRunGate).
	ErrTaskNotRunnable = errors.New("agent: work item task is not runnable")
)

// Profile is the Agent product profile.
type Profile struct {
	Name        string
	Description string
	Model       string
	CLI         string // e.g. "claudecode"
	// LLM tuning (T236). All optional; empty = the runtime/center default. Carried
	// the SAME way as Model/CLI (event → projector → reconcile command → session)
	// so editing them + restart applies on next spawn.
	Reasoning string            // reasoning effort: minimal|low|medium|high ("" = default)
	Mode      string            // operating mode ("" = default)
	Provider  string            // LLM provider ("" = center default)
	EnvVars   map[string]string // injected into the runtime process
	// Model-routing config (F3, design §5 & §10). All optional; carried the SAME
	// way as Model/Reasoning (domain → repo → migration → service → projector).
	// OrchestratorModel is the orchestrator's own model (cheap/fast tier); empty =
	// center default. DefaultExecutorModel is the fallback executor model; empty =
	// center default. MaxConcurrentTasks caps concurrent executors for the agent
	// (0/absent ⇒ EffectiveMaxConcurrentTasks default of 3).
	OrchestratorModel    string
	DefaultExecutorModel string
	MaxConcurrentTasks   int
	// AllowedExecutors is the AUTHORITATIVE executor-candidate list (v2.18.1 BE-1,
	// issue-8746a5b9): each entry is a {cli, model} profile, because an executor need
	// not share the orchestrator's CLI (e.g. a claude-code supervisor dispatching a
	// codex executor). Empty = concurrency not opted in. Persisted as a JSON array of
	// {cli, model} objects (migration 0085).
	AllowedExecutors []ExecutorProfile
	// AllowedModels is the LEGACY model-only candidate set (deprecated by
	// AllowedExecutors). It is no longer authoritative for the concurrency opt-in or
	// the center cap; it is kept as a DERIVED mirror (the distinct models of
	// AllowedExecutors) so the F3 model router (which still reads it until BE-2
	// migrates routing to {cli, model}) keeps working. Persisted as a JSON string
	// array like EnvVars/skills/tags.
	AllowedModels []string
}

// ExecutorProfile is one executor candidate: which CLI runs it and which model it
// runs with (v2.18.1 BE-1). The CLI is hard-validated against SupportedExecutorCLIs
// (a closed set the daemon can actually fork); the model is free text (non-empty) —
// provider model names rotate too often to hard-enumerate without going stale.
type ExecutorProfile struct {
	CLI   string `json:"cli"`
	Model string `json:"model"`
}

// SupportedExecutorCLIs is the closed set of executor CLIs the worker daemon can
// fork (BE-1 hard-validation; BE-2 adds the codex runner builder). claude-code is
// the historical default; codex is the cross-CLI executor this feature unlocks.
var SupportedExecutorCLIs = map[string]struct{}{
	"claude-code": {}, "codex": {},
}

// DefaultExecutorCLI is the CLI assumed when an agent's own cli is unset while
// backfilling legacy allowed_models into {cli, model} profiles (migration 0085 / the
// API's old-input compatibility path).
const DefaultExecutorCLI = "claude-code"

// IsSupportedExecutorCLI reports whether cli is a forkable executor CLI.
func IsSupportedExecutorCLI(cli string) bool {
	_, ok := SupportedExecutorCLIs[cli]
	return ok
}

// NormalizeAllowedExecutors validates + canonicalizes an executor-candidate list
// (v2.18.1 BE-1): every entry must carry a supported CLI and a non-empty (trimmed)
// model; exact {cli, model} duplicates are dropped, preserving first-seen order. A
// nil/empty input returns nil (no candidates → concurrency not opted in). It is the
// single choke point the domain, the persistence backfill, and the API all run
// candidates through, so the validation rule lives in exactly one place.
func NormalizeAllowedExecutors(in []ExecutorProfile) ([]ExecutorProfile, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[ExecutorProfile]struct{}, len(in))
	out := make([]ExecutorProfile, 0, len(in))
	for _, e := range in {
		e.Model = strings.TrimSpace(e.Model)
		if !IsSupportedExecutorCLI(e.CLI) {
			return nil, fmt.Errorf("%w: unsupported cli %q", ErrInvalidExecutorProfile, e.CLI)
		}
		if e.Model == "" {
			return nil, fmt.Errorf("%w: empty model for cli %q", ErrInvalidExecutorProfile, e.CLI)
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out, nil
}

// ExecutorsFromModels lifts a legacy model-only list into {cli, model} profiles
// using the SAME backfill rule as migration 0085: each model pairs with `cli` (the
// agent's own cli; empty → DefaultExecutorCLI). It is the back-compat path for an
// API caller that still sends allowed_models instead of allowed_executors. The
// result is NOT yet normalized — callers run it through NormalizeAllowedExecutors.
func ExecutorsFromModels(models []string, cli string) []ExecutorProfile {
	if len(models) == 0 {
		return nil
	}
	if cli == "" {
		cli = DefaultExecutorCLI
	}
	out := make([]ExecutorProfile, 0, len(models))
	for _, m := range models {
		out = append(out, ExecutorProfile{CLI: cli, Model: m})
	}
	return out
}

// ModelsOf returns the DISTINCT models of an executor list, first-seen order — the
// derived mirror written into the legacy allowed_models column so model-only readers
// (the F3 router, until BE-2) still see candidates.
func ModelsOf(execs []ExecutorProfile) []string {
	if len(execs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(execs))
	out := make([]string, 0, len(execs))
	for _, e := range execs {
		if _, ok := seen[e.Model]; ok {
			continue
		}
		seen[e.Model] = struct{}{}
		out = append(out, e.Model)
	}
	return out
}

// DefaultMaxConcurrentTasks is the fallback executor concurrency cap for an agent
// whose Profile.MaxConcurrentTasks is unset/zero (F3, design §5).
const DefaultMaxConcurrentTasks = 3

// EffectiveMaxConcurrentTasks returns the agent's executor concurrency cap,
// defaulting to DefaultMaxConcurrentTasks when MaxConcurrentTasks is unset/zero
// (or negative). The persisted column may legitimately be 0 (= "use the default").
func (p Profile) EffectiveMaxConcurrentTasks() int {
	if p.MaxConcurrentTasks <= 0 {
		return DefaultMaxConcurrentTasks
	}
	return p.MaxConcurrentTasks
}

// ConcurrencyEnabled is the SINGLE-SOURCE opt-in predicate for an agent running
// more than one task at a time (W1 "PD ruling, decision 2"): concurrency activates
// only when the profile sets MaxConcurrentTasks>0 AND lists ≥1 allowed executor
// (v2.18.1 BE-1: the authoritative list is now AllowedExecutors [{cli,model}], not
// the legacy model-only AllowedModels). A default agent (AllowedExecutors empty, the
// column DEFAULT '[]') is therefore NOT enabled even though migration 0082
// backfilled max_concurrent_tasks to 3 — so it keeps the historical single-active
// behaviour and never silently jumps to ≤3.
//
// This is the ONE definition both the worker daemon's executor-pool gate
// (workerdaemon/concurrent_exec.go) and the CENTER's ≤N start cap
// (projectmanager Service.enforceConcurrencyCap, via the OrgDirectory adapter)
// consult, so the two can never drift (v2.18.0 W4c, issue-b8687f2a §2).
func (p Profile) ConcurrencyEnabled() bool {
	return p.MaxConcurrentTasks > 0 && len(p.AllowedExecutors) > 0
}

// EffectiveConcurrencyCap is the agent's RUN-slot cap enforced by the center on
// every task→running transition (v2.18.0 W4c): the EffectiveMaxConcurrentTasks
// when concurrency is enabled, else 1 (single-active — no regression for default
// agents). This replaces the per-agent single-active UNIQUE index (migration 0072,
// dropped by 0084): a UNIQUE index can only express ≤1, never per-agent ≤N.
func (p Profile) EffectiveConcurrencyCap() int {
	if !p.ConcurrencyEnabled() {
		return 1
	}
	return p.EffectiveMaxConcurrentTasks()
}

// SupportedReasoningEfforts is the allowlist for Profile.Reasoning. Empty is
// always allowed (= the runtime default); a non-empty value must be one of these.
var SupportedReasoningEfforts = map[string]struct{}{
	"minimal": {}, "low": {}, "medium": {}, "high": {},
}

// IsSupportedReasoning reports whether r is a valid reasoning effort. Empty is
// valid (defaults to the runtime's own default).
func IsSupportedReasoning(r string) bool {
	if r == "" {
		return true
	}
	_, ok := SupportedReasoningEfforts[r]
	return ok
}

// Agent is the long-running Agent aggregate. The Worker binding is immutable in
// v2.7 (changing worker = new Agent, ADR-0049 §5 / Alt C). runtime home/memory
// live on the Worker (Environment); the layout is derived from worker+agent id.
type Agent struct {
	id             AgentID
	organizationID string
	profile        Profile
	skills         []string
	// capabilityTags are free-form specialty labels (FE / BE / platform / test /
	// integration / docs ...) the PD reads to dispatch work by capability + load
	// (T461). Distinct from skills (runtime CLI skill names): tags describe the
	// agent's role for human/PD assignment, not the loaded tool surface.
	capabilityTags []string
	workerID       string // immutable runtime binding
	lifecycle      AgentLifecycle
	lifecycleError string // populated when lifecycle == error
	createdBy      IdentityRef
	// identityMemberID is the agent identity-member (identity BC) this execution
	// Agent represents (v2.7 #157) — it holds the identity-member's identity ID
	// ("agent-<ulid>"), so Members can navigate member→AgentDetail by
	// member.identity_id == agent.identity_member_id. NOT an ADR-0033 actor ref
	// (createdBy is that); a plain id pointer. Optional: set by the unified
	// Members→Add Agent flow, empty for a standalone execution-agent create.
	identityMemberID string
	createdAt        time.Time
	updatedAt        time.Time
	version          int
}

// NewAgentInput captures constructor args.
type NewAgentInput struct {
	ID             AgentID
	OrganizationID string
	Profile        Profile
	Skills         []string
	CapabilityTags []string // free-form dispatch labels (T461); optional
	WorkerID       string   // required; immutable thereafter
	CreatedBy      IdentityRef
	// IdentityMemberID (optional, v2.7 #157) — the identity-member id ("agent-<ulid>")
	// this execution Agent represents; set by the unified Members→Add Agent flow.
	IdentityMemberID string
	CreatedAt        time.Time
}

// NewAgent constructs a fresh Agent in the stopped state. A Worker MUST be
// chosen at creation (ADR-0049 §5).
func NewAgent(in NewAgentInput) (*Agent, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("agent: id required")
	}
	if strings.TrimSpace(in.OrganizationID) == "" {
		return nil, errors.New("agent: organization_id required")
	}
	if strings.TrimSpace(in.Profile.Name) == "" {
		return nil, errors.New("agent: profile name required")
	}
	if strings.TrimSpace(in.WorkerID) == "" {
		return nil, ErrWorkerRequired
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return nil, err
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("agent: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Agent{
		id:               in.ID,
		organizationID:   in.OrganizationID,
		profile:          in.Profile,
		skills:           append([]string(nil), in.Skills...),
		capabilityTags:   normalizeTags(in.CapabilityTags),
		workerID:         in.WorkerID,
		lifecycle:        LifecycleStopped,
		createdBy:        in.CreatedBy,
		identityMemberID: in.IdentityMemberID,
		createdAt:        at,
		updatedAt:        at,
		version:          1,
	}, nil
}

// normalizeTags trims each tag, drops blanks, and de-duplicates case-insensitively
// while preserving the first-seen original casing and order. Returns nil for an
// empty result so the field round-trips as an empty JSON array via the repo.
func normalizeTags(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RehydrateAgentInput is for repository round-trip.
type RehydrateAgentInput struct {
	ID               AgentID
	OrganizationID   string
	Profile          Profile
	Skills           []string
	CapabilityTags   []string
	WorkerID         string
	Lifecycle        AgentLifecycle
	LifecycleError   string
	CreatedBy        IdentityRef
	IdentityMemberID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int
}

// RehydrateAgent reconstructs without invariant checks.
func RehydrateAgent(in RehydrateAgentInput) (*Agent, error) {
	if !in.Lifecycle.IsValid() {
		return nil, ErrInvalidLifecycle
	}
	if in.Version < 1 {
		return nil, errors.New("agent: version must be >= 1")
	}
	return &Agent{
		id:               in.ID,
		organizationID:   in.OrganizationID,
		profile:          in.Profile,
		skills:           append([]string(nil), in.Skills...),
		capabilityTags:   normalizeTags(in.CapabilityTags),
		workerID:         in.WorkerID,
		lifecycle:        in.Lifecycle,
		lifecycleError:   in.LifecycleError,
		createdBy:        in.CreatedBy,
		identityMemberID: in.IdentityMemberID,
		createdAt:        in.CreatedAt.UTC(),
		updatedAt:        in.UpdatedAt.UTC(),
		version:          in.Version,
	}, nil
}

// Getters.
func (a *Agent) ID() AgentID               { return a.id }
func (a *Agent) OrganizationID() string    { return a.organizationID }
func (a *Agent) Profile() Profile          { return a.profile }
func (a *Agent) WorkerID() string          { return a.workerID }
func (a *Agent) Lifecycle() AgentLifecycle { return a.lifecycle }
func (a *Agent) LifecycleError() string    { return a.lifecycleError }
func (a *Agent) CreatedBy() IdentityRef    { return a.createdBy }
func (a *Agent) IdentityMemberID() string  { return a.identityMemberID }
func (a *Agent) CreatedAt() time.Time      { return a.createdAt }
func (a *Agent) UpdatedAt() time.Time      { return a.updatedAt }
func (a *Agent) Version() int              { return a.version }

// Skills returns a defensive copy.
func (a *Agent) Skills() []string {
	if len(a.skills) == 0 {
		return nil
	}
	out := make([]string, len(a.skills))
	copy(out, a.skills)
	return out
}

// CapabilityTags returns a defensive copy of the dispatch labels (T461).
func (a *Agent) CapabilityTags() []string {
	if len(a.capabilityTags) == 0 {
		return nil
	}
	out := make([]string, len(a.capabilityTags))
	copy(out, a.capabilityTags)
	return out
}

// --- runtime home layout (ADR-0049 §5, plan §10 OQ7) -----------------------

// HomeSubdirs are the fixed subdirectories under an Agent's runtime home.
// Design §3: memory, plans, tasks (no config/logs/tmp/workspace).
var HomeSubdirs = []string{"memory", "plans", "tasks"}

// HomeRel returns the worker-relative home path:
// workers/{worker_id}/agents/{agent_id}. The Environment BC joins this under
// the worker's ~/.agent-center prefix.
func (a *Agent) HomeRel() string {
	return path.Join("workers", a.workerID, "agents", string(a.id))
}

// TasksDirRel returns the worker-relative tasks directory:
// workers/{worker_id}/agents/{agent_id}/tasks.
func (a *Agent) TasksDirRel() string {
	return path.Join(a.HomeRel(), "tasks")
}

// PlansDirRel returns the worker-relative plans directory:
// workers/{worker_id}/agents/{agent_id}/plans.
func (a *Agent) PlansDirRel() string {
	return path.Join(a.HomeRel(), "plans")
}

// MemoryDirRel returns the worker-relative memory directory:
// workers/{worker_id}/agents/{agent_id}/memory.
func (a *Agent) MemoryDirRel() string {
	return path.Join(a.HomeRel(), "memory")
}

// --- lifecycle intent transitions (ADR-0049 §5) ----------------------------

// Start moves stopped/error/failed → running. Starting a terminal-FAILED agent is
// the operator's MANUAL recovery out of the crash-loop circuit-breaker.
func (a *Agent) Start(at time.Time) error {
	// Archived is terminal (v2.8 #272): you cannot start an archived agent. Distinct
	// error → 400 (fundamentally invalid), not 409 (transient conflict).
	if a.lifecycle == LifecycleArchived {
		return ErrAgentArchived
	}
	if a.lifecycle != LifecycleStopped && a.lifecycle != LifecycleError && a.lifecycle != LifecycleFailed {
		return ErrIllegalLifecycle
	}
	a.lifecycle = LifecycleRunning
	a.lifecycleError = ""
	a.touch(at)
	return nil
}

// Stop is an OPERATIONAL stop: running → stopping. It must NOT touch any
// AgentWorkItem (blocked is an explicit business state, ADR-0049 §5); the
// Agent BC simply makes active work appear paused via availability.
func (a *Agent) Stop(at time.Time) error {
	if a.lifecycle != LifecycleRunning {
		return ErrIllegalLifecycle
	}
	a.lifecycle = LifecycleStopping
	a.touch(at)
	return nil
}

// Restart requests a restart while keeping the running intent (running →
// running, version bumps to signal the Environment).
func (a *Agent) Restart(at time.Time) error {
	if a.lifecycle != LifecycleRunning {
		return ErrIllegalLifecycle
	}
	a.touch(at)
	return nil
}

// Reset moves the Agent to resetting for the given scope. The second
// confirmation is enforced by the AppService/UI, not here.
//
// Precondition (v2.16 W5 / design §3.1): Reset wipes runtime state (memory /
// plans / tasks, per scope), so it is only legal from a SETTLED lifecycle where
// nothing is executing — stopped / error / failed. Issuing it on a running /
// stopping / resetting agent returns ErrResetRequiresStopped (the operator must
// stop it first); a fresh archived agent is rejected by IsValid-less fallthrough
// to the same precondition. This subsumes the old "double-reset is illegal" guard
// (resetting is simply not a settled state).
func (a *Agent) Reset(scope ResetScope, at time.Time) error {
	if !scope.IsValid() {
		return ErrInvalidResetScope
	}
	switch a.lifecycle {
	case LifecycleStopped, LifecycleError, LifecycleFailed:
		// settled — nothing executing → resettable
	default: // running, stopping, resetting, archived
		return ErrResetRequiresStopped
	}
	a.lifecycle = LifecycleResetting
	a.touch(at)
	return nil
}

// Archive soft-deletes the Agent (v2.8 #272): a terminal transition that retires
// it from the user surface while RETAINING the row (history). It is the sole
// user-facing delete path; the second confirmation is enforced by the
// AppService/UI (ConfirmModal), not here.
//
//   - (b) strict two-step: only a SETTLED non-running agent may be archived
//     (stopped / error / failed). A running/stopping/resetting agent →
//     ErrAgentNotStoppedForArchive (409) — the operator stops it first.
//   - Idempotent: archiving an already-archived agent → ErrAgentAlreadyArchived,
//     which the service maps to a 200 no-op (no re-persist, no double version bump).
//   - Clears the worker binding (workerID="") — the worker is freed to re-bind.
//     The agent is already stopped, so its control channel is already torn down;
//     archive just releases the binding (persisted via the dedicated repo Archive).
func (a *Agent) Archive(at time.Time) error {
	if a.lifecycle == LifecycleArchived {
		return ErrAgentAlreadyArchived
	}
	switch a.lifecycle {
	case LifecycleStopped, LifecycleError, LifecycleFailed:
		// settled, non-running → archivable
	default: // running, stopping, resetting
		return ErrAgentNotStoppedForArchive
	}
	a.lifecycle = LifecycleArchived
	a.lifecycleError = ""
	a.workerID = "" // release the binding; the worker becomes re-bindable
	a.touch(at)
	return nil
}

// MarkStopped is the Environment feedback that a stopping/resetting Agent has
// settled to stopped.
func (a *Agent) MarkStopped(at time.Time) error {
	if a.lifecycle != LifecycleStopping && a.lifecycle != LifecycleResetting {
		return ErrIllegalLifecycle
	}
	a.lifecycle = LifecycleStopped
	a.touch(at)
	return nil
}

// MarkError records an Environment-reported error state.
func (a *Agent) MarkError(msg string, at time.Time) {
	a.lifecycle = LifecycleError
	a.lifecycleError = msg
	a.touch(at)
}

// MarkRecovered is the Environment feedback that a CRASHED agent's session is back
// up (issue I13 auto-recovery): it clears error → running so the agent becomes
// AVAILABLE for dispatch again and the UI stops showing it crashed. It acts ONLY on
// the TRANSIENT `error` state — every other state is a deliberate NO-OP so a stale or
// racing "running" feedback can never resurrect a deliberately stopped/stopping/
// resetting agent, un-latch the TERMINAL `failed` circuit-breaker (manual recovery
// only), revive an archived agent, or needlessly re-touch an already-running one. Like
// MarkError/MarkStopped it is a RESULT feedback (persist-only at the service layer; no
// outbox emit → no reconcile loop).
func (a *Agent) MarkRecovered(at time.Time) {
	if a.lifecycle != LifecycleError {
		return
	}
	a.lifecycle = LifecycleRunning
	a.lifecycleError = ""
	a.touch(at)
}

// MarkFailed records the TERMINAL crash-loop circuit-breaker state (v2.7 GATE-7
// Mode-B): the worker's self-heal exhausted its bounded relaunch attempts. Valid
// only from running/error (the agent was up / transiently crashing). The only way
// OUT is a manual Start/Reset. msg is the last crash cause (surfaced to the operator).
func (a *Agent) MarkFailed(msg string, at time.Time) error {
	if a.lifecycle != LifecycleRunning && a.lifecycle != LifecycleError {
		return ErrIllegalLifecycle
	}
	a.lifecycle = LifecycleFailed
	a.lifecycleError = msg
	a.touch(at)
	return nil
}

// UpdateProfile replaces the editable profile. Per ADR-0049 §5 this does NOT
// restart the Agent; the new config applies on next restart.
func (a *Agent) UpdateProfile(p Profile, at time.Time) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("agent: profile name required")
	}
	a.profile = p
	a.touch(at)
	return nil
}

// SetSkills replaces the skill list (also applies on next restart).
func (a *Agent) SetSkills(skills []string, at time.Time) {
	a.skills = append([]string(nil), skills...)
	a.touch(at)
}

// SetCapabilityTags replaces the dispatch labels (T461). Tags are normalized
// (trimmed, blanks dropped, case-insensitively de-duplicated). Pure metadata —
// it does not affect the runtime, so unlike a profile edit it carries no restart
// implication; it just changes what the PD sees in find_org_agent.
func (a *Agent) SetCapabilityTags(tags []string, at time.Time) {
	a.capabilityTags = normalizeTags(tags)
	a.touch(at)
}

func (a *Agent) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	a.updatedAt = at.UTC()
	a.version++
}
