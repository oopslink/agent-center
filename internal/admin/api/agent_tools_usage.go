package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/usage"
)

// report_usage (v2.15.0 I28/F2, issue-a7ff560e) — the per-turn usage ingest.
//
// WHO CALLS IT. This is a WORKER-initiated agent-tool, not an LLM-facing one: the
// worker's turn-complete hook (agent_controller.onEvent) observes the per-turn
// token counts off the claude `result` line and POSTs them here. The model never
// calls report_usage (it has no view of its own token counts), so it is
// deliberately NOT registered in the agent-facing MCP set. Auth still rides the
// standard worker-bearer + agent-binding gate (requireAgentOnWorker).
//
// WHAT IT DOES. Materializes cost at the turn's price (decision 2: list-price
// estimate) and appends a usage_events row with source='report' (decision 1 main
// path). Best-effort by design: a malformed/over-budget report must never break
// the agent loop, so the worker treats any non-2xx as a logged warning.

// reportUsageReq is the body for POST /admin/agent-tools/report_usage. The
// payload carries token counts + model + optional task_id; project_id is NOT
// trusted from the wire — the center derives it from the task (see handler).
type reportUsageReq struct {
	AgentID          string `json:"agent_id"`
	Model            string `json:"model"`
	TaskID           string `json:"task_id,omitempty"` // "" = converse / non-task turn
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	TS               string `json:"ts,omitempty"` // RFC3339 turn time; default = server now
}

func (s *Server) reportUsageHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req reportUsageReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.UsageEventRepo == nil || d.ModelPriceRepo == nil {
		writeError(w, http.StatusNotImplemented, "usage_not_wired", "")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "")
		return
	}

	// Event time drives point-in-time pricing (effective_from <= ts). Default to
	// now when the worker omits it (the report arrives right after the turn).
	ts := time.Now().UTC()
	if raw := strings.TrimSpace(req.TS); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_ts", err.Error())
			return
		}
		ts = parsed.UTC()
	}

	// project_id is derived from the task (authoritative), never trusted from the
	// payload. A task-less (converse) turn has no project → "" sentinel: the
	// agent's own interaction-usage bucket. The column is NOT NULL — we always
	// store "" or a real id, NEVER NULL (keep the sentinel consistent). A task
	// lookup miss (deleted / cross-project / PM unwired) must not drop the usage
	// record — fall back to "" and still account the tokens.
	//
	// task_id fallback (issue-af03da2f / I54): the worker's per-turn hook reports an
	// empty task_id whenever it can't see the agent's current task — the production
	// reality, since agents self-manage their queue via MCP start_task and the daemon
	// never learns the task_id (and a converse turn explicitly clears it). That left
	// EVERY usage_event task-less → the Top Cost Tasks panel was permanently empty.
	// The center is the running-task authority, so when the report carries no task_id
	// we attribute it to the agent's SOLE running task (PMService.SoleRunningTask).
	// The "exactly one" guard keeps converse/idle turns (0 running) and concurrency
	// agents (>1 running, ambiguous) unattributed — they stay "" (non-task overhead),
	// matching the design's "don't force a task_id onto a non-task turn" rule.
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" && d.PMService != nil {
		if t, err := d.PMService.SoleRunningTask(r.Context(), pm.IdentityRef(agentActor(a))); err == nil && t != nil {
			taskID = string(t.ID())
		}
	}
	projectID := ""
	if taskID != "" && d.PMService != nil {
		if t, err := d.PMService.GetTask(r.Context(), pm.TaskID(taskID)); err == nil {
			projectID = string(t.ProjectID())
		}
	}

	tokens := usage.TokenCounts{
		Input:      req.InputTokens,
		Output:     req.OutputTokens,
		CacheRead:  req.CacheReadTokens,
		CacheWrite: req.CacheWriteTokens,
	}

	// Materialize cost at the turn's price. Unknown model (no price in force) →
	// cost_micros = 0 and the tokens still land; when that model is first priced
	// later, a by-model recompute (decision 4 backfill) fills the 0-cost rows.
	// Tokens are never dropped over a missing price.
	var costMicros int64
	pb, err := d.ModelPriceRepo.LoadPriceBook(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	switch c, perr := pb.CostMicrosAt(req.Model, ts, tokens); {
	case perr == nil:
		costMicros = c
	case errors.Is(perr, usage.ErrNoPrice):
		costMicros = 0 // unknown model — record tokens, recompute cost on first pricing
	default:
		mapDomainError(w, perr)
		return
	}

	ev := usage.UsageEvent{
		ID:         idgen.MustNewULID(),
		AgentRef:   agentActor(a),
		ProjectID:  projectID,
		TaskID:     taskID,
		Model:      req.Model,
		Tokens:     tokens,
		CostMicros: costMicros,
		TS:         ts,
		Source:     usage.SourceReport,
	}
	if err := ev.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_usage_event", err.Error())
		return
	}
	if err := d.UsageEventRepo.Append(r.Context(), ev); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"event_id":    ev.ID,
		"cost_micros": costMicros,
		"project_id":  projectID,
	})
}
