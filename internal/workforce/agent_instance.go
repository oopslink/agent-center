package workforce

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// AgentInstanceID is the typed ULID PK.
type AgentInstanceID string

// String returns the typed value.
func (id AgentInstanceID) String() string { return string(id) }

// AgentInstanceState is the 4-state lifecycle (ADR-0024 § 3 + ADR-0029 § 4).
type AgentInstanceState string

const (
	AgentInstanceIdle     AgentInstanceState = "idle"
	AgentInstanceActive   AgentInstanceState = "active"
	AgentInstanceSleeping AgentInstanceState = "sleeping"
	AgentInstanceArchived AgentInstanceState = "archived"
)

// IsValid reports enum membership.
func (s AgentInstanceState) IsValid() bool {
	switch s {
	case AgentInstanceIdle, AgentInstanceActive, AgentInstanceSleeping, AgentInstanceArchived:
		return true
	}
	return false
}

// IsTerminal returns true for archived state.
func (s AgentInstanceState) IsTerminal() bool { return s == AgentInstanceArchived }

// String returns the underlying value.
func (s AgentInstanceState) String() string { return string(s) }

// AgentInstanceArchivedReason categorises archive cause.
type AgentInstanceArchivedReason string

const (
	AgentInstanceArchivedReasonManual          AgentInstanceArchivedReason = "manual"
	AgentInstanceArchivedReasonWorkerRetired   AgentInstanceArchivedReason = "worker_retired"
)

// IsValid reports enum membership.
func (r AgentInstanceArchivedReason) IsValid() bool {
	switch r {
	case AgentInstanceArchivedReasonManual, AgentInstanceArchivedReasonWorkerRetired:
		return true
	}
	return false
}

// String returns the underlying value.
func (r AgentInstanceArchivedReason) String() string { return string(r) }

// AgentInstance is the Workforce BC AR (ADR-0024 + ADR-0029).
//
// Invariants per ADR-0024 § 9:
//  1. name globally unique
//  2. worker_id immutable post-create (NULL for built-in)
//  3. archived terminal — irreversible
//  4. state ↔ worker linkage (offline/online flip sleeping/idle)
//  5. archive blocked when active or sleeping (must idle first)
//  6. is_builtin / name / agent_cli / is_builtin immutable post-create
//  7. built-in cannot be archived
type AgentInstance struct {
	id              AgentInstanceID
	name            string
	agentCLI        string
	workerID        *WorkerID // nullable iff isBuiltin
	config          string    // raw JSON; opaque to AR (validated by service layer)
	maxConcurrent   *int      // nullable = no AR-level cap
	state           AgentInstanceState
	isBuiltin       bool
	// v2.6 fields (BE-7)
	identityID      string // FK identities.id; empty for pre-v2.6 or supervisor rows
	organizationID  string // FK organizations.id (app-layer enforced)
	kind            string // closed enum: 'agent' (supervisor cut in v2.6)
	createdAt       time.Time
	archivedAt      *time.Time
	archivedReason  AgentInstanceArchivedReason
	archivedMessage string
	version         int
}

// NewAgentInstanceInput is the constructor input. Caller passes WorkerID==nil
// only when IsBuiltin is true; the constructor enforces the XOR.
type NewAgentInstanceInput struct {
	ID             AgentInstanceID
	Name           string
	AgentCLI       string
	WorkerID       *WorkerID
	Config         string // JSON; pass "{}" if empty
	MaxConcurrent  *int
	IsBuiltin      bool
	// v2.6 fields (BE-7)
	IdentityID     string // FK identities.id; empty string for pre-v2.6 / supervisor
	OrganizationID string // FK organizations.id
	CreatedAt      time.Time
}

// NewAgentInstance constructs a fresh idle AgentInstance.
func NewAgentInstance(in NewAgentInstanceInput) (*AgentInstance, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("agent instance: id required")
	}
	if err := validateAgentInstanceName(in.Name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.AgentCLI) == "" {
		return nil, errors.New("agent instance: agent_cli required")
	}
	// XOR: built-in → workerID must be nil; non-builtin → workerID required.
	if in.IsBuiltin && in.WorkerID != nil {
		return nil, errors.New("agent instance: built-in cannot have worker_id")
	}
	if !in.IsBuiltin && in.WorkerID == nil {
		return nil, errors.New("agent instance: non-builtin requires worker_id")
	}
	if in.WorkerID != nil {
		if err := validateWorkerID(*in.WorkerID); err != nil {
			return nil, err
		}
	}
	if in.MaxConcurrent != nil && *in.MaxConcurrent < 1 {
		return nil, errors.New("agent instance: max_concurrent must be >= 1 (or nil for no cap)")
	}
	config := in.Config
	if config == "" {
		config = "{}"
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("agent instance: created_at required")
	}
	kind := "agent"
	return &AgentInstance{
		id:             in.ID,
		name:           in.Name,
		agentCLI:       in.AgentCLI,
		workerID:       copyWorkerIDPtr(in.WorkerID),
		config:         config,
		maxConcurrent:  copyIntPtr(in.MaxConcurrent),
		state:          AgentInstanceIdle,
		isBuiltin:      in.IsBuiltin,
		identityID:     in.IdentityID,
		organizationID: in.OrganizationID,
		kind:           kind,
		createdAt:      in.CreatedAt.UTC(),
		version:        1,
	}, nil
}

