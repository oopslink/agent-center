package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	cogdec "github.com/oopslink/agent-center/internal/cognition/decision"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/observability"
)

// =============================================================================
// SupervisorSpawner — Spawn
// =============================================================================

type spawnSupervisorReq struct {
	ScopeKind     string   `json:"scope_kind"`
	ScopeKey      string   `json:"scope_key"`
	TriggerEvents []string `json:"trigger_event_ids"`
}

func (s *Server) supervisorSpawnHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.SupervisorSpawner == nil {
		writeError(w, http.StatusNotImplemented, "spawner_not_wired", "")
		return
	}
	var req spawnSupervisorReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	scope, err := cognition.NewInvocationScope(cognition.ScopeKind(req.ScopeKind), req.ScopeKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_scope", err.Error())
		return
	}
	eids := make([]observability.EventID, 0, len(req.TriggerEvents))
	for _, id := range req.TriggerEvents {
		if id != "" {
			eids = append(eids, observability.EventID(id))
		}
	}
	triggers, err := cognition.NewTriggerEventSet(eids)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_trigger_events", err.Error())
		return
	}
	newID, err := d.SupervisorSpawner.Spawn(r.Context(), scheduler.InvocationRequest{
		Scope:         scope,
		TriggerEvents: triggers,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invocation_id": string(newID)})
}

// =============================================================================
// DecisionRecorder — Record (supervisor-only; user calls become silent no-ops)
// =============================================================================

type recordDecisionReq struct {
	InvocationID   string `json:"invocation_id"`
	Kind           string `json:"kind"`
	TargetRefsJSON string `json:"target_refs_json"`
	Rationale      string `json:"rationale"`
	Outcome        string `json:"outcome"`
	OutcomeMessage string `json:"outcome_message"`
}

func (s *Server) decisionRecordHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.DecisionRecorder == nil {
		writeError(w, http.StatusNotImplemented, "decision_recorder_not_wired", "")
		return
	}
	var req recordDecisionReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	actor := cogdec.Actor{
		Kind:         "supervisor",
		ID:           req.InvocationID,
		InvocationID: cognition.InvocationID(req.InvocationID),
	}
	outcome := cognition.DecisionOutcome(req.Outcome)
	if outcome == "" {
		outcome = cognition.OutcomeSucceeded
	}
	// DecisionRecorder.Record is meant to be called inside an outer tx
	// (per recorder.go doc); the admin transport just hands through —
	// callers that need atomicity should batch via a future bundle endpoint.
	did, err := d.DecisionRecorder.Record(r.Context(), actor, cogdec.RecordRequest{
		Kind:           cognition.DecisionKind(req.Kind),
		TargetRefsJSON: req.TargetRefsJSON,
		Rationale:      req.Rationale,
		Outcome:        outcome,
		OutcomeMessage: req.OutcomeMessage,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision_id": string(did)})
}

// =============================================================================
// InvocationRepo — FindByID / Save / UpdateStatusToTerminal
// =============================================================================

func (s *Server) invocationFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvocationRepo == nil {
		writeError(w, http.StatusNotImplemented, "invocation_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	inv, err := d.InvocationRepo.FindByID(r.Context(), cognition.InvocationID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invocationMap(inv))
}

// invocationSaveHandler accepts a previously-Spawn'd invocation row. v2.2-A2
// scope keeps this an opaque pass-through: caller supplies all fields, we
// rehydrate via cognition.Rehydrate, then Save. Used by tooling that
// replays invocations or seeds tests; never called by the production CLI.
type invocationSaveReq struct {
	ID                 string             `json:"id"`
	AgentInstanceID    string             `json:"agent_instance_id"`
	ScopeKind          string             `json:"scope_kind"`
	ScopeKey           string             `json:"scope_key"`
	TriggerEventIDs    []string           `json:"trigger_event_ids"`
	Status             string             `json:"status"`
	HardTimeoutSeconds int                `json:"hard_timeout_seconds"`
	StartedAt          time.Time          `json:"started_at"`
	PromptBlobRef      string             `json:"prompt_blob_ref"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	Version            int64              `json:"version"`
}

func (s *Server) invocationSaveHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvocationRepo == nil {
		writeError(w, http.StatusNotImplemented, "invocation_repo_not_wired", "")
		return
	}
	var req invocationSaveReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	scope, err := cognition.NewInvocationScope(cognition.ScopeKind(req.ScopeKind), req.ScopeKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_scope", err.Error())
		return
	}
	eids := make([]observability.EventID, 0, len(req.TriggerEventIDs))
	for _, id := range req.TriggerEventIDs {
		if id != "" {
			eids = append(eids, observability.EventID(id))
		}
	}
	triggers, err := cognition.NewTriggerEventSet(eids)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_trigger_events", err.Error())
		return
	}
	inv, err := cognition.Rehydrate(cognition.RehydrateInput{
		ID:                 cognition.InvocationID(req.ID),
		AgentInstanceID:    req.AgentInstanceID,
		Scope:              scope,
		TriggerEvents:      triggers,
		Status:             cognition.InvocationStatus(req.Status),
		HardTimeoutSeconds: req.HardTimeoutSeconds,
		StartedAt:          req.StartedAt,
		PromptBlobRef:      req.PromptBlobRef,
		CreatedAt:          req.CreatedAt,
		UpdatedAt:          req.UpdatedAt,
		Version:            req.Version,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_invocation", err.Error())
		return
	}
	if err := d.InvocationRepo.Save(r.Context(), inv); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invocation_id": string(inv.ID())})
}

// invocationUpdateStatusToTerminalHandler is a thin admin escape hatch for
// the matching repo method; production code uses the Spawner finalize path.
type invocationUpdateStatusReq struct {
	InvocationID string `json:"invocation_id"`
}

func (s *Server) invocationUpdateStatusToTerminalHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.InvocationRepo == nil {
		writeError(w, http.StatusNotImplemented, "invocation_repo_not_wired", "")
		return
	}
	var req invocationUpdateStatusReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	inv, err := d.InvocationRepo.FindByID(r.Context(), cognition.InvocationID(req.InvocationID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if err := d.InvocationRepo.UpdateStatusToTerminal(r.Context(), inv); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invocation_id": string(inv.ID())})
}

// =============================================================================
// DecisionRepo — FindByInvocationID
// =============================================================================

func (s *Server) decisionFindByInvocationIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.DecisionRepo == nil {
		writeError(w, http.StatusNotImplemented, "decision_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("invocation_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_invocation_id", "")
		return
	}
	list, err := d.DecisionRepo.FindByInvocationID(r.Context(), cognition.InvocationID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, drec := range list {
		out[i] = decisionMap(drec)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// Projection helpers
// =============================================================================

func invocationMap(inv *cognition.SupervisorInvocation) map[string]any {
	m := map[string]any{
		"id":                   string(inv.ID()),
		"agent_instance_id":    inv.AgentInstanceID(),
		"scope_kind":           string(inv.Scope().Kind()),
		"scope_key":            inv.Scope().Key(),
		"status":               string(inv.Status()),
		"hard_timeout_seconds": inv.HardTimeoutSeconds(),
		"started_at":           inv.StartedAt().Format(time.RFC3339Nano),
		"failed_reason":        string(inv.FailedReason()),
		"failed_message":       inv.FailedMessage(),
		"decisions_made":       inv.DecisionsMade(),
		"prompt_blob_ref":      inv.PromptBlobRef(),
		"created_at":           inv.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":           inv.UpdatedAt().Format(time.RFC3339Nano),
		"version":              inv.Version(),
	}
	if ea := inv.EndedAt(); ea != nil {
		m["ended_at"] = ea.Format(time.RFC3339Nano)
	}
	if to := inv.TimedOutAt(); to != nil {
		m["timed_out_at"] = to.Format(time.RFC3339Nano)
	}
	return m
}

func decisionMap(d *cognition.DecisionRecord) map[string]any {
	return map[string]any{
		"id":               string(d.ID()),
		"invocation_id":    string(d.InvocationID()),
		"kind":             string(d.Kind()),
		"target_refs_json": d.TargetRefsJSON(),
		"rationale":        d.Rationale(),
		"outcome":          string(d.Outcome()),
		"outcome_message":  d.OutcomeMessage(),
		"created_at":       d.CreatedAt().Format(time.RFC3339Nano),
	}
}
