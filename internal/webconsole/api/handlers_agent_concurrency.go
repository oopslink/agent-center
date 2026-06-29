package api

import (
	"net/http"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
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
	//
	// Three-state freshness (T606, issue-af03da2f): a single `stale` bool conflated
	// three very different situations and the UI mislabeled all of them "worker
	// unreachable". We now also surface:
	//   - reachable    — is the bound worker ONLINE? (false = worker truly offline)
	//   - has_snapshot — has this agent EVER reported a live snapshot?
	// so the UI can tell apart (a) worker offline, (b) a snapshot that aged past the
	// TTL, and (c) an agent that never reported one (concurrency not active on the
	// worker — the common case for a non-concurrent agent). `stale` is retained as the
	// coarse "live view not usable" flag (true when no fresh snapshot) for back-compat.
	active := 0
	stale := true
	hasSnapshot := false
	var snapshotAgeMs int64
	executors := []map[string]any{}
	if d.LiveState != nil {
		if snap, age, found := d.LiveState.Get(agentID, time.Now()); found {
			hasSnapshot = true
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

	// reachable: is the bound worker ONLINE? Default true — only a worker we can look
	// up AND find OFFLINE flips it to false, so a missing worker record never
	// fabricates a misleading "offline" state.
	reachable := true
	if wid := a.WorkerID(); wid != "" && d.WorkerRepo != nil {
		if wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(wid)); err == nil && wk != nil {
			reachable = wk.Status() == workforce.WorkerOnline
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":        agentID,
		"cap":             cap,
		"active":          active,
		"queued":          queued,
		"stale":           stale,
		"reachable":       reachable,
		"has_snapshot":    hasSnapshot,
		"snapshot_age_ms": snapshotAgeMs,
		"executors":       executors,
	})
}
