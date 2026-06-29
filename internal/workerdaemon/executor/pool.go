package executor

// pool.go — F1 (process model) concurrency gate (design §3 / §4 / §10).
//
// A Pool is the per-agent "≤ N concurrent executors" primitive: it admits up to
// max_concurrent_tasks executor launches, each fully isolated (own directory +
// own git worktree + own process group), and refuses the (N+1)th with
// ErrAtCapacity so the orchestrator QUEUES it rather than hard-starting (design
// §3 "超额排队，不硬起"). The Pool owns only the admit/provision/spawn/track
// mechanism; the orchestrator's wake-loop, queue draining, watchdog and center
// writeback live in F5 and DRIVE this Pool.
//
// Isolation per launch (the F1 acceptance surface):
//   - a fresh <agent_root>/executors/<id>/ directory (FileExchange.Provision);
//   - a dedicated git worktree on its own branch (WorktreeProvisioner.AddNewBranch)
//     so two concurrent executors never share a checkout;
//   - a forked process in its own process group with an mcp-free, credential-free
//     environment (Spawner.Spawn + BuildExecutorEnv).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/oopslink/agent-center/internal/clock"
)

// ErrAtCapacity is returned by Launch when the Pool already has max executors
// active. The orchestrator treats it as "queue and retry on the next Release",
// NOT as a failure of the work itself (design §3).
var ErrAtCapacity = errors.New("executor: pool at capacity")

// ErrAlreadyActive is returned when an executor id is launched while an instance
// with the same id is still tracked (a double-launch bug or a stale id reuse).
var ErrAlreadyActive = errors.New("executor: id already active")

// DefaultMaxConcurrent is the profile default for max_concurrent_tasks (design §10).
const DefaultMaxConcurrent = 3

// PoolConfig configures a Pool. The Pool is per-agent: FileExchange anchors at the
// agent home; WorktreeProvisioner (optional) roots at the source repo the executors
// branch off.
type PoolConfig struct {
	// Exchange is the F2 file protocol over <agent_root>/executors/ (required).
	Exchange *FileExchange
	// Worktrees provisions each executor's isolated git worktree. OPTIONAL (W1, PD
	// ruling): production agents do not necessarily edit a git repo — their workspace
	// is just an isolated directory (process-group + env + path containment already
	// isolate them). A git worktree is provisioned ONLY when both Worktrees AND BaseRef
	// are set (the "this executor edits a source repo" case). Worktrees and BaseRef must
	// be set together or both empty; setting one without the other is a config error.
	Worktrees *WorktreeProvisioner
	// Spawner forks executor processes. Nil → NewSpawner().
	Spawner *Spawner
	// AgentRoot is the per-agent home (the FileExchange Layout root). Used to point
	// the forked executor at <agent_root>/executors/<id>/ (required).
	AgentRoot string
	// BaseRef is the git ref each executor's worktree branches from. OPTIONAL — set
	// together with Worktrees to enable git-worktree workspaces; empty ⇒ the workspace
	// is a plain isolated directory (no git worktree). See Worktrees.
	BaseRef string
	// BinaryPath is the agent-center executable carrying `worker executor`. Empty →
	// os.Executable() at spawn time.
	BinaryPath string
	// AgentEnv is the ② per-agent overlay for executor env (git identity / profile).
	AgentEnv map[string]string
	// Tracker, when set, persists an orchestrator-private Record (pid + base_ref +
	// runner_cmd) under each launched executor's dir, so a RESTARTED orchestrator
	// can probe the orphan's liveness and re-adopt it (design §12 crash recovery).
	// OPTIONAL: nil ⇒ no durable record is written (the executor is then invisible to
	// post-restart recovery). Production wiring (W3) always sets it.
	Tracker *Tracker
	// Max is max_concurrent_tasks. <= 0 → DefaultMaxConcurrent.
	Max int
	// Clock is injected for deterministic tests (worktree branch naming uses ids,
	// not time, so this is currently only a forward-looking seam). Nil → SystemClock.
	Clock clock.Clock
}

