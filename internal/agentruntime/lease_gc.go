package agentruntime

// lease_gc.go — T860 piece ③: the agent-runtime process's OWN execution-lease renewal
// + task-dir GC, moved off the daemon's AgentController.OnTick (which the controller
// model no longer runs). Self-contained per-agent, k8s-aligned: the agent process,
// which KNOWS it is alive and which tasks it runs, keeps its leases fresh itself.
//
// CRITICAL over the old daemon renewer: it renews EVERY in-flight task this agent runs
// — the supervisor's current task AND each live executor's task — not just the session's
// current task. A concurrency-enabled agent runs N tasks at once; the old renewer only
// covered one, so its executors' long tasks could lapse. This version fixes that.

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
)

// ErrLeaseRevoked is the sentinel the Reporter's RenewTaskLease returns when the center
// declined the renew because THIS agent's execution should STOP — the task was blocked
// mid-flight (ADR-0046: a block keeps status=running, so a running executor would
// otherwise keep going — the P67 Ship 事故 where dead code got merged), reassigned away,
// or reached a terminal state. drainLeaseRenewals matches it with errors.Is to
// circuit-break (熔断) the in-flight executor. It lives HERE (not in workerdaemon, the
// adminclient's package) because the import arrow is workerdaemon → agentruntime — the
// checker and the sentinel must share a package the transport can import without a cycle.
var ErrLeaseRevoked = errors.New("agentruntime: execution lease revoked")

// DefaultLeaseRenewEvery / DefaultGCInterval are the cadences. 60s is far below the
// server's execution-lease TTL (hours); GC hourly. Both are overridable via env for the
// §6.8 sandbox acceptance (shorten the renew cadence to observe per-task renewals fast),
// mirroring the stall-timeout override pattern.
const (
	DefaultLeaseRenewEvery = 60 * time.Second
	DefaultGCInterval      = time.Hour
)

func leaseRenewEvery() time.Duration {
	if d := envDuration("AC_LEASE_RENEW_EVERY"); d > 0 {
		return d
	}
	return DefaultLeaseRenewEvery
}

func gcInterval() time.Duration {
	if d := envDuration("AC_GC_INTERVAL"); d > 0 {
		return d
	}
	return DefaultGCInterval
}

func envDuration(key string) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 0
}

