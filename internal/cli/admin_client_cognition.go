// Package cli — admin_client_cognition.go: Client methods for the
// Cognition BC admin surface (supervisor spawn / decision record /
// invocation + decision reads). Mirrors internal/admin/api/cognition.go 1:1.
package cli

import (
	"context"
	"time"
)

// =============================================================================
// DTOs — JSON shape returned by admin/api/cognition.go projection helpers.
// =============================================================================

// InvocationDTO mirrors admin api invocationMap.
type InvocationDTO struct {
	ID                 string    `json:"id"`
	AgentInstanceID    string    `json:"agent_instance_id"`
	ScopeKind          string    `json:"scope_kind"`
	ScopeKey           string    `json:"scope_key"`
	Status             string    `json:"status"`
	HardTimeoutSeconds int       `json:"hard_timeout_seconds"`
	StartedAt          string    `json:"started_at"`
	FailedReason       string    `json:"failed_reason"`
	FailedMessage      string    `json:"failed_message"`
	DecisionsMade      int       `json:"decisions_made"`
	PromptBlobRef      string    `json:"prompt_blob_ref"`
	CreatedAt          string    `json:"created_at"`
	UpdatedAt          string    `json:"updated_at"`
	Version            int64     `json:"version"`
	EndedAt            string    `json:"ended_at,omitempty"`
	TimedOutAt         string    `json:"timed_out_at,omitempty"`
	TriggerEventIDs    []string  `json:"trigger_event_ids,omitempty"`
}

// DecisionDTO mirrors admin api decisionMap.
type DecisionDTO struct {
	ID             string `json:"id"`
	InvocationID   string `json:"invocation_id"`
	Kind           string `json:"kind"`
	TargetRefsJSON string `json:"target_refs_json"`
	Rationale      string `json:"rationale"`
	Outcome        string `json:"outcome"`
	OutcomeMessage string `json:"outcome_message"`
	CreatedAt      string `json:"created_at"`
}

// =============================================================================
// Request payloads.
// =============================================================================

// SupervisorSpawnRequest mirrors api spawnSupervisorReq.
type SupervisorSpawnRequest struct {
	ScopeKind     string   `json:"scope_kind"`
	ScopeKey      string   `json:"scope_key"`
	TriggerEvents []string `json:"trigger_event_ids"`
}

// SupervisorSpawnResponse mirrors api success body.
type SupervisorSpawnResponse struct {
	InvocationID string `json:"invocation_id"`
}

// DecisionRecordRequest mirrors api recordDecisionReq.
type DecisionRecordRequest struct {
	InvocationID   string `json:"invocation_id"`
	Kind           string `json:"kind"`
	TargetRefsJSON string `json:"target_refs_json"`
	Rationale      string `json:"rationale"`
	Outcome        string `json:"outcome"`
	OutcomeMessage string `json:"outcome_message"`
}

// DecisionRecordResponse mirrors api success body.
type DecisionRecordResponse struct {
	DecisionID string `json:"decision_id"`
}

// InvocationSaveRequest mirrors api invocationSaveReq. We keep
// time.Time fields here (vs string) because the admin handler decodes
// JSON with time.Time directly — the wire serialisation matches Go's
// default RFC3339Nano format.
type InvocationSaveRequest struct {
	ID                 string    `json:"id"`
	AgentInstanceID    string    `json:"agent_instance_id"`
	ScopeKind          string    `json:"scope_kind"`
	ScopeKey           string    `json:"scope_key"`
	TriggerEventIDs    []string  `json:"trigger_event_ids"`
	Status             string    `json:"status"`
	HardTimeoutSeconds int       `json:"hard_timeout_seconds"`
	StartedAt          time.Time `json:"started_at"`
	PromptBlobRef      string    `json:"prompt_blob_ref"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Version            int64     `json:"version"`
}

// InvocationSaveResponse mirrors api success body.
type InvocationSaveResponse struct {
	InvocationID string `json:"invocation_id"`
}

// InvocationUpdateStatusRequest mirrors api invocationUpdateStatusReq.
type InvocationUpdateStatusRequest struct {
	InvocationID string `json:"invocation_id"`
}

// =============================================================================
// SupervisorSpawner — Spawn
// =============================================================================

// SupervisorSpawn POSTs /admin/cognition/supervisor/spawn.
func (c *Client) SupervisorSpawn(ctx context.Context, req SupervisorSpawnRequest) (SupervisorSpawnResponse, error) {
	var res SupervisorSpawnResponse
	err := c.postJSON(ctx, "/admin/cognition/supervisor/spawn", req, &res)
	return res, err
}

// =============================================================================
// DecisionRecorder — Record
// =============================================================================

// DecisionRecord POSTs /admin/cognition/decision/record.
func (c *Client) DecisionRecord(ctx context.Context, req DecisionRecordRequest) (DecisionRecordResponse, error) {
	var res DecisionRecordResponse
	err := c.postJSON(ctx, "/admin/cognition/decision/record", req, &res)
	return res, err
}

// =============================================================================
// InvocationRepo — FindByID / Save / UpdateStatusToTerminal
// =============================================================================

// InvocationFindByID GETs /admin/cognition/invocation/find-by-id?id=…
func (c *Client) InvocationFindByID(ctx context.Context, id string) (InvocationDTO, error) {
	var out InvocationDTO
	err := c.getJSON(ctx, "/admin/cognition/invocation/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// InvocationSave POSTs /admin/cognition/invocation/save.
func (c *Client) InvocationSave(ctx context.Context, req InvocationSaveRequest) (InvocationSaveResponse, error) {
	var res InvocationSaveResponse
	err := c.postJSON(ctx, "/admin/cognition/invocation/save", req, &res)
	return res, err
}

// InvocationUpdateStatusToTerminal POSTs /admin/cognition/invocation/update-status-to-terminal.
func (c *Client) InvocationUpdateStatusToTerminal(ctx context.Context, req InvocationUpdateStatusRequest) (InvocationSaveResponse, error) {
	var res InvocationSaveResponse
	err := c.postJSON(ctx, "/admin/cognition/invocation/update-status-to-terminal", req, &res)
	return res, err
}

// =============================================================================
// DecisionRepo — FindByInvocationID
// =============================================================================

// DecisionFindByInvocationID GETs /admin/cognition/decision/find-by-invocation-id?invocation_id=…
func (c *Client) DecisionFindByInvocationID(ctx context.Context, invocationID string) ([]DecisionDTO, error) {
	var out []DecisionDTO
	err := c.getJSON(ctx, "/admin/cognition/decision/find-by-invocation-id"+
		buildQuery("invocation_id", invocationID), &out)
	return out, err
}
