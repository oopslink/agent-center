package agentruntime

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
)

const defaultClonePrepareTimeout = 30 * time.Minute

type cloneEntry struct {
	inflight bool
	ready    *reporepo.PreparedClone
}

type cloneGate struct {
	mu      sync.Mutex
	entries map[string]*cloneEntry
	wg      sync.WaitGroup
}

func (r *LocalRuntime) clonePrepareTimeout() time.Duration {
	if d := r.cfg.ClonePrepareTimeout; d > 0 {
		return d
	}
	return defaultClonePrepareTimeout
}

// takePreparedClone transfers ownership of a completed independent clone to the
// spawn path. Deleting the entry ensures no later control retry can reuse it.
func (r *LocalRuntime) takePreparedClone(taskID string) (reporepo.PreparedClone, bool) {
	r.clones.mu.Lock()
	defer r.clones.mu.Unlock()
	entry := r.clones.entries[taskID]
	if entry == nil || entry.ready == nil {
		return reporepo.PreparedClone{}, false
	}
	clone := *entry.ready
	delete(r.clones.entries, taskID)
	return clone, true
}

func (r *LocalRuntime) discardPreparedClone(taskID string) bool {
	r.clones.mu.Lock()
	entry := r.clones.entries[taskID]
	if entry == nil || entry.ready == nil {
		r.clones.mu.Unlock()
		return false
	}
	path := entry.ready.WorkspacePath
	delete(r.clones.entries, taskID)
	r.clones.mu.Unlock()
	_ = os.RemoveAll(path)
	return true
}

// deferForClone starts one independent clone per task and returns immediately. A
// control command has a five-second transport deadline, so network clone work can
// never execute synchronously in SpawnExecutor.
func (r *LocalRuntime) deferForClone(agentID, taskID string, target reporepo.RepoTarget, req reporepo.CloneRequest) {
	r.clones.mu.Lock()
	if r.clones.entries == nil {
		r.clones.entries = make(map[string]*cloneEntry)
	}
	if entry := r.clones.entries[taskID]; entry != nil {
		r.clones.mu.Unlock()
		r.log("work_available agent=%s task=%s: independent clone already materializing — task left queued",
			agentID, taskID)
		return
	}
	r.clones.entries[taskID] = &cloneEntry{inflight: true}
	r.clones.mu.Unlock()

	r.log("work_available agent=%s task=%s: starting BACKGROUND independent clone (control command returns now, task re-driven on completion)",
		agentID, taskID)

	r.clones.wg.Add(1)
	go func() {
		defer r.clones.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), r.clonePrepareTimeout())
		clone, err := r.cfg.CloneMaterializer.PrepareClone(ctx, target, req)
		cancel()
		if err != nil {
			r.clones.mu.Lock()
			delete(r.clones.entries, taskID)
			r.clones.mu.Unlock()
			r.log("work_available agent=%s task=%s prepare independent clone: %v — executor NOT forked; failing task loud",
				agentID, taskID, err)
			r.failTaskRepoUnavailable(agentID, taskID, err)
			return
		}

		r.clones.mu.Lock()
		entry := r.clones.entries[taskID]
		if entry == nil {
			entry = &cloneEntry{}
			r.clones.entries[taskID] = entry
		}
		entry.inflight = false
		entry.ready = &clone
		r.clones.mu.Unlock()

		r.log("agent=%s task=%s: independent clone ready — re-driving deferred task", agentID, taskID)
		r.redriveDeferredClone(agentID, taskID)
	}()
}

func (r *LocalRuntime) redriveDeferredClone(agentID, taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), r.sourcePrewarmTimeout())
	res, err := r.SpawnExecutor(ctx, SpawnRequest{TaskID: taskID, redrive: true})
	cancel()
	if err != nil {
		r.discardPreparedClone(taskID)
		r.log("agent=%s task=%s re-drive after independent clone ready: %v", agentID, taskID, err)
		return
	}
	if res == nil {
		// SpawnExecutor owns cleanup after taking the clone. If it returned at an
		// earlier guard (reassigned/terminal), the clone remains in the gate and this
		// fallback removes it.
		if r.discardPreparedClone(taskID) {
			r.log("agent=%s task=%s re-drive after independent clone ready: not forked before consuming clone — removed prepared workspace",
				agentID, taskID)
		} else {
			r.log("agent=%s task=%s re-drive after independent clone ready: not forked — prepared clone was cleaned; waiting for a new work_available",
				agentID, taskID)
		}
		return
	}
	r.log("agent=%s task=%s re-drive after independent clone ready: forked executor=%s",
		agentID, taskID, res.ExecutorID)
}

func (r *LocalRuntime) waitClonePrewarm() {
	r.clones.wg.Wait()
}
