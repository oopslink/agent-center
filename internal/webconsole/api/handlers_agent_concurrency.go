package api

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
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
	admissionCap := cap
	slotCount := cap
	configVersion := 0
	slotStable := false
	integrity := ""
	integrityError := ""
	executors := []map[string]any{}
	slots := []map[string]any{}
	if d.LiveState != nil {
		if snap, age, found := d.LiveState.Get(liveKey, time.Now()); found {
			hasSnapshot = true
			snapshotAgeMs = age.Milliseconds()
			stale = age > liveStateTTL
			active = snap.Active
			configVersion = snap.ConfigVersion
			if snap.AdmissionCap > 0 {
				admissionCap = snap.AdmissionCap
			}
			if snap.SlotCount > 0 {
				slotCount = snap.SlotCount
			}
			integrity = snap.Integrity
			integrityError = snap.IntegrityError
			if slotCount <= 0 && len(snap.Slots) > 0 {
				slotCount = inferredSlotCount(snap.Slots)
			}
			execList := sortedExecutorSnapshots(snap.Executors)
			for _, e := range execList {
				executors = append(executors, executorSnapshotMap(e))
			}
			requireExecutorSlots := snap.SlotCount > 0 || len(snap.Slots) > 0
			if integrity == "" {
				if err := validateExecutorSlots(execList, slotCount, requireExecutorSlots); err != "" {
					integrity = "degraded"
					integrityError = err
				}
				if active != len(execList) {
					integrity = "degraded"
					if integrityError == "" {
						integrityError = fmt.Sprintf("active=%d but executors=%d", active, len(execList))
					}
				}
			}
			slotList := sortedSlotSnapshots(snap.Slots)
			if len(slotList) == 0 && integrity == "" && snapshotSlotStable(execList, slotCount) {
				slotList = deriveSlotSnapshots(execList, slotCount, admissionCap)
			}
			if len(slotList) > 0 {
				if err := validateFullSlots(slotList, slotCount); err != "" {
					integrity = "degraded"
					if integrityError == "" {
						integrityError = err
					}
					slotList = nil
				}
			}
			slotStable = integrity == "" && len(slotList) == slotCount && slotCount > 0
			for _, sl := range slotList {
				slots = append(slots, slotSnapshotMap(sl, !stale))
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
		"configured_cap":      cap,
		"admission_cap":       admissionCap,
		"slot_count":          slotCount,
		"config_version":      configVersion,
		"slot_stable":         slotStable,
		"integrity":           integrity,
		"integrity_error":     integrityError,
		"active":              active,
		"queued":              queued,
		"running":             running,
		"concurrency_enabled": concurrencyEnabled,
		"stale":               stale,
		"reachable":           reachable,
		"has_snapshot":        hasSnapshot,
		"snapshot_age_ms":     snapshotAgeMs,
		"executors":           executors,
		"slots":               slots,
	})
}

func sortedExecutorSnapshots(in []concurrency.ExecutorSnapshot) []concurrency.ExecutorSnapshot {
	out := append([]concurrency.ExecutorSnapshot(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.SlotIndex != nil && b.SlotIndex != nil && *a.SlotIndex != *b.SlotIndex:
			return *a.SlotIndex < *b.SlotIndex
		case a.SlotIndex != nil && b.SlotIndex == nil:
			return true
		case a.SlotIndex == nil && b.SlotIndex != nil:
			return false
		default:
			return a.ExecutorID < b.ExecutorID
		}
	})
	return out
}

func executorSnapshotMap(e concurrency.ExecutorSnapshot) map[string]any {
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
	if e.LastProgressAt != nil && !e.LastProgressAt.IsZero() {
		em["last_progress_at"] = e.LastProgressAt.Format(time.RFC3339Nano)
	}
	if e.CurrentActivity != "" {
		em["current_activity"] = e.CurrentActivity
	}
	if e.SlotIndex != nil {
		em["slot_index"] = *e.SlotIndex
	}
	return em
}

func sortedSlotSnapshots(in []concurrency.SlotSnapshot) []concurrency.SlotSnapshot {
	out := append([]concurrency.SlotSnapshot(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SlotIndex != out[j].SlotIndex {
			return out[i].SlotIndex < out[j].SlotIndex
		}
		return out[i].ExecutorID < out[j].ExecutorID
	})
	return out
}

func inferredSlotCount(slots []concurrency.SlotSnapshot) int {
	max := 0
	for _, sl := range slots {
		if sl.SlotIndex >= max {
			max = sl.SlotIndex + 1
		}
	}
	return max
}

