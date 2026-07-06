package agentruntime

import (
	"encoding/json"
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
