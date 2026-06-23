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
	ResetWorkspace ResetScope = "workspace" // wipe {home}/workspace
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
	ErrVersionConflict    = errors.New("agent: version conflict (optimistic lock)")
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
	WorkerID       string // required; immutable thereafter
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
		workerID:         in.WorkerID,
		lifecycle:        LifecycleStopped,
		createdBy:        in.CreatedBy,
		identityMemberID: in.IdentityMemberID,
		createdAt:        at,
		updatedAt:        at,
		version:          1,
	}, nil
}

// RehydrateAgentInput is for repository round-trip.
type RehydrateAgentInput struct {
	ID               AgentID
	OrganizationID   string
	Profile          Profile
	Skills           []string
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

// --- runtime home layout (ADR-0049 §5, plan §10 OQ7) -----------------------

// HomeSubdirs are the fixed subdirectories under an Agent's runtime home.
var HomeSubdirs = []string{"config", "logs", "tmp", "memory", "workspace"}

// HomeRel returns the worker-relative home path:
// workers/{worker_id}/agents/{agent_id}. The Environment BC joins this under
// the worker's ~/.agent-center prefix.
func (a *Agent) HomeRel() string {
	return path.Join("workers", a.workerID, "agents", string(a.id))
}

// DefaultWorkspaceRel returns the default current working directory
// ({home}/workspace), NOT the home root (plan §10 OQ7).
func (a *Agent) DefaultWorkspaceRel() string {
	return path.Join(a.HomeRel(), "workspace")
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
func (a *Agent) Reset(scope ResetScope, at time.Time) error {
	if !scope.IsValid() {
		return ErrInvalidResetScope
	}
	if a.lifecycle == LifecycleResetting {
		return ErrIllegalLifecycle
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

func (a *Agent) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	a.updatedAt = at.UTC()
	a.version++
}
