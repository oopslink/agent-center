package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// pending_judgment.go — the durable per-agent set of executor results awaiting the
// supervisor's JUDGED completion (option b, issue-68ccb310). When a forked executor
// finishes, the writeback injects a judgment prompt into the supervisor session and
// records a pendingJudgment here; the supervisor reviews REAL delivery and calls
// complete_task/block_task itself. The low-frequency Tick reconcile (reconcile_
// pending_judgments) then guarantees no dropped judgment strands a task:
//   - task no longer running (supervisor judged) → drop
//   - still running past a grace window       → re-inject (nudge)
//   - too many nudges with no judgment         → escalate to a human (post_message)
//
// STRICT rule: this layer NEVER writes task status itself (that would re-introduce
// the auto-writeback "binding"). It only drives the supervisor (nudge) or escalates.
//
// Durability is the crux: the store is a JSON file in the agent home, so a runtime
// restart re-loads the pending set (via newPendingStore) and the reconcile keeps
// re-driving — a crash between "executor finished" and "supervisor judged" cannot
// silently lose the task.

// pendingJudgment is one executor result awaiting the supervisor's judged completion.
type pendingJudgment struct {
	TaskRef    string    `json:"task_ref"`
	Prompt     string    `json:"prompt"`      // re-injected verbatim on a nudge
	InjectedAt time.Time `json:"injected_at"` // when the first judgment was delivered
	NudgeCount int       `json:"nudge_count"` // reconcile re-injections so far
	// Escalated marks that the reconcile gave the supervisor a FINAL "resolve or
	// block_task(input_required)" nudge after exhausting the nudge budget. The entry is
	// KEPT (not dropped) so a relaunched supervisor still re-judges from the durable set
	// on boot, but no further nudges are sent — the strict fallback surfaces to a human
	// via that final nudge + a WARN log, and NEVER writes task status from Go.
	Escalated bool `json:"escalated"`
}

// pendingStore is the durable, mutex-guarded per-agent pending-judgment set, keyed
// by task ref. All mutations persist to disk immediately so the state survives a
// crash/restart.
type pendingStore struct {
	mu   sync.Mutex
	path string
	m    map[string]pendingJudgment
}

// newPendingStore loads the store from path (tolerating a missing/corrupt file —
// a fresh agent, or a partial write, starts empty rather than failing boot).
func newPendingStore(path string) *pendingStore {
	return &pendingStore{path: path, m: loadPendingFile(path)}
}

// record adds (or refreshes) a pending judgment for taskRef and persists. Called by
// the writeback's inject seam when a judgment is delivered. A re-record for the same
// task (e.g. a re-run) resets the clock + nudge count.
func (s *pendingStore) record(taskRef, prompt string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[taskRef] = pendingJudgment{TaskRef: taskRef, Prompt: prompt, InjectedAt: now, NudgeCount: 0}
	s.persistLocked()
}

// drop removes a pending judgment (the supervisor judged it, or it was escalated)
// and persists. Idempotent.
func (s *pendingStore) drop(taskRef string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[taskRef]; !ok {
		return
	}
	delete(s.m, taskRef)
	s.persistLocked()
}

// bumpNudge increments the nudge count for taskRef and persists, returning the new
// count. Called after the reconcile re-injects a nudge.
func (s *pendingStore) bumpNudge(taskRef string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[taskRef]
	if !ok {
		return 0
	}
	p.NudgeCount++
	s.m[taskRef] = p
	s.persistLocked()
	return p.NudgeCount
}

// markEscalated flags a pending judgment as escalated (final nudge sent) and
// persists — the entry is kept for boot-recovery but no longer nudged.
func (s *pendingStore) markEscalated(taskRef string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[taskRef]
	if !ok {
		return
	}
	p.Escalated = true
	s.m[taskRef] = p
	s.persistLocked()
}

// snapshot returns a stable-ordered copy of the pending set for the reconcile to
// iterate without holding the lock across center calls.
func (s *pendingStore) snapshot() []pendingJudgment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]pendingJudgment, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskRef < out[j].TaskRef })
	return out
}

