package api

import (
	"net/http"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// liveStateTTL bounds how fresh a worker snapshot must be (2× the idle heartbeat
// cadence, 30s) before the concurrency view is reported stale. A snapshot older than
// this — or none at all — is returned as the last-known value with stale=true rather
// than an error, so the UI can keep showing the prior view greyed out.
const liveStateTTL = 60 * time.Second

// agentConcurrencyHandler serves GET /api/orgs/{slug}/agents/{id}/concurrency
// (v2.19.0, #并发讨论2): the real-time per-agent executor view — the profile cap +
// center-derived queued depth joined with the worker's last-known live executor
// snapshot. Org-member readable; never returns credentials (the snapshot carries
// none). A missing/stale snapshot → stale=true with the last-known (or empty) set.
func (s *Server) agentConcurrencyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	agentID := agentFacingID(a)
	cap := a.Profile().EffectiveConcurrencyCap()

	// queued = the agent's PENDING tasks (open/assigned, unblocked, not yet running) —
	// the assign_flow AgentTaskLoad split. Fail-soft: a count error degrades to 0.
	queued := 0
	if d.PM != nil {
		if loads, err := d.PM.AgentTaskLoads(r.Context()); err == nil {
			queued = loads[pm.IdentityRef("agent:"+agentID)].Pending
		}
	}

	// active + executors come from the worker's last heartbeat snapshot.
	active := 0
	stale := true
	var snapshotAgeMs int64
	executors := []map[string]any{}
	if d.LiveState != nil {
		if snap, age, found := d.LiveState.Get(agentID, time.Now()); found {
			snapshotAgeMs = age.Milliseconds()
			stale = age > liveStateTTL
			active = snap.Active
			for _, e := range snap.Executors {
				em := map[string]any{
					"executor_id": e.ExecutorID,
					"task_id":     e.TaskID,
					"cli":         e.CLI,
					"model":       e.Model,
					"state":       e.State,
					"pid":         e.PID,
				}
				if !e.StartedAt.IsZero() {
					em["started_at"] = e.StartedAt.Format(time.RFC3339Nano)
				}
				executors = append(executors, em)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":        agentID,
		"cap":             cap,
		"active":          active,
		"queued":          queued,
		"stale":           stale,
		"snapshot_age_ms": snapshotAgeMs,
		"executors":       executors,
	})
}
