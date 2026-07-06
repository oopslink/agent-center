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
	"os"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
)

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
	// supervisor task that is also an executor task is renewed once.
	tasks := make(map[string]struct{})
	if superTask != "" {
		tasks[superTask] = struct{}{}
	}
	for _, s := range r.SnapshotConcurrency() {
		if s.TaskID != "" {
			tasks[s.TaskID] = struct{}{}
		}
	}
	for taskID := range tasks {
		if err := r.cfg.Reporter.RenewTaskLease(ctx, r.cfg.AgentID, taskID, now); err != nil {
			r.log("agent=%s lease-renew task=%s: %v", r.cfg.AgentID, taskID, err)
		} else {
			r.log("agent=%s renewed execution lease task=%s", r.cfg.AgentID, taskID)
		}
	}
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
