package workerdaemon

// center_client.go — W2 daemon adapter: bridges the orchestrator's CenterClient
// port (internal/workerdaemon/orchestrator) to the daemon's authed admin
// transport (*AdminClient.CallAgentTool over the agent-tools endpoints). This is
// the production wiring that lets the executor Writeback reach the center WITHOUT
// the orchestrator package importing the daemon (it depends only on the port).
//
// Every call goes through the same worker-bearer transport as the rest of the
// daemon and carries the agent_id; the center authenticates the worker and maps it
// to the agent's business identity — the daemon never touches center storage
// directly (conventions §0.4).

import (
	"context"
	"encoding/json"

	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// agentToolCaller is the narrow seam the writeback adapter needs from the admin
// client: POST /admin/agent-tools/<tool>. *AdminClient satisfies it (CallAgentTool).
type agentToolCaller interface {
	CallAgentTool(ctx context.Context, tool string, body any, out *json.RawMessage) error
}

// centerClientAdapter implements orchestrator.CenterClient over an agentToolCaller.
type centerClientAdapter struct {
	caller agentToolCaller
}

// newCenterClient wraps an agentToolCaller as an orchestrator.CenterClient. Returns
// nil when the caller is nil (the daemon then wires a nil Writeback — graceful
// degrade to reap-and-free-slot, as in W1).
func newCenterClient(caller agentToolCaller) orchestrator.CenterClient {
	if caller == nil {
		return nil
	}
	return &centerClientAdapter{caller: caller}
}

// compile-time check.
var _ orchestrator.CenterClient = (*centerClientAdapter)(nil)

// CompleteTask → POST /admin/agent-tools/complete_task {agent_id, task_id, summary}.
// The center posts summary to the task's conversation AND completes it in one tx.
func (a *centerClientAdapter) CompleteTask(ctx context.Context, agentID, taskID, summary string) error {
	body := map[string]any{
		"agent_id": agentID,
		"task_id":  taskID,
		"summary":  summary,
	}
	return a.caller.CallAgentTool(ctx, "complete_task", body, nil)
}

// BlockTask → POST /admin/agent-tools/block_task {agent_id, task_id, reason, reason_type}.
// The center posts reason to the task's conversation AND blocks it in one tx.
func (a *centerClientAdapter) BlockTask(ctx context.Context, agentID, taskID, reason, reasonType string) error {
	body := map[string]any{
		"agent_id":    agentID,
		"task_id":     taskID,
		"reason":      reason,
		"reason_type": reasonType,
	}
	return a.caller.CallAgentTool(ctx, "block_task", body, nil)
}

// PostMessage → POST /admin/agent-tools/post_message {agent_id, target{conversation}, content}.
func (a *centerClientAdapter) PostMessage(ctx context.Context, agentID, conversationID, content string) error {
	body := map[string]any{
		"agent_id": agentID,
		"target":   map[string]any{"type": "conversation", "id": conversationID},
		"content":  content,
	}
	return a.caller.CallAgentTool(ctx, "post_message", body, nil)
}