// LaunchSpec is one executor launch request. Input is the fully-resolved F2 Input
// (the orchestrator has already chosen the model per design §5 before calling).
type LaunchSpec struct {
	// Input is written to input.json; Input.ExecutorID identifies the launch.
	Input Input
	// RunnerCmd is the pure-compute command the executor runs in its workspace
	// (the model-routed agent CLI; F3 supplies it). Empty is allowed at the
	// process-model layer — the executor entrypoint reports a clear error.
	RunnerCmd []string
}

// Pool tracks an agent's live executors under a concurrency cap.
type Pool struct {
	cfg     PoolConfig
	spawner *Spawner
	clk     clock.Clock
	max     int

	mu     sync.Mutex
	active map[string]*Handle // value nil == reserved slot mid-launch
}

// NewPool validates cfg and builds a Pool.
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.Exchange == nil {
		return nil, errors.New("executor: pool exchange required")
	}
	if cfg.AgentRoot == "" {
		return nil, errors.New("executor: pool agent_root required")
	}
	// Worktree mode is opt-in and all-or-nothing: Worktrees + BaseRef must be set
	// together (git-worktree workspace) or both empty (plain isolated dir). A half
	// -configured pool (one without the other) is a programming error, surfaced now.
	hasWT := cfg.Worktrees != nil
	hasBase := strings.TrimSpace(cfg.BaseRef) != ""
	if hasWT != hasBase {
		return nil, errors.New("executor: pool worktrees and base_ref must be set together (or both empty for a plain-dir workspace)")
	}
	max := cfg.Max
	if max <= 0 {
		max = DefaultMaxConcurrent
	}
	sp := cfg.Spawner
	if sp == nil {
		sp = NewSpawner()
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Pool{
		cfg:     cfg,
		spawner: sp,
		clk:     clk,
		max:     max,
		active:  make(map[string]*Handle),
	}, nil
}

// Max returns the concurrency cap.
func (p *Pool) Max() int { return p.max }

// Active returns the number of executors currently occupying a slot (running +
// mid-launch reservations).
func (p *Pool) Active() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.active)
}

// Available returns how many more executors may be launched right now.
func (p *Pool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max - len(p.active)
}

// Handles returns the live handles for currently-spawned executors (excludes
// mid-launch reservations). Order is unspecified.
func (p *Pool) Handles() []*Handle {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Handle, 0, len(p.active))
	for _, h := range p.active {
		if h != nil {
			out = append(out, h)
		}
	}
	return out
}

// Launch admits one executor if a slot is free, then provisions its directory +
// worktree, writes input.json, and forks the process. Returns ErrAtCapacity when
// full (orchestrator queues) or ErrAlreadyActive on a duplicate id. The capacity
// reservation is taken under lock and the slow I/O (git worktree add, spawn) runs
// OUTSIDE the lock, so concurrent launches into distinct free slots do not
// serialize on git — while the cap is still enforced atomically.
func (p *Pool) Launch(ctx context.Context, spec LaunchSpec) (*Handle, error) {
	id := spec.Input.ExecutorID
	if err := spec.Input.Validate(); err != nil {
		return nil, err
	}

	// Reserve a slot (enforces the cap atomically).
	p.mu.Lock()
	if _, dup := p.active[id]; dup {
		p.mu.Unlock()
		return nil, ErrAlreadyActive
	}
	if len(p.active) >= p.max {
		p.mu.Unlock()
		return nil, ErrAtCapacity
	}
	p.active[id] = nil // reservation; counts toward the cap until finalized
	p.mu.Unlock()

	h, err := p.provisionAndSpawn(ctx, spec)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		delete(p.active, id) // free the reserved slot on failure
		return nil, err
	}
	p.active[id] = h
	return h, nil
}

