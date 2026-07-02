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
	// liveKey is the id the WORKER keys its concurrency snapshot by: the execution
	// entity's AR id (string(a.ID())). It travels to the daemon as the lifecycle
	// event's agent_id (agent/service/service.go → agent_control_projector) and is used
	// verbatim as the LiveState.Put key on the heartbeat. For any member-provisioned
	// agent this is DISTINCT from agentFacingID (the member id "agent-<hex>"), so the
	// read MUST key by a.ID(); keying by agentFacingID silently missed every member
	// agent's snapshot and reported "concurrency not active" while it was running
	// (issue-c44ccf6b). The response's `agent_id` field below stays agentFacingID — the
	// outward contract is unchanged; only the internal store lookup key is corrected.
	liveKey := string(a.ID())
	cap := a.Profile().EffectiveConcurrencyCap()
	// concurrencyEnabled distinguishes a genuinely single-active agent (cap 1, the
	// honest "concurrency not active" case) from one that HAS concurrency enabled but
	// simply has no fresh snapshot yet — the UI must not label the latter "not active".
	concurrencyEnabled := a.Profile().ConcurrencyEnabled()

	// PM-derived per-agent load (keyed by the member ref agent:<member-id>):
	//   queued  = Pending (open/assigned, unblocked, not yet running)
	//   running = Running (center-known in-progress) — the FALLBACK occupancy the UI
	//             shows when no live snapshot is available, so a busy agent never reads
	//             a bare "—". Fail-soft: a count error degrades both to 0.
	queued := 0
	running := 0
	if d.PM != nil {
		if loads, err := d.PM.AgentTaskLoads(r.Context()); err == nil {
			load := loads[pm.IdentityRef("agent:"+agentID)]
			queued = load.Pending
			running = load.Running
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
		if snap, age, found := d.LiveState.Get(liveKey, time.Now()); found {
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
		"agent_id":            agentID,
		"cap":                 cap,
		"active":              active,
		"queued":              queued,
		"running":             running,
		"concurrency_enabled": concurrencyEnabled,
		"stale":               stale,
		"reachable":           reachable,
		"has_snapshot":        hasSnapshot,
		"snapshot_age_ms":     snapshotAgeMs,
		"executors":           executors,
	})
}
