package agentruntime

// selfheal.go — the mid-run crash-recovery STATE MACHINE (formerly
// workerdaemon/self_heal.go), moved DOWN per docs §4.0.3. Only the LOGIC
// (decide / record / due-selection) moves; the survival STORE stays owned at the
// daemon level (injected here) so it SURVIVES the managedAgent delete onExit does
// on a crash — behavior byte-identical to before. The relaunch ACTION itself
// (reap + bring-up + executor re-attach) stays in the daemon because it is
// entangled with the executor面 (Phase 0c); DrainDue hands the daemon the specs to
// relaunch.
//
// The store is guarded by the SAME shared mutex as SessionState (the reviewer
// redline): every method locks Mu, exactly as workerdaemon.AgentController.mu
// guarded c.selfHeal before.

import (
	"sync"
	"time"
)

// Self-heal defaults (overridable via SelfHealParams; manual is authoritative).
const (
	DefaultSelfHealMaxAttempts = 5
	DefaultSelfHealBackoffBase = 1 * time.Second
	DefaultSelfHealBackoffCap  = 30 * time.Second
	DefaultSelfHealResetWindow = 60 * time.Second
)

// selfHealEntry is the per-agent crash-recovery state. Encapsulated in this package;
// the daemon never touches it directly (it drives the store via methods).
type selfHealEntry struct {
	crashCount     int
	lastRelaunchAt time.Time
	nextRelaunchAt time.Time
	failed         bool
	lastCrashMsg   string
	spec           RelaunchSpec
}

// SelfHealParams tunes the backoff curve / cap / reset window / attempt cap.
type SelfHealParams struct {
	MaxAttempts int
	BackoffBase time.Duration
	BackoffCap  time.Duration
	ResetWindow time.Duration
}

func (p SelfHealParams) withDefaults() SelfHealParams {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultSelfHealMaxAttempts
	}
	if p.BackoffBase <= 0 {
		p.BackoffBase = DefaultSelfHealBackoffBase
	}
	if p.BackoffCap <= 0 {
		p.BackoffCap = DefaultSelfHealBackoffCap
	}
	if p.ResetWindow <= 0 {
		p.ResetWindow = DefaultSelfHealResetWindow
	}
	return p
}

type selfHealDecision struct {
	failed     bool
	crashCount int
	backoff    time.Duration
}

// DecideSelfHeal is the PURE crash→action policy (unit-tested for curve/cap/reset).
func DecideSelfHeal(prevCount int, lastRelaunchAt, now time.Time, p SelfHealParams) (failed bool, crashCount int, backoff time.Duration) {
	d := decideSelfHeal(prevCount, lastRelaunchAt, now, p.withDefaults())
	return d.failed, d.crashCount, d.backoff
}

func decideSelfHeal(prevCount int, lastRelaunchAt, now time.Time, p SelfHealParams) selfHealDecision {
	count := prevCount + 1
	if !lastRelaunchAt.IsZero() && now.Sub(lastRelaunchAt) >= p.ResetWindow {
		count = 1
	}
	if count > p.MaxAttempts {
		return selfHealDecision{failed: true, crashCount: count}
	}
	backoff := p.BackoffBase << (count - 1)
	if backoff <= 0 || backoff > p.BackoffCap {
		backoff = p.BackoffCap
	}
	return selfHealDecision{crashCount: count, backoff: backoff}
}

// RelaunchSpec is the payload carried across a crash so the relaunch re-drives the
// SAME agent config (self-heal gets no fresh reconcile). Attempt is the crashCount
// at the moment of relaunch (observability).
type RelaunchSpec struct {
	AgentID            string
	Version            int
	Nudge              bool
	TaskID             string
	Model              string
	DisplayName        string
	PromptDescription  string
	EnvVars            map[string]string
	ConcurrencyEnabled bool
	Attempt            int
}

// SelfHealStore is the daemon-level crash-recovery survival store. It holds the
// per-agent state machine and is guarded by the SHARED mutex (Mu). The daemon
// constructs ONE and injects it (by pointer) into every LocalRuntime plus keeps it
// for the OnTick drain.
type SelfHealStore struct {
	mu      *sync.Mutex
	params  SelfHealParams
	log     func(format string, args ...any)
	entries map[string]*selfHealEntry
}

// NewSelfHealStore builds the store over the shared mutex + tuning + logger.
func NewSelfHealStore(mu *sync.Mutex, params SelfHealParams, log func(format string, args ...any)) *SelfHealStore {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &SelfHealStore{
		mu:      mu,
		params:  params.withDefaults(),
		log:     log,
		entries: map[string]*selfHealEntry{},
	}
}

