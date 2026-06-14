package cli

import (
	"context"
	"encoding/json"
	"strconv"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/environment"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// T53 — paused-node resume adapter. It implements pmservice.NodeResumer over the
// agent service (resume) + env control (the agent.work_available wake), so the pm
// BC can offer an OPERATOR "resume stuck node" action without importing agent or
// environment. Composition-root glue: pm authorizes (project member + plan
// running), then delegates the cross-BC effect here.

// commandTypeWorkAvailable + the payload shape MIRROR the pm WorkItemProjector's
// per-agent wake (the daemon reads agent_id; work_item_id is idempotency/observe).
// Duplicated here intentionally — it is a stable wire contract, and importing the
// projector's unexported const would couple composition to its internals.
const commandTypeWorkAvailable = "agent.work_available"

type workAvailablePayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
}

// nodeResumerWorkItems is the slice of the agent WorkItem repo the adapter needs.
type nodeResumerWorkItems interface {
	ListByTask(ctx context.Context, taskRef string) ([]*agentpkg.AgentWorkItem, error)
	FindByID(ctx context.Context, id string) (*agentpkg.AgentWorkItem, error)
}

// nodeResumerOperator resumes a paused work item on behalf of an operator and
// returns the owning agent id (agent service ResumeWorkByOperator).
type nodeResumerOperator interface {
	ResumeWorkByOperator(ctx context.Context, workItemID string) (agentpkg.AgentID, error)
}

// nodeResumerAgents resolves an agent (for its worker binding + lifecycle).
type nodeResumerAgents interface {
	FindByID(ctx context.Context, id agentpkg.AgentID) (*agentpkg.Agent, error)
}

// nodeResumerWaker appends a worker control command (env control EnqueueCommand).
type nodeResumerWaker interface {
	EnqueueCommand(ctx context.Context, in environment.AppendCommandInput) (*environment.WorkerControlEvent, error)
}

// NodeResumerAdapter implements pmservice.NodeResumer (T53).
type NodeResumerAdapter struct {
	workItems nodeResumerWorkItems
	resumer   nodeResumerOperator
	agents    nodeResumerAgents
	waker     nodeResumerWaker
}

// NewNodeResumerAdapter wires the adapter from the composition-root dependencies.
func NewNodeResumerAdapter(workItems nodeResumerWorkItems, resumer nodeResumerOperator, agents nodeResumerAgents, waker nodeResumerWaker) *NodeResumerAdapter {
	return &NodeResumerAdapter{workItems: workItems, resumer: resumer, agents: agents, waker: waker}
}

// ResumePausedNode finds the task's paused work item, resumes it (paused→active via
// the operator path), then wakes the owning agent so it re-engages. The wake is a
// BEST-EFFORT signal (mirrors WorkItemProjector.enqueueWork): if the agent has no
// worker binding or is not running, the resume still stands (the item is active and
// AgentControlProjector re-delivers on lifecycle→running) — the resume error is NOT
// masked by a wake failure. Returns pmservice.ErrNodeNotPaused when the task has no
// paused work item.
func (a *NodeResumerAdapter) ResumePausedNode(ctx context.Context, taskRef string) error {
	items, err := a.workItems.ListByTask(ctx, taskRef)
	if err != nil {
		return err
	}
	var paused *agentpkg.AgentWorkItem
	for _, wi := range items {
		if wi.Status() == agentpkg.WorkItemPaused {
			paused = wi
			break
		}
	}
	if paused == nil {
		return pmservice.ErrNodeNotPaused
	}
	agentID, err := a.resumer.ResumeWorkByOperator(ctx, paused.ID())
	if err != nil {
		return err
	}
	a.wake(ctx, agentID, paused.ID())
	return nil
}

// wake enqueues the per-agent agent.work_available command so a resumed agent pulls
// its now-active item. Best-effort: a missing worker binding, an unresolved agent,
// or an enqueue error is swallowed (the resume already succeeded). The idempotency
// key carries the work item's CURRENT version so a SUBSEQUENT resume of the same
// item (after it is paused again) delivers a fresh wake rather than being deduped
// against the prior one.
func (a *NodeResumerAdapter) wake(ctx context.Context, agentID agentpkg.AgentID, workItemID string) {
	ag, err := a.agents.FindByID(ctx, agentID)
	if err != nil || ag == nil || ag.WorkerID() == "" {
		return
	}
	ver := 0
	if wi, ferr := a.workItems.FindByID(ctx, workItemID); ferr == nil && wi != nil {
		ver = wi.Version()
	}
	payload, err := json.Marshal(workAvailablePayload{AgentID: string(agentID), WorkItemID: workItemID})
	if err != nil {
		return
	}
	_, _ = a.waker.EnqueueCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(ag.WorkerID()),
		CommandType:    commandTypeWorkAvailable,
		Payload:        string(payload),
		IdempotencyKey: "agent.work_available:resume:" + workItemID + ":v" + strconv.Itoa(ver),
	})
}