// RehydrateAgentInstanceInput is for Repository implementations only.
type RehydrateAgentInstanceInput struct {
	ID              AgentInstanceID
	Name            string
	AgentCLI        string
	WorkerID        *WorkerID
	Config          string
	MaxConcurrent   *int
	State           AgentInstanceState
	IsBuiltin       bool
	// v2.6 fields (BE-7)
	IdentityID      string
	OrganizationID  string
	Kind            string
	CreatedAt       time.Time
	ArchivedAt      *time.Time
	ArchivedReason  AgentInstanceArchivedReason
	ArchivedMessage string
	Version         int
}

// RehydrateAgentInstance reconstructs from persisted state.
func RehydrateAgentInstance(in RehydrateAgentInstanceInput) (*AgentInstance, error) {
	if !in.State.IsValid() {
		return nil, fmt.Errorf("agent instance: invalid state %q", in.State)
	}
	if in.Version < 1 {
		return nil, errors.New("agent instance: version must be >= 1")
	}
	config := in.Config
	if config == "" {
		config = "{}"
	}
	kind := in.Kind
	if kind == "" {
		kind = "agent"
	}
	return &AgentInstance{
		id:              in.ID,
		name:            in.Name,
		agentCLI:        in.AgentCLI,
		workerID:        copyWorkerIDPtr(in.WorkerID),
		config:          config,
		maxConcurrent:   copyIntPtr(in.MaxConcurrent),
		state:           in.State,
		isBuiltin:       in.IsBuiltin,
		identityID:      in.IdentityID,
		organizationID:  in.OrganizationID,
		kind:            kind,
		createdAt:       in.CreatedAt.UTC(),
		archivedAt:      copyTimePtr(in.ArchivedAt),
		archivedReason:  in.ArchivedReason,
		archivedMessage: in.ArchivedMessage,
		version:         in.Version,
	}, nil
}

// Getters.

func (a *AgentInstance) ID() AgentInstanceID                       { return a.id }
func (a *AgentInstance) Name() string                              { return a.name }
func (a *AgentInstance) AgentCLI() string                          { return a.agentCLI }
func (a *AgentInstance) WorkerID() *WorkerID                       { return copyWorkerIDPtr(a.workerID) }
func (a *AgentInstance) Config() string                            { return a.config }
func (a *AgentInstance) MaxConcurrent() *int                       { return copyIntPtr(a.maxConcurrent) }
func (a *AgentInstance) State() AgentInstanceState                 { return a.state }
func (a *AgentInstance) IsBuiltin() bool                           { return a.isBuiltin }
// v2.6 getters (BE-7)
func (a *AgentInstance) IdentityID() string                        { return a.identityID }
func (a *AgentInstance) OrganizationID() string                    { return a.organizationID }
func (a *AgentInstance) Kind() string                              { return a.kind }
func (a *AgentInstance) CreatedAt() time.Time                      { return a.createdAt }
func (a *AgentInstance) ArchivedAt() *time.Time                    { return copyTimePtr(a.archivedAt) }
func (a *AgentInstance) ArchivedReason() AgentInstanceArchivedReason { return a.archivedReason }
func (a *AgentInstance) ArchivedMessage() string                   { return a.archivedMessage }
func (a *AgentInstance) Version() int                              { return a.version }

// SetConfig replaces the JSON config blob. Bumps version. The config string
// itself is opaque to the AR; service layer validates JSON shape.
func (a *AgentInstance) SetConfig(at time.Time, newConfig string) error {
	if a.state == AgentInstanceArchived {
		return ErrAgentInstanceArchived
	}
	if newConfig == "" {
		newConfig = "{}"
	}
	a.config = newConfig
	_ = at // updated_at column not stored on AR yet; bumping version is enough for CAS
	a.version++
	return nil
}

// SetMaxConcurrent updates the max_concurrent cap.
func (a *AgentInstance) SetMaxConcurrent(at time.Time, newCap *int) error {
	if a.state == AgentInstanceArchived {
		return ErrAgentInstanceArchived
	}
	if newCap != nil && *newCap < 1 {
		return errors.New("agent instance: max_concurrent must be >= 1 (or nil for no cap)")
	}
	a.maxConcurrent = copyIntPtr(newCap)
	a.version++
	return nil
}

// State transitions (ADR-0024 § 3 + ADR-0029 § 4).

// MarkActive transitions idle → active. No-op if already active.
func (a *AgentInstance) MarkActive() error {
	switch a.state {
	case AgentInstanceActive:
		return nil
	case AgentInstanceIdle:
		a.state = AgentInstanceActive
		a.version++
		return nil
	default:
		return fmt.Errorf("agent instance: cannot transition %s → active", a.state)
	}
}

