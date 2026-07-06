package agentruntime

// center_client.go — the daemon adapter that bridges the orchestrator's CenterClient
// / UsageReporter ports to the daemon's authed admin transport, moved (Phase 0c) into
// agentruntime because buildExecutorEngine (now a LocalRuntime method) constructs the
// writeback from it. ToolCaller is the narrow POST-agent-tool seam; workerdaemon
// aliases it back (agentToolCaller = agentruntime.ToolCaller) and *AdminClient
// satisfies it. This file imports only orchestrator (which does not import workerdaemon).

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
)

// ToolCaller is the narrow seam the writeback adapter needs from the admin client:
// POST /admin/agent-tools/<tool>. *AdminClient satisfies it (CallAgentTool).
type ToolCaller interface {
	CallAgentTool(ctx context.Context, tool string, body any, out *json.RawMessage) error
}

// centerClientAdapter implements orchestrator.CenterClient over a ToolCaller.
type centerClientAdapter struct {
	caller ToolCaller
}

// newCenterClient wraps a ToolCaller as an orchestrator.CenterClient. Returns nil
// when the caller is nil (the runtime then wires a nil Writeback — graceful degrade
// to reap-and-free-slot, as in W1).
func newCenterClient(caller ToolCaller) orchestrator.CenterClient {
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

// ResetTask → POST /admin/agent-tools/reset_task {agent_id, task_id, confirmed_dead}. The
// center resets a confirmed-dead running task back to the pool (running→open, assignee/lease
// cleared) and re-dispatches it to a fresh executor. T862 tier-3 recovery. confirmedDead is
// the owner's tier-3 assertion (RecoverFresh path) that lets the reset skip the live-lease
// guard the owner is itself still renewing — see task.ResetToOpen bypassLease.
func (a *centerClientAdapter) ResetTask(ctx context.Context, agentID, taskID string, confirmedDead bool) error {
	body := map[string]any{
		"agent_id":       agentID,
		"task_id":        taskID,
		"confirmed_dead": confirmedDead,
	}
	return a.caller.CallAgentTool(ctx, "reset_task", body, nil)
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

// InflightTask is one entry from list_my_inflight_tasks — an agent's active
// (open/running) task in the UNFILTERED in-flight set (design §4.2), including tasks
// whose deps are unsatisfied (which list_my_tasks drops). The reconcile pass (§4.4)
// reconciles each on-disk executor Record against this set: present ⇒ adopt/recover;
// absent (discarded / completed / reassigned / plan stopped) ⇒ stop + clean up.
type InflightTask struct {
	TaskID            string `json:"task_id"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	BlockedReason     string `json:"blocked_reason"`
	BlockedReasonType string `json:"blocked_reason_type"`
	BlockedComment    string `json:"blocked_comment"`
	LeaseExpiresAt    string `json:"lease_expires_at"`
}

// InflightTaskLister is the reconcile-facing read seam: list THIS agent's full
// in-flight task set from the center. Kept SEPARATE from orchestrator.CenterClient
// (the executor writeback surface — complete/block/post) because it is a reconcile
// concern, not an executor-writeback one. *centerClientAdapter satisfies it, so the
// runtime can build one over any ToolCaller (the daemon-injected *AdminClient OR its
// self-built *CenterHTTPClient).
type InflightTaskLister interface {
	ListMyInflightTasks(ctx context.Context, agentID string) ([]InflightTask, error)
}

// compile-time check.
var _ InflightTaskLister = (*centerClientAdapter)(nil)

// NewInflightTaskLister wraps a ToolCaller as an InflightTaskLister (nil ⇒ nil, matching
// newCenterClient's graceful-degrade contract). The reconcile pass builds one from the
// runtime's center client (daemon-injected or self-built).
func NewInflightTaskLister(caller ToolCaller) InflightTaskLister {
	if caller == nil {
		return nil
	}
	return &centerClientAdapter{caller: caller}
}

// ListMyInflightTasks → POST /admin/agent-tools/list_my_inflight_tasks {agent_id}. The
// center returns the UNFILTERED active set (ListAssignedAgentTasks), so a running task
// with unsatisfied deps is INCLUDED (unlike list_my_tasks). Returns an empty slice for
// a well-formed empty response.
func (a *centerClientAdapter) ListMyInflightTasks(ctx context.Context, agentID string) ([]InflightTask, error) {
	body := map[string]any{"agent_id": agentID}
	var raw json.RawMessage
	if err := a.caller.CallAgentTool(ctx, "list_my_inflight_tasks", body, &raw); err != nil {
		return nil, err
	}
	var resp struct {
		Tasks []InflightTask `json:"tasks"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("agentruntime: list_my_inflight_tasks: decode response: %w", err)
		}
	}
	if resp.Tasks == nil {
		resp.Tasks = []InflightTask{}
	}
	return resp.Tasks, nil
}