// drainLeaseRenewals renews the execution lease for every in-flight task this agent
// runs (deduped): the supervisor's current task + each live executor's task. Rate-
// limited to leaseRenewEvery. Best-effort — a per-task failure logs and never aborts
// the rest (the next tick re-arms; a persistent failure lets the server's nudge
// backstop engage). Emits one `renewed execution lease task=…` line per task so a real
// run can confirm EVERY in-flight task is covered.
func (r *LocalRuntime) drainLeaseRenewals(ctx context.Context, now time.Time) {
	if r.cfg.Reporter == nil {
		return
	}
	r.mu.Lock()
	if !r.nextLeaseRenewAt.IsZero() && now.Before(r.nextLeaseRenewAt) {
		r.mu.Unlock()
		return
	}
	r.nextLeaseRenewAt = now.Add(leaseRenewEvery())
	// The supervisor's current task, iff a live session is up and not being torn down
	// (an intentional stop / survival detach should let its lease lapse). r.mu is the
	// SessionState lock.
	var superTask string
	if r.state != nil && r.state.Session != nil && !r.state.ExpectedStop && !r.state.Detaching {
		superTask = r.state.CurrentTaskID
	}
	r.mu.Unlock()

	// Union of all in-flight task ids: supervisor's + each live executor's (including
	// adopted/recovering orphans, which SnapshotConcurrency already surfaces — a task
	// mid-tier-recovery is still should-continue and must keep its lease). Dedup so a
	// supervisor task that is also an executor task is renewed once. execTasks records
	// which of them map to a LIVE EXECUTOR (not the supervisor session) so a revoked
	// renew fuses the executor process — never the supervisor session (误杀 an agent).
	tasks := make(map[string]struct{})
	execTasks := make(map[string]struct{})
	if superTask != "" {
		tasks[superTask] = struct{}{}
	}
	for _, s := range r.SnapshotConcurrency() {
		if s.TaskID != "" {
			tasks[s.TaskID] = struct{}{}
			execTasks[s.TaskID] = struct{}{}
		}
	}
	for taskID := range tasks {
		err := r.cfg.Reporter.RenewTaskLease(ctx, r.cfg.AgentID, taskID, now)
		switch {
		case err == nil:
			r.log("agent=%s renewed execution lease task=%s", r.cfg.AgentID, taskID)
		case errors.Is(err, ErrLeaseRevoked):
			// The center revoked this task's lease (blocked mid-flight / reassigned /
			// terminal). Circuit-break: if a LIVE executor is running it, graceful-kill
			// the executor so it stops before the next dangerous action (issue-88e32d98).
			// A supervisor-only task (no executor) is NOT killed — blocking the session
			// would误杀 the agent; the block is handled by the normal supervisor path.
			if _, isExec := execTasks[taskID]; isExec {
				if killed, kErr := r.fuseExecutorForTask(ctx, taskID); kErr != nil {
					r.log("agent=%s lease-revoked task=%s: fuse-kill: %v", r.cfg.AgentID, taskID, kErr)
				} else if killed {
					r.log("agent=%s lease-revoked task=%s: fused (graceful-killed) executor", r.cfg.AgentID, taskID)
				} else {
					r.log("agent=%s lease-revoked task=%s: no live executor to fuse", r.cfg.AgentID, taskID)
				}
			} else {
				r.log("agent=%s lease-revoked task=%s: supervisor task, not fusing session", r.cfg.AgentID, taskID)
			}
		default:
			r.log("agent=%s lease-renew task=%s: %v", r.cfg.AgentID, taskID, err)
		}
	}
}

// fuseExecutorForTask circuit-breaks the live executor running taskID (issue-88e32d98
// P0 block-fuse). The fuseExecutor seam lets tests record the fuse without signalling a
// real process; nil (production) routes to the executor engine's graceful kill. Returns
// killed=true when a matching live executor was found and signalled; (false,nil) when
// there is no executor engine or no executor is running that task.
func (r *LocalRuntime) fuseExecutorForTask(ctx context.Context, taskID string) (bool, error) {
	if r.fuseExecutor != nil {
		return r.fuseExecutor(ctx, taskID)
	}
	ee := r.execEngine()
	if ee == nil {
		return false, nil
	}
	return ee.FuseExecutorForTask(ctx, taskID)
}

// maybeRunGC sweeps THIS agent's task-dir residue if gcInterval has elapsed (T860
// piece ③: the agent-runtime GCs its own per-agent residue — self-contained). No-op
// without a TaskDirManager.
func (r *LocalRuntime) maybeRunGC(now time.Time) {
	if r.cfg.TaskDirManager == nil {
		return
	}
	r.mu.Lock()
	if !r.lastGCAt.IsZero() && now.Sub(r.lastGCAt) < gcInterval() {
		r.mu.Unlock()
		return
	}
	r.lastGCAt = now
	r.mu.Unlock()

	_, tasksDir, _, err := r.agentPaths(r.cfg.AgentID)
	if err != nil {
		return
	}
	result := taskexec.RunGC(tasksDir, taskexec.DefaultGCConfig(), now)
	if result.AbortedCleaned > 0 || result.DoneCleaned > 0 || result.LeftoverCleaned > 0 || len(result.Errors) > 0 {
		r.log("agent=%s gc aborted=%d done=%d leftover=%d errors=%d",
			r.cfg.AgentID, result.AbortedCleaned, result.DoneCleaned, result.LeftoverCleaned, len(result.Errors))
	}
}
