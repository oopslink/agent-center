package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// sweepPMReads is the PM read surface the session-heal sweep candidate builder needs
// (a narrowed subset of *pmservice.Service, for testability).
type sweepPMReads interface {
	// AgentTaskLoads returns the per-assignee active-task split (Running + Pending) for
	// the whole surface in one grouped query.
	AgentTaskLoads(ctx context.Context) (map[pm.IdentityRef]pm.AgentTaskLoad, error)
	// ListRunnableAgentTasks returns the assignee's tasks whose dependencies are
	// satisfied (open-or-running, deps resolved) — the same query list_my_tasks uses.
	ListRunnableAgentTasks(ctx context.Context, assignee pm.IdentityRef) ([]*pm.Task, error)
}

// sweepAgentReads is the agent read surface the builder needs (narrowed subset of
// agent.Repository).
type sweepAgentReads interface {
	FindByIdentityMemberID(ctx context.Context, identityMemberID string) (*agent.Agent, error)
	FindByID(ctx context.Context, id agent.AgentID) (*agent.Agent, error)
}

// buildSweepCandidates produces the SweepCandidates source for the WakeProjector's
// session-heal sweep: the agents that are desired-running but have queued runnable
// work and NO running task (≈ a dropped/idle session whose wake was lost).
//
// Detection (server-side, where session liveness is NOT observable — we infer it):
//   - AgentTaskLoads prefilter: Running == 0 && Pending > 0. A healthy busy agent has
//     >= 1 running task, so it is skipped — no false nudge of a working session.
//   - Lifecycle == running: only agents the center WANTS up. A stopped/stopping/error/
//     failed/archived agent is intentionally down — not the sweep's to resurrect (and
//     this is what excludes circuit-broken/terminal agents from the give-up churn).
//   - A confirmed RUNNABLE open task: Pending counts dependency-blocked tasks too,
//     which are not pullable; ListRunnableAgentTasks filters to deps-satisfied tasks,
//     so we only nudge an agent that genuinely has work it could start right now.
//
// The SweepCandidate.AgentID is the ENTITY id (a.ID()) the daemon session map and
// resume-state key on — NOT the agentActor/identity-member ref the task assignee
// carries (the two differ; resolveSweepAgent bridges them).
func buildSweepCandidates(pmr sweepPMReads, ar sweepAgentReads) func(context.Context) ([]envservice.SweepCandidate, error) {
	return func(ctx context.Context) ([]envservice.SweepCandidate, error) {
		loads, err := pmr.AgentTaskLoads(ctx)
		if err != nil {
			return nil, err
		}
		var out []envservice.SweepCandidate
		for ref, load := range loads {
			if load.Running != 0 || load.Pending == 0 {
				continue // busy (has a running task) or nothing pending — not stuck
			}
			s := string(ref)
			if !strings.HasPrefix(s, "agent:") {
				continue // user: assignees are not sessions to relaunch
			}
			ag := resolveSweepAgent(ctx, ar, strings.TrimPrefix(s, "agent:"))
			if ag == nil || ag.Lifecycle() != agent.LifecycleRunning {
				continue
			}
			worker := strings.TrimSpace(ag.WorkerID())
			if worker == "" {
				continue
			}
			runnable, err := pmr.ListRunnableAgentTasks(ctx, ref)
			if err != nil {
				return nil, err
			}
			taskID := firstOpenTaskID(runnable)
			if taskID == "" {
				continue // pending tasks are all dependency-blocked — not pullable
			}
			out = append(out, envservice.SweepCandidate{
				WorkerID: worker,
				AgentID:  string(ag.ID()),
				TaskID:   taskID,
			})
		}
		return out, nil
	}
}

// resolveSweepAgent maps a task assignee ref's id back to the execution Agent. The
// ref is agentActor(a) = "agent:" + identityMemberID when set, else "agent:" + entity
// id — so try the identity-member binding first, then the entity id. Returns nil when
// neither resolves (a stale/cross-entity ref → no candidate).
func resolveSweepAgent(ctx context.Context, ar sweepAgentReads, id string) *agent.Agent {
	if ag, err := ar.FindByIdentityMemberID(ctx, id); err == nil && ag != nil {
		return ag
	} else if err != nil && !errors.Is(err, agent.ErrAgentNotFound) {
		return nil
	}
	if ag, err := ar.FindByID(ctx, agent.AgentID(id)); err == nil {
		return ag
	}
	return nil
}

// firstOpenTaskID returns the id of the first OPEN task in the runnable set (a wake
// payload needs a task id as its dedup anchor). Running tasks are skipped — the
// candidate prefilter already guarantees there are none, but this keeps the payload
// anchored to genuinely-queued work.
func firstOpenTaskID(tasks []*pm.Task) string {
	for _, t := range tasks {
		if t.Status() == pm.TaskOpen {
			return string(t.ID())
		}
	}
	return ""
}