// CenterInflightLister returns an InflightTaskLister for this runtime's boot reconcile
// (§4.4). It PREFERS the daemon-injected center transport (cfg.ToolCaller, when the
// daemon hands one in) and FALLS BACK to a self-built *CenterHTTPClient from the
// runtime's OWN config (AdminURL / ServerFingerprint / WorkerToken) — the §4.2
// "runtime self-builds its center client" capability, so a runtime hosted WITHOUT a
// daemon-injected caller (the k8s target) still reconciles. Errors only when the
// fallback self-build fails (e.g. an unparseable AdminURL).
func (r *LocalRuntime) CenterInflightLister() (InflightTaskLister, error) {
	if tc := r.toolCaller(); tc != nil {
		return NewInflightTaskLister(tc), nil
	}
	c, err := NewCenterHTTPClient(r.cfg.AdminURL, r.cfg.ServerFingerprint, r.cfg.WorkerToken, 0)
	if err != nil {
		return nil, err
	}
	return NewInflightTaskLister(c), nil
}

// usageReporterAdapter implements orchestrator.UsageReporter over a ToolCaller
// (T613): it POSTs an executor run's aggregate token usage to the center's
// report_usage agent-tool, the SAME endpoint + worker-bearer transport the
// in-process per-turn hook uses. The executor never reaches here — only the
// orchestrator (sole writer, already authed) does, so executor default-deny holds.
type usageReporterAdapter struct {
	caller ToolCaller
}

// newUsageReporter wraps a ToolCaller as an orchestrator.UsageReporter. Returns nil
// when the caller is nil (the writeback then leaves usage reporting off — graceful
// degrade, matching newCenterClient).
func newUsageReporter(caller ToolCaller) orchestrator.UsageReporter {
	if caller == nil {
		return nil
	}
	return &usageReporterAdapter{caller: caller}
}

// compile-time check.
var _ orchestrator.UsageReporter = (*usageReporterAdapter)(nil)

// ReportUsage → POST /admin/agent-tools/report_usage. The body mirrors the
// in-process hook's shape (AdminClient.ReportUsage): task_id/cache fields are
// omitted when empty/zero so a task-less run carries no task_id (the center keeps
// it unattributed; acceptance ②), and a present task_id makes the center skip its
// sole-running-task fallback (T605).
func (a *usageReporterAdapter) ReportUsage(ctx context.Context, s orchestrator.UsageSample) error {
	body := map[string]any{
		"agent_id":      s.AgentID,
		"model":         s.Model,
		"input_tokens":  s.Usage.InputTokens,
		"output_tokens": s.Usage.OutputTokens,
	}
	if s.TaskID != "" {
		body["task_id"] = s.TaskID
	}
	if s.Usage.CacheReadTokens != 0 {
		body["cache_read_tokens"] = s.Usage.CacheReadTokens
	}
	if s.Usage.CacheWriteTokens != 0 {
		body["cache_write_tokens"] = s.Usage.CacheWriteTokens
	}
	if !s.At.IsZero() {
		body["ts"] = s.At.UTC().Format(time.RFC3339Nano)
	}
	return a.caller.CallAgentTool(ctx, "report_usage", body, nil)
}