// provisionAndSpawn does the isolated, lock-free heavy lifting: per-executor dir,
// git worktree, input.json, then the process-group fork.
func (p *Pool) provisionAndSpawn(ctx context.Context, spec LaunchSpec) (*Handle, error) {
	id := spec.Input.ExecutorID
	if _, err := p.cfg.Exchange.Provision(id); err != nil {
		return nil, fmt.Errorf("executor: pool provision %s: %w", id, err)
	}
	wsPath, err := p.cfg.Exchange.Layout().WorkspaceDir(id)
	if err != nil {
		return nil, err
	}
	// Provision the workspace: a git worktree when configured (executor edits a source
	// repo), else a plain isolated directory (PD ruling, W1). Either way the executor's
	// file edits are confined to wsPath by the F2 containment guard + its process group.
	if p.cfg.Worktrees != nil && strings.TrimSpace(p.cfg.BaseRef) != "" {
		branch := executorBranch(id)
		if err := p.cfg.Worktrees.AddNewBranch(ctx, wsPath, branch, p.cfg.BaseRef); err != nil {
			return nil, fmt.Errorf("executor: pool worktree %s: %w", id, err)
		}
	} else if err := os.MkdirAll(wsPath, 0o700); err != nil {
		return nil, fmt.Errorf("executor: pool workspace dir %s: %w", id, err)
	}
	if err := p.cfg.Exchange.WriteInput(spec.Input); err != nil {
		return nil, fmt.Errorf("executor: pool write input %s: %w", id, err)
	}
	h, err := p.spawner.Spawn(SpawnSpec{
		BinaryPath: p.cfg.BinaryPath,
		ExecutorID: id,
		AgentRoot:  p.cfg.AgentRoot,
		RunnerCmd:  spec.RunnerCmd,
		AgentEnv:   p.cfg.AgentEnv,
	})
	if err != nil {
		return nil, err
	}
	h.startedAt = p.clk.Now() // v2.19.0: stamp spawn time for the concurrency snapshot
	// Persist the orchestrator-private recovery Record AFTER the fork (we now know the
	// pid) but BEFORE returning the handle, so a crash in the next instant still leaves
	// a probe-able record (design §12: files are the durable state). If the record
	// cannot be written the executor would be an unrecoverable orphan on restart — kill
	// it now rather than leak an untracked process, and fail the launch (work re-queues).
	if p.cfg.Tracker != nil {
		rec := Record{
			ExecutorID: id,
			PID:        h.PID,
			SpawnedAt:  p.clk.Now(),
			BaseRef:    p.cfg.BaseRef,
			RunnerCmd:  spec.RunnerCmd,
		}
		if werr := p.cfg.Tracker.Write(rec); werr != nil {
			_ = h.Kill()
			return nil, fmt.Errorf("executor: pool track %s: %w", id, werr)
		}
	}
	return h, nil
}

// Adopt reserves a slot for an executor that is ALREADY running but is not
// tracked by this Pool — the crash-recovery case (design §12): after an
// orchestrator restart the new Pool is empty, yet reparented executors are still
// alive and must count toward max_concurrent_tasks again. Adopt takes the slot
// WITHOUT spawning (the process exists), tracking it as a handle-less reservation
// (Handles() skips it; Release frees it). Returns ErrAlreadyActive if the id is
// already tracked, or ErrAtCapacity if no slot is free.
func (p *Pool) Adopt(executorID string) error {
	if err := validateExecutorID(executorID); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.active[executorID]; dup {
		return ErrAlreadyActive
	}
	if len(p.active) >= p.max {
		return ErrAtCapacity
	}
	p.active[executorID] = nil // handle-less reservation: alive but not reapable here
	return nil
}

// Release frees the slot held by an executor (called after the orchestrator has
// harvested output.json and torn down the worktree — that teardown is F5's job).
// Returns whether a slot was actually held. Idempotent.
func (p *Pool) Release(executorID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.active[executorID]; !ok {
		return false
	}
	delete(p.active, executorID)
	return true
}

// executorBranch is the per-executor worktree branch name: a stable, collision
// -free branch derived from the (already-validated) executor id so two executors
// never share a branch (design §6.D end-to-end isolation).
func executorBranch(executorID string) string {
	return "executor/" + executorID
}