func deriveSlotSnapshots(execs []concurrency.ExecutorSnapshot, slotCount, admissionCap int) []concurrency.SlotSnapshot {
	if slotCount <= 0 {
		return nil
	}
	bySlot := make(map[int]concurrency.ExecutorSnapshot, len(execs))
	for _, e := range execs {
		if e.SlotIndex != nil {
			bySlot[*e.SlotIndex] = e
		}
	}
	out := make([]concurrency.SlotSnapshot, 0, slotCount)
	for i := 0; i < slotCount; i++ {
		if e, ok := bySlot[i]; ok {
			out = append(out, slotFromExecutorSnapshot(i, e))
			continue
		}
		state := concurrency.StateIdle
		if i >= admissionCap {
			state = concurrency.StateDraining
		}
		out = append(out, concurrency.SlotSnapshot{SlotIndex: i, State: state})
	}
	return out
}

func slotFromExecutorSnapshot(slot int, e concurrency.ExecutorSnapshot) concurrency.SlotSnapshot {
	return concurrency.SlotSnapshot{
		SlotIndex:       slot,
		ExecutorID:      e.ExecutorID,
		TaskID:          e.TaskID,
		CLI:             e.CLI,
		Model:           e.Model,
		State:           e.State,
		StartedAt:       e.StartedAt,
		PID:             e.PID,
		LastProgressAt:  e.LastProgressAt,
		CurrentActivity: e.CurrentActivity,
	}
}

func slotSnapshotMap(sl concurrency.SlotSnapshot, fresh bool) map[string]any {
	state := sl.State
	if state == "" {
		state = concurrency.StateUnknown
	}
	if !fresh && sl.ExecutorID == "" {
		state = concurrency.StateUnknown
	}
	sm := map[string]any{
		"slot_index": sl.SlotIndex,
		"state":      state,
	}
	if sl.ExecutorID != "" {
		sm["executor_id"] = sl.ExecutorID
	}
	if sl.TaskID != "" {
		sm["task_id"] = sl.TaskID
	}
	if sl.CLI != "" {
		sm["cli"] = sl.CLI
	}
	if sl.Model != "" {
		sm["model"] = sl.Model
	}
	if sl.PID != 0 {
		sm["pid"] = sl.PID
	}
	if !sl.StartedAt.IsZero() {
		sm["started_at"] = sl.StartedAt.Format(time.RFC3339Nano)
	}
	if sl.LastProgressAt != nil && !sl.LastProgressAt.IsZero() {
		sm["last_progress_at"] = sl.LastProgressAt.Format(time.RFC3339Nano)
	}
	if sl.CurrentActivity != "" {
		sm["current_activity"] = sl.CurrentActivity
	}
	return sm
}

func validateExecutorSlots(execs []concurrency.ExecutorSnapshot, slotCount int, requireAll bool) string {
	if slotCount <= 0 {
		return ""
	}
	seen := map[int]string{}
	for _, e := range execs {
		if e.SlotIndex == nil {
			if requireAll {
				return fmt.Sprintf("executor %s missing slot_index", e.ExecutorID)
			}
			continue
		}
		slot := *e.SlotIndex
		if slot < 0 || slot >= slotCount {
			return fmt.Sprintf("executor %s slot_index %d out of range [0,%d)", e.ExecutorID, slot, slotCount)
		}
		if other := seen[slot]; other != "" && other != e.ExecutorID {
			return fmt.Sprintf("duplicate slot_index %d for %s and %s", slot, other, e.ExecutorID)
		}
		seen[slot] = e.ExecutorID
	}
	return ""
}

func validateFullSlots(slots []concurrency.SlotSnapshot, slotCount int) string {
	if slotCount <= 0 {
		return ""
	}
	if len(slots) != slotCount {
		return fmt.Sprintf("slots len=%d but slot_count=%d", len(slots), slotCount)
	}
	for i, sl := range slots {
		if sl.SlotIndex != i {
			return fmt.Sprintf("slot index sequence mismatch at %d: got %d", i, sl.SlotIndex)
		}
	}
	return ""
}

func snapshotSlotStable(execs []concurrency.ExecutorSnapshot, slotCount int) bool {
	if slotCount <= 0 {
		return false
	}
	seen := map[int]struct{}{}
	for _, e := range execs {
		if e.SlotIndex == nil {
			return false
		}
		slot := *e.SlotIndex
		if slot < 0 || slot >= slotCount {
			return false
		}
		if _, dup := seen[slot]; dup {
			return false
		}
		seen[slot] = struct{}{}
	}
	return true
}