// RecordCrashAndSchedule records an UNEXPECTED crash and either schedules a backed-off
// relaunch or circuit-breaks to terminal. Returns the lifecycle state to report
// ("error" | "failed" | ""). Locks Mu (matching the old recordCrashAndSchedule).
func (s *SelfHealStore) RecordCrashAndSchedule(spec RelaunchSpec, now time.Time, msg string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordLocked(spec, now, msg, "crash")
}

// RecordRelaunchFailAndSchedule advances the state when a RELAUNCH itself fails to
// come up (FINDING-3 #117 part A) — same state machine as a crash. Locks Mu.
func (s *SelfHealStore) RecordRelaunchFailAndSchedule(spec RelaunchSpec, now time.Time, msg string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordLocked(spec, now, msg, "relaunch-fail")
}

func (s *SelfHealStore) recordLocked(spec RelaunchSpec, now time.Time, msg, kind string) string {
	e := s.entries[spec.AgentID]
	if e == nil {
		e = &selfHealEntry{}
		s.entries[spec.AgentID] = e
	}
	if e.failed {
		s.log("agent=%s self-heal: %s after terminal-failed — ignored (manual reset required). cause: %s", spec.AgentID, kind, msg)
		return ""
	}
	dec := decideSelfHeal(e.crashCount, e.lastRelaunchAt, now, s.params)
	e.crashCount = dec.crashCount
	e.lastCrashMsg = msg
	e.spec = spec
	if dec.failed {
		e.failed = true
		e.nextRelaunchAt = time.Time{}
		if kind == "crash" {
			s.log("agent=%s self-heal TERMINAL: %d consecutive crashes reached the cap — circuit-broken, NO further auto relaunch (manual reset required). last cause: %s",
				spec.AgentID, dec.crashCount, msg)
		} else {
			s.log("agent=%s self-heal TERMINAL: relaunch failed to come up %d time(s), reached the cap — circuit-broken, NO further auto relaunch (manual reset required). last cause: %s",
				spec.AgentID, dec.crashCount, msg)
		}
		return "failed"
	}
	e.nextRelaunchAt = now.Add(dec.backoff)
	if kind == "crash" {
		s.log("agent=%s self-heal: crash #%d → relaunch scheduled in %s (cause: %s)", spec.AgentID, dec.crashCount, dec.backoff, msg)
	} else {
		s.log("agent=%s self-heal: relaunch-fail #%d → retry scheduled in %s (cause: %s)", spec.AgentID, dec.crashCount, dec.backoff, msg)
	}
	return "error"
}

// DrainDue selects every due relaunch (not failed, scheduled, due), dropping any
// whose session is already live (isLive), consuming the schedule + arming the
// healthy-run reset window. isLive is called with Mu HELD (it must not re-lock).
// Returns the specs the daemon should relaunch. Locks Mu (single critical section,
// exactly like the old OnTick drain).
func (s *SelfHealStore) DrainDue(now time.Time, isLive func(agentID string) bool) []RelaunchSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	var dues []RelaunchSpec
	for id, e := range s.entries {
		if e.failed || e.nextRelaunchAt.IsZero() || e.nextRelaunchAt.After(now) {
			continue
		}
		if isLive(id) {
			e.nextRelaunchAt = time.Time{}
			continue
		}
		spec := e.spec
		spec.AgentID = id
		spec.Attempt = e.crashCount
		dues = append(dues, spec)
		e.nextRelaunchAt = time.Time{}
		e.lastRelaunchAt = now
	}
	return dues
}

// Rearm re-schedules an agent's relaunch for `at` if it still exists and is not
// terminal (the home-lock-busy retry path). Locks Mu.
func (s *SelfHealStore) Rearm(agentID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[agentID]; e != nil && !e.failed {
		e.nextRelaunchAt = at
	}
}

// Clear drops an agent's self-heal state (incl the terminal failed flag) — the
// command-driven manual/intentional path (reconcile running/stop/reset). Locks Mu.
func (s *SelfHealStore) Clear(agentID string) {
	s.mu.Lock()
	delete(s.entries, agentID)
	s.mu.Unlock()
}

// EntryForTest returns a snapshot of an agent's self-heal entry (crashCount / failed
// / present) for the daemon-level unit tests that assert the state machine. Locks Mu.
func (s *SelfHealStore) EntryForTest(agentID string) (crashCount int, failed, present bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[agentID]
	if e == nil {
		return 0, false, false
	}
	return e.crashCount, e.failed, true
}