// len reports the pending count (test/introspection helper).
func (s *pendingStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// persistLocked writes the map to disk atomically (temp file + rename). Best-effort:
// a persist error is not fatal (the in-memory set still drives this process; the next
// mutation retries). Caller holds s.mu.
func (s *pendingStore) persistLocked() {
	if s.path == "" {
		return
	}
	b, err := json.Marshal(s.m)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// loadPendingFile reads a pending-store file, returning an empty map on any error
// (missing file, partial write) so a fresh/recovered agent starts clean.
func loadPendingFile(path string) map[string]pendingJudgment {
	m := map[string]pendingJudgment{}
	if path == "" {
		return m
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]pendingJudgment{}
	}
	return m
}

// reconcile tuning (option b). Deliberately low-frequency + generous grace: the
// supervisor is usually just busy, not dropped, so give it room before nudging.
const (
	pendingReconcileEvery = 30 * time.Second // rate-limit the reconcile sweep
	pendingNudgeInterval  = 2 * time.Minute  // min gap before each (re-)nudge
	pendingMaxNudges      = 3                // final escalation after this many nudges
)

// reconcilePendingJudgments is the option-b heartbeat backstop (issue-68ccb310), run
// from Tick (rate-limited). For each pending judgment it cross-checks the center's
// in-flight task set:
//   - task no longer in the RUNNING set (completed, or blocked so it left running) →
//     the supervisor judged it → drop.
//   - still running, grace elapsed, under budget → re-inject the judgment (nudge).
//   - budget exhausted → ONE final "resolve or block_task(input_required)" nudge + a
//     WARN log, then mark escalated (kept for boot-recovery, no more nudges).
//
// STRICT (issue-68ccb310): this NEVER writes task status from Go — it only DRIVES the
// supervisor (nudge) or surfaces to a human (escalation nudge + log). A center read
// error is transient: skip this sweep (never assume terminal on a failed read).
func (r *LocalRuntime) reconcilePendingJudgments(ctx context.Context, now time.Time) {
	if r.pending == nil || r.pending.len() == 0 {
		return
	}
	if now.Before(r.nextPendingReconcileAt) {
		return
	}
	r.nextPendingReconcileAt = now.Add(pendingReconcileEvery)

	lister := NewInflightTaskLister(r.toolCaller())
	if lister == nil {
		return
	}
	tasks, err := lister.ListMyInflightTasks(ctx, r.cfg.AgentID)
	if err != nil {
		r.log("agent=%s pending-reconcile list_my_inflight_tasks: %v", r.cfg.AgentID, err)
		return
	}
	running := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if t.Status == "running" {
			running[t.TaskID] = true
		}
	}

	for _, p := range r.pending.snapshot() {
		if !running[p.TaskRef] {
			r.pending.drop(p.TaskRef) // supervisor judged it (completed / blocked)
			continue
		}
		if p.Escalated {
			continue // already escalated — kept for boot-recovery, no more nudges
		}
		if now.Sub(p.InjectedAt) < time.Duration(p.NudgeCount+1)*pendingNudgeInterval {
			continue // give the supervisor time before the next (re-)nudge
		}
		if p.NudgeCount >= pendingMaxNudges {
			r.log("WARN agent=%s task=%s: executor finished but supervisor has not judged after %d nudges — escalating", r.cfg.AgentID, p.TaskRef, p.NudgeCount)
			_ = r.injectSession(ctx, escalationPrompt(p.TaskRef)) // best-effort; never writes status
			r.pending.markEscalated(p.TaskRef)
			continue
		}
		if err := r.injectSession(ctx, p.Prompt); err != nil {
			// Session down (mid-restart/crash) — self-heal relaunches it; retry next Tick.
			r.log("agent=%s task=%s pending-nudge inject: %v", r.cfg.AgentID, p.TaskRef, err)
			continue
		}
		r.pending.bumpNudge(p.TaskRef)
	}
}

// escalationPrompt is the FINAL nudge after the nudge budget is exhausted: it tells
// the supervisor to resolve the task or surface it to a human via
// block_task(input_required). Go never writes the status itself.
func escalationPrompt(taskRef string) string {
	return fmt.Sprintf(
		"[reminder] You still have NOT judged the finished executor for task %s after several nudges. "+
			"Resolve it NOW: call complete_task(task_id=%q) if it delivered, or "+
			"block_task(task_id=%q, reason=\"executor finished but delivery could not be judged — needs attention\", "+
			"reason_type=\"input_required\") to surface it to a human. Do not leave it pending.",
		taskRef, taskRef, taskRef,
	)
}