// MarkIdle transitions active → idle. No-op if already idle. Other states
// (sleeping / archived) reject.
func (a *AgentInstance) MarkIdle() error {
	switch a.state {
	case AgentInstanceIdle:
		return nil
	case AgentInstanceActive:
		a.state = AgentInstanceIdle
		a.version++
		return nil
	default:
		return fmt.Errorf("agent instance: cannot transition %s → idle", a.state)
	}
}

// MarkSleeping transitions idle / active → sleeping (worker offline).
func (a *AgentInstance) MarkSleeping() error {
	switch a.state {
	case AgentInstanceSleeping:
		return nil
	case AgentInstanceIdle, AgentInstanceActive:
		a.state = AgentInstanceSleeping
		a.version++
		return nil
	default:
		return fmt.Errorf("agent instance: cannot transition %s → sleeping", a.state)
	}
}

// MarkAwakened transitions sleeping → idle (worker online).
func (a *AgentInstance) MarkAwakened() error {
	switch a.state {
	case AgentInstanceIdle:
		return nil
	case AgentInstanceSleeping:
		a.state = AgentInstanceIdle
		a.version++
		return nil
	default:
		return fmt.Errorf("agent instance: cannot transition %s → idle (awakened)", a.state)
	}
}

// Archive transitions idle → archived. Built-in agents reject (per
// ADR-0029 § 5). Active / sleeping reject (must idle first per ADR-0024 § 9).
func (a *AgentInstance) Archive(at time.Time, reason AgentInstanceArchivedReason, message string) error {
	if a.isBuiltin {
		return ErrAgentInstanceIsBuiltin
	}
	if a.state != AgentInstanceIdle {
		return fmt.Errorf("agent instance: cannot archive from state %s (must be idle)", a.state)
	}
	if !reason.IsValid() {
		return fmt.Errorf("agent instance: invalid archived reason %q", reason)
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("agent instance: archived message required (conventions § 16)")
	}
	at = at.UTC()
	a.state = AgentInstanceArchived
	a.archivedAt = &at
	a.archivedReason = reason
	a.archivedMessage = message
	a.version++
	return nil
}

// HomeDirPath returns the convention path for this instance's home dir
// (ADR-0029 § 3). Built-in supervisor uses /name/, worker agent uses /id/.
func (a *AgentInstance) HomeDirPath() string {
	if a.isBuiltin {
		return fmt.Sprintf("~/.agent-center/agents/%s/", a.name)
	}
	return fmt.Sprintf("~/.agent-center-worker/agents/%s/", a.id)
}

// HasMCPConfig reports whether the AgentInstance.config JSON references any
// MCP config (per ADR-0027). v2 dispatch feature-check uses this to short-
// circuit the supports_mcp probe — if no MCP config, no need to require
// adapter.SupportsMCP.
//
// Detection is a substring scan for the "mcp_config" key in the raw JSON;
// avoids parsing in the AR layer. Service callers may do a stricter parse.
func (a *AgentInstance) HasMCPConfig() bool {
	if a == nil {
		return false
	}
	return strings.Contains(a.config, "\"mcp_config\"")
}

// HasSkillsHint reports whether the AgentInstance.config JSON references any
// skills attachment (per ADR-0028). Same heuristic as HasMCPConfig.
func (a *AgentInstance) HasSkillsHint() bool {
	if a == nil {
		return false
	}
	return strings.Contains(a.config, "\"skills\"")
}

func validateAgentInstanceName(name string) error {
	s := strings.TrimSpace(name)
	if s == "" {
		return errors.New("agent instance: name required")
	}
	if len(s) > 128 {
		return errors.New("agent instance: name too long (max 128)")
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.':
		default:
			return fmt.Errorf("agent instance: name %q contains invalid character %q", s, c)
		}
	}
	return nil
}

func copyWorkerIDPtr(p *WorkerID) *WorkerID {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// AgentInstance sentinel errors.
var (
	ErrAgentInstanceNotFound        = errors.New("workforce: agent instance not found")
	ErrAgentInstanceAlreadyExists   = errors.New("workforce: agent instance id already exists")
	ErrAgentInstanceNameTaken       = errors.New("workforce: agent instance name already taken")
	ErrAgentInstanceVersionConflict = errors.New("workforce: agent instance version conflict (optimistic lock)")
	ErrAgentInstanceArchived        = errors.New("workforce: agent instance is archived (terminal)")
	ErrAgentInstanceIsBuiltin       = errors.New("workforce: built-in agent instance cannot be archived (ADR-0029 § 5)")
)

// BuiltinSupervisorName is the canonical name for the system-provisioned
// supervisor AgentInstance (per ADR-0029 § 2).
const BuiltinSupervisorName = "supervisor"

// BuiltinSupervisorDefaultAgentCLI is the v2 default CLI for the supervisor
// (per ADR-0029 § 2).
const BuiltinSupervisorDefaultAgentCLI = "claude-code"
