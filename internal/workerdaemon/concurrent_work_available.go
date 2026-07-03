package workerdaemon

// concurrent_work_available.go — W4a / F1 (issue I55): the LIVE trigger that makes
// W1 executor concurrency actually fire in production.
//
// Before this, agent.work_available only ever NUDGED the resident claude to run its
// MCP pull loop (workAvailable → Inject(workAvailableNudge)); the executor fork
// entrypoint (HandleWork) had NO live producer, so an opted-in agent's executor pool
// sat at active:0 forever. This file closes that gap: when a CONCURRENCY-ENABLED
// agent (one with an attached executorEngine — maybeAttachExecutorEngine only
// attaches for a non-codex agent whose profile opts in) receives work_available, the
// daemon itself:
//   1. pulls the task detail via the get_task agent-tool (title/description/model),
//   2. ADMITS the task by calling start_task on the agent's behalf (open→running) —
//      the center enforces the W4c ≤max_concurrent run-slot cap AND single-active
//      idempotency in that one transactional gate, and
//   3. forks an isolated executor for it (the W1 HandleWork chain) instead of nudging.
//
// MUTUAL EXCLUSION (防双跑, acceptance ②): the concurrency branch in workAvailable
// short-circuits — it NEVER also injects the pull nudge. The resident claude is not
// asked to self-start the same task, so the executor and the resident session can't
// both drive one task. start_task is the center-side admission gate that also makes
// this idempotent: a re-emitted work_available (the task is already running) or an
// at-cap agent is cleanly declined and we simply don't fork (the task stays queued;
// the wake sweep re-emits, and a freed slot admits it on a later tick).
//
// DEFAULT (non-concurrent) AGENTS ARE UNTOUCHED: with no executorEngine attached the
// agent keeps the legacy nudge/relaunch path in workAvailable byte-for-byte.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/modelrouter"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// centerTaskDetail is the subset of the get_task agent-tool projection the fork path
// needs to build a WorkItem (agentTaskMap in internal/admin/api). Unknown fields are
// ignored; model/org_ref are emitted only when set.
type centerTaskDetail struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Model       string `json:"model"`
	OrgRef      string `json:"org_ref"`
	// v2.31.0 (issue-9f749a19 Phase 1): repo hint from the get_task projection —
	// the project's PRIMARY repo reference (credential-free) plus base_ref (its
	// default branch). Emitted only when the project has a primary repo; absent on
	// older centers (the zero value is a nil Repo + empty BaseRef). Consumed by the
	// per-executor repo-workspace track; decoded here so the fields survive.
	Repo    *centerTaskRepo `json:"repo,omitempty"`
	BaseRef string          `json:"base_ref"`
}

// centerTaskRepo mirrors the agentRepoRefMap projection (internal/admin/api): a
// credential-free view of a project↔repo reference. Only the fields the fork path
// may need to build a repo workspace are decoded; unknown keys are ignored.
type centerTaskRepo struct {
	RefID         string `json:"ref_id"`
	RepoID        string `json:"repo_id"`
	Label         string `json:"label"`
	URL           string `json:"url"`
	Provider      string `json:"provider"`
	DefaultBranch string `json:"default_branch"`
	IsPrimary     bool   `json:"is_primary"`
}

// goalTitle derives the executor goal title: the task title, else the first
// non-blank line of the description, else a stable task-id fallback.
func (d *centerTaskDetail) goalTitle(taskID string) string {
	if t := strings.TrimSpace(d.Title); t != "" {
		return t
	}
	if t := firstNonEmptyLine(d.Description); t != "" {
		return t
	}
	return "task " + taskID
}

