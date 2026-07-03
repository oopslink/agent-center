package executor

// recovery.go — F5 (crash recovery) durable orchestrator state + reconcile (§12).
//
// The orchestrator is a single point: if it crashes, its executors become
// orphans. The mitigation (design §12) is "files are the durable state" — on
// restart the orchestrator scans executors/ and rebuilds "which executors it
// manages + each one's goal/state", losing none and double-spawning none.
//
// Rebuilding needs one fact the executor-written F2 protocol does NOT carry: the
// pid, to probe whether an orphan is still alive ("进程是否活"). That is an
// ORCHESTRATOR-side concern (it spawned the process and knows the pid), so F5
// persists it in an orchestrator-PRIVATE record (orchestrator.json) alongside —
// but distinct from — the executor-written files (input/output/status/progress).
// The executor never reads or writes it.

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"time"
)

// orchestratorFileName is the orchestrator-private tracking record. It is NOT
// part of the §7 file-exchange wire contract (the executor ignores it); it is
// F5's durable handle so a restarted orchestrator can probe liveness + re-adopt.
const orchestratorFileName = "orchestrator.json"

// Record is what the orchestrator persists when it launches an executor, so a
// later (post-restart) orchestrator can reconstruct and probe it.
type Record struct {
	ExecutorID string    `json:"executor_id"`
	PID        int       `json:"pid"`
	SpawnedAt  time.Time `json:"spawned_at"`
	// BaseRef / RunnerCmd capture enough to RE-LAUNCH a crashed-retryable executor
	// without re-deriving them (the orchestrator already chose them at spawn).
	BaseRef   string   `json:"base_ref,omitempty"`
	RunnerCmd []string `json:"runner_cmd,omitempty"`
	// RepoKey / SourcePath are the durable teardown handle for a repo-materializer
	// worktree (P5). Written ONLY for worktree-backed executors; empty otherwise, so a
	// plain-dir executor's Record is byte-for-byte as before. Finalize (and a restarted
	// daemon's recovery) tears the worktree down via a WorktreeCleaner when RepoKey is
	// set — never touching the canonical source (design §10).
	RepoKey    string `json:"repo_key,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
}

// Validate enforces the fields recovery relies on.
func (r Record) Validate() error {
	if err := validateExecutorID(r.ExecutorID); err != nil {
		return err
	}
	if r.PID <= 0 {
		return errors.New("executor: record.pid required")
	}
	if r.SpawnedAt.IsZero() {
		return errors.New("executor: record.spawned_at required")
	}
	return nil
}

// LivenessProbe reports whether a pid is still a live process. Injected so
// recovery is testable without real processes.
type LivenessProbe interface {
	Alive(pid int) bool
}

// SignalLiveness is the production probe: signal 0 (the POSIX existence check).
// nil → the process exists and is signalable; EPERM → it exists but is owned by
// another user (still alive); ESRCH → gone.
type SignalLiveness struct{}

// Alive reports whether pid names a live process.
func (SignalLiveness) Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Tracker reads/writes the orchestrator-private Record under an executor dir. It
// reuses the package's atomic write + missing-file-as-ErrNotExist read helpers.
type Tracker struct {
	layout *Layout
}

// NewTracker builds a Tracker over a Layout.
func NewTracker(layout *Layout) (*Tracker, error) {
	if layout == nil {
		return nil, errors.New("executor: tracker layout required")
	}
	return &Tracker{layout: layout}, nil
}

func (t *Tracker) path(executorID string) (string, error) {
	dir, err := t.layout.Dir(executorID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, orchestratorFileName), nil
}

// Write atomically persists rec (orchestrator → durable state, at launch).
func (t *Tracker) Write(rec Record) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	path, err := t.path(rec.ExecutorID)
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, rec)
}

// Read loads the Record (missing → os.ErrNotExist, errors.Is-matchable, so a
// recovery scan can tell "we never tracked this" from "corrupt record").
func (t *Tracker) Read(executorID string) (Record, error) {
	path, err := t.path(executorID)
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := readJSON(path, &rec); err != nil {
		return Record{}, mapReadErr(orchestratorFileName, executorID, err)
	}
	if err := rec.Validate(); err != nil {
		return Record{}, fmt.Errorf("executor: %s for %s invalid: %w", orchestratorFileName, executorID, err)
	}
	return rec, nil
}

// Reconciled is one executor's rebuilt state after a crash/restart: the dir's
// snapshot, the orchestrator record (nil if never tracked), and the Completion
// the dual signal yields given the probed liveness (design §12).
type Reconciled struct {
	ExecutorID string
	Snapshot   Snapshot
	Record     *Record
	// Completion classifies the orphan: Running (re-adopt), or Succeeded/Failed/
	// Crashed (finalize). Retryable crashes carry Record (BaseRef/RunnerCmd) for
	// re-launch.
	Completion Completion
}

// Reconciler rebuilds in-flight executor state from durable files at startup.
type Reconciler struct {
	fx      *FileExchange
	tracker *Tracker
	live    LivenessProbe
}

// NewReconciler wires a Reconciler. A nil probe defaults to SignalLiveness.
func NewReconciler(fx *FileExchange, tracker *Tracker, live LivenessProbe) (*Reconciler, error) {
	if fx == nil {
		return nil, errors.New("executor: reconciler exchange required")
	}
	if tracker == nil {
		return nil, errors.New("executor: reconciler tracker required")
	}
	if live == nil {
		live = SignalLiveness{}
	}
	return &Reconciler{fx: fx, tracker: tracker, live: live}, nil
}

// Reconcile scans every executor dir and classifies each one exactly once
// (design §12: no loss, no duplication). It performs NO side effects — no spawn,
// no kill, no writeback — so it can never double-launch; the orchestrator drives
// those from the returned list (finalize terminal/crashed, re-adopt running).
func (r *Reconciler) Reconcile() ([]Reconciled, error) {
	snaps, err := r.fx.Scan()
	if err != nil {
		return nil, err
	}
	out := make([]Reconciled, 0, len(snaps))
	for _, snap := range snaps {
		rec := r.recordFor(snap.ExecutorID)
		alive := rec != nil && r.live.Alive(rec.PID)
		facts := CompletionFacts{
			ExecutorID: snap.ExecutorID,
			Exited:     false, // recovery path: we never reaped this process
			Alive:      alive,
			Output:     snap.Output,
			HasOutput:  snap.HasOutput,
			Status:     snap.Status,
		}
		out = append(out, Reconciled{
			ExecutorID: snap.ExecutorID,
			Snapshot:   snap,
			Record:     rec,
			Completion: Classify(facts),
		})
	}
	return out, nil
}

// recordFor returns the orchestrator Record for id, or nil if it was never
// tracked / is unreadable (a corrupt record must not abort the whole rebuild —
// the dir still surfaces, classified by its executor-written files alone).
func (r *Reconciler) recordFor(id string) *Record {
	rec, err := r.tracker.Read(id)
	if err != nil {
		return nil
	}
	return &rec
}