// forkOnWorkAvailable is the W4a concurrency-path handler for agent.work_available:
// it admits the task through the center (start_task, ≤N cap) and forks an executor
// for it. It is best-effort and NON-WEDGING — every failure is logged and swallowed
// (the caller always acks the wake so the single-cursor control loop never stalls);
// a declined/failed admission simply leaves the task queued for a later re-emit.
//
// Called ONLY from workAvailable, on the single ControlLoop executor goroutine, so
// the get_task → start_task → fork sequence is never concurrent with another invocation
// for the same agent (no in-process double-fork race; start_task guards cross-process).
func (c *AgentController) forkOnWorkAvailable(ctx context.Context, agentID, taskID string, ee *executorEngine) {
	if strings.TrimSpace(taskID) == "" {
		c.log("work_available agent=%s concurrency fork: empty task_id — skipping", agentID)
		return
	}
	caller := c.cfg.ToolCaller
	if caller == nil {
		// No agent-tool transport wired (dormant / pre-cutover / unit harness): we can
		// neither read the task nor admit it. Leave it queued — the agent's next
		// daemon-boot reconcile (or a wired transport) picks it up.
		c.log("work_available agent=%s task=%s concurrency fork: no ToolCaller — left queued", agentID, taskID)
		return
	}

	// 1. Pull the task detail to build the WorkItem (title/description/model).
	task, err := c.fetchCenterTask(ctx, agentID, taskID)
	if err != nil {
		c.log("work_available agent=%s task=%s get_task: %v — left queued", agentID, taskID, err)
		return
	}

	// Cheap idempotency precheck: only an OPEN/REOPENED task is startable (pm task
	// transitions). A re-emit for an already-running task (forked earlier, or started
	// by another party) is skipped here without burning a start_task call. start_task
	// below is still the authoritative gate (this read is racy on its own).
	if st := strings.TrimSpace(task.Status); st != "" && st != "open" && st != "reopened" {
		c.log("work_available agent=%s task=%s already %s — not forking", agentID, taskID, st)
		return
	}

	// 2. Admission gate: 代 start_task (open→running). The center enforces the W4c
	//    ≤max_concurrent run-slot cap and single-active idempotency atomically; on any
	//    decline (at cap / already running / not runnable) we DON'T fork.
	if err := c.startCenterTask(ctx, agentID, taskID); err != nil {
		c.log("work_available agent=%s task=%s start_task declined (cap/again/not-runnable): %v — left queued",
			agentID, taskID, err)
		return
	}

	// 3. Fork the executor (W1 HandleWork chain).
	if err := c.launchExecutor(ctx, agentID, taskID, buildWorkItem(taskID, task), ee); err != nil {
		if errors.Is(err, modelrouter.ErrModelNotAllowed) {
			c.log("work_available agent=%s task=%s model not allowed: %v — blocking task", agentID, taskID, err)
			c.blockTaskOnModelNotAllowed(ctx, agentID, taskID, err)
			return
		}
		// The task is already running center-side but the local fork failed. This is a
		// rare reap-skew case (e.g. a finished executor's slot not yet freed when the
		// center already admitted): no executor runs (no double-run), and the execution
		// lease reclaims the task to open so the sweep re-dispatches it. Surface it loudly.
		c.log("work_available agent=%s task=%s started but fork failed: %v (lease will reclaim → re-dispatch)",
			agentID, taskID, err)
	}
}

// blockTaskOnModelNotAllowed blocks a task whose task.model is not in the
// agent's allowed_executors. The task was already started (open→running) by
// startCenterTask, so we block it (running→blocked) with an obstacle reason.
func (c *AgentController) blockTaskOnModelNotAllowed(ctx context.Context, agentID, taskID string, err error) {
	if c.cfg.ToolCaller == nil {
		return
	}
	body := map[string]any{
		"agent_id":    agentID,
		"task_id":     taskID,
		"reason":      err.Error(),
		"reason_type": "obstacle",
	}
	if bErr := c.cfg.ToolCaller.CallAgentTool(ctx, "block_task", body, nil); bErr != nil {
		c.log("work_available agent=%s task=%s block_task failed: %v", agentID, taskID, bErr)
	}
}

// buildWorkItem maps a center task detail onto the orchestrator WorkItem the W1 fork
// chain consumes. TaskRef carries the CANONICAL task_id (not the T<n> org ref) so the
// W2 CenterWriteback completes/blocks the right task via Source.TaskRef (acceptance ③);
// TaskModel carries task.model so the §5 model chain can hard-override ("" → the judge
// / default_executor_model fallback).
func buildWorkItem(taskID string, task *centerTaskDetail) orchestrator.WorkItem {
	return orchestrator.WorkItem{
		TaskID:  taskID,
		TaskRef: taskID,
		Goal: executor.Goal{
			Title:       task.goalTitle(taskID),
			Description: task.Description,
		},
		TaskModel: task.Model,
	}
}

// fetchCenterTask reads one task's detail via the get_task agent-tool (the same
// authed transport the W2 writeback uses). The center re-checks the worker→agent
// binding and read scope; we only decode the projection we need.
func (c *AgentController) fetchCenterTask(ctx context.Context, agentID, taskID string) (*centerTaskDetail, error) {
	var raw json.RawMessage
	body := map[string]any{"agent_id": agentID, "task_id": taskID}
	if err := c.cfg.ToolCaller.CallAgentTool(ctx, "get_task", body, &raw); err != nil {
		return nil, err
	}
	var t centerTaskDetail
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("decode get_task response: %w", err)
	}
	return &t, nil
}

// startCenterTask admits a task by calling start_task on the agent's behalf
// (open→running). The center enforces the W4c run-slot cap + single-active gate; a
// non-2xx surfaces as the caller's error (cap/again/not-runnable) so we don't fork.
func (c *AgentController) startCenterTask(ctx context.Context, agentID, taskID string) error {
	body := map[string]any{"agent_id": agentID, "task_id": taskID}
	return c.cfg.ToolCaller.CallAgentTool(ctx, "start_task", body, nil)
}
