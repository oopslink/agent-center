package orchestrator

// writeback.go — W2 (agent-concurrent-execution phase 2) center writeback: the
// orchestrator's SOLE-WRITER result sink (design §3 "唯一写入者" / §11.2 step g).
// It implements the executor.Writeback port the F5 Monitor calls in Finalize,
// BEFORE it tears down the executor dir — so a writeback failure preserves the
// durable state for a retry (no silent loss).
//
// An executor never connects to the center (F1 isolation); the orchestrator reads
// its result files and is the only party that writes back. On completion this:
//   - Succeeded → complete_task (the agent-tool atomically posts the summary to
//     the task's conversation AND completes the task in one center tx);
//   - Failed/Crashed → block_task (atomically posts the reason + blocks, design §9
//     "按失败处理"; a crash is flagged retryable in the message);
// reaching the center exclusively through the CenterClient port (the daemon wraps
// its authed admin transport — never a direct DB write, conventions §0.4).
//
// Source routing: the Completion carries only the executor id, so Report reads the
// executor's input.json (still present — Finalize reports before teardown) for the
// source refs the orchestrator recorded at fork time (TaskRef primary; ChatIDs as
// the fallback target when a work item had no task).
//
// NOT in W2 (documented seam): writing agent MEMORY. Memory is the supervisor's
// own per-agent git repo (internal/cognition/memory) and the scope id (project)
// is not yet plumbed into the WorkItem/Completion; a daemon-side memory write
// would also cross the supervisor's ownership of that repo (§0.4). The optional
// MemoryWriter seam is left so a follow-up plugs it once the scope is plumbed and
// the writer (daemon vs supervisor) is decided. The sole-writer mutex here already
// guarantees the "不并发覆盖" property when it lands.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// maxRelayChars bounds the summary/reason text relayed to the center conversation
// so a verbose executor result does not post a wall of text into chat.
const maxRelayChars = 1500

// CenterClient is the port the writeback uses to reach the center, via the agent
// -tools transport (the daemon wraps *AdminClient.CallAgentTool). All calls carry
// the agent id; the center authenticates the worker bearer and maps it to the
// agent's business identity (the sole writer is the orchestrator on the agent's behalf).
type CenterClient interface {
	// CompleteTask completes taskID; summary is posted to the task's conversation
	// in the SAME center tx (atomic relay + completion).
	CompleteTask(ctx context.Context, agentID, taskID, summary string) error
	// BlockTask blocks taskID with reason (reasonType "obstacle" | "input_required");
	// reason is posted to the task's conversation in the SAME center tx.
	BlockTask(ctx context.Context, agentID, taskID, reason, reasonType string) error
	// PostMessage posts content to a conversation (the fallback relay when a work
	// item has a source chat but no center task).
	PostMessage(ctx context.Context, agentID, conversationID, content string) error
}

// MemoryWriter is the (optional, W2-deferred) seam for writing agent memory after
// a completion. Left nil in W2; a follow-up supplies it once the memory scope is
// plumbed and the writer is decided (see file header).
type MemoryWriter interface {
	WriteCompletion(ctx context.Context, in executor.Input, c executor.Completion) error
}

// CenterWriteback implements executor.Writeback. One per agent. Its mutex makes
// the orchestrator the effective SOLE WRITER even though the daemon reaps each
// executor on its own goroutine (design §3) — concurrent completions serialize, so
// center writes (and a future memory write) never interleave/clobber.
type CenterWriteback struct {
	client  CenterClient
	fx      *executor.FileExchange
	agentID string
	mem     MemoryWriter // optional; nil in W2
	mu      sync.Mutex
}

// NewCenterWriteback validates deps and builds the writeback.
func NewCenterWriteback(client CenterClient, fx *executor.FileExchange, agentID string) (*CenterWriteback, error) {
	if client == nil {
		return nil, errors.New("orchestrator: writeback client required")
	}
	if fx == nil {
		return nil, errors.New("orchestrator: writeback exchange required")
	}
	if strings.TrimSpace(agentID) == "" {
		return nil, errors.New("orchestrator: writeback agent_id required")
	}
	return &CenterWriteback{client: client, fx: fx, agentID: agentID}, nil
}

// WithMemoryWriter attaches an optional MemoryWriter (the W2-deferred seam).
func (w *CenterWriteback) WithMemoryWriter(m MemoryWriter) *CenterWriteback {
	w.mem = m
	return w
}

// Report routes one finished executor's outcome to the center (executor.Writeback).
// It is serialized (sole writer) and reads the executor's input.json for the
// source refs. Running is never reported (the Monitor filters it). An unknown kind
// is surfaced as an error rather than silently dropped (conventions §17).
func (w *CenterWriteback) Report(ctx context.Context, c executor.Completion) error {
	if c.Kind == executor.OutcomeRunning {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	in, err := w.fx.ReadInput(c.ExecutorID)
	if err != nil {
		// Without input.json we cannot resolve the source to write back to. Surface
		// it (the Monitor keeps the dir on a Report error) rather than drop the result.
		return fmt.Errorf("orchestrator: writeback read input %s: %w", c.ExecutorID, err)
	}

	switch c.Kind {
	case executor.OutcomeSucceeded:
		return w.reportSuccess(ctx, in, c)
	case executor.OutcomeFailed, executor.OutcomeCrashed:
		return w.reportFailure(ctx, in, c)
	default:
		return fmt.Errorf("orchestrator: writeback unknown completion kind %q for %s", c.Kind, c.ExecutorID)
	}
}

// reportSuccess completes the source task (relaying the summary), or posts to the
// source chat when the work item had no task.
func (w *CenterWriteback) reportSuccess(ctx context.Context, in executor.Input, c executor.Completion) error {
	summary := successSummary(in, c)
	if taskRef := strings.TrimSpace(in.Source.TaskRef); taskRef != "" {
		if err := w.client.CompleteTask(ctx, w.agentID, taskRef, summary); err != nil {
			return fmt.Errorf("orchestrator: writeback complete_task %s: %w", taskRef, err)
		}
		return w.writeMemory(ctx, in, c)
	}
	if err := w.relayToChat(ctx, in, summary); err != nil {
		return err
	}
	return w.writeMemory(ctx, in, c)
}

// reportFailure blocks the source task with the failure reason (atomic relay +
// block), or posts the failure to the source chat when there is no task. A crash
// is retryable: it is flagged in the message so the owner/PM sees it may re-run.
func (w *CenterWriteback) reportFailure(ctx context.Context, in executor.Input, c executor.Completion) error {
	reason := failureReason(c)
	if taskRef := strings.TrimSpace(in.Source.TaskRef); taskRef != "" {
		// Both failed and crashed block with reason_type "obstacle" (needs owner/PM
		// attention); a real auto re-queue strategy for crashes is a follow-up (the
		// Monitor already retains a crashed executor's dir for relaunch).
		if err := w.client.BlockTask(ctx, w.agentID, taskRef, reason, "obstacle"); err != nil {
			return fmt.Errorf("orchestrator: writeback block_task %s: %w", taskRef, err)
		}
		return w.writeMemory(ctx, in, c)
	}
	if err := w.relayToChat(ctx, in, reason); err != nil {
		return err
	}
	return w.writeMemory(ctx, in, c)
}

// relayToChat posts content to the first source chat conversation. With neither a
// task nor a chat ref there is nowhere to write back — surfaced as an error so the
// result is not silently lost (conventions §17).
func (w *CenterWriteback) relayToChat(ctx context.Context, in executor.Input, content string) error {
	for _, chatID := range in.Source.ChatIDs {
		if strings.TrimSpace(chatID) == "" {
			continue
		}
		if err := w.client.PostMessage(ctx, w.agentID, chatID, content); err != nil {
			return fmt.Errorf("orchestrator: writeback post_message %s: %w", chatID, err)
		}
		return nil
	}
	return fmt.Errorf("orchestrator: writeback executor %s has no task or chat source to report to", in.ExecutorID)
}

// writeMemory invokes the optional MemoryWriter (nil in W2 → no-op). Best-effort is
// NOT used: a configured writer's failure is surfaced so memory loss is visible.
func (w *CenterWriteback) writeMemory(ctx context.Context, in executor.Input, c executor.Completion) error {
	if w.mem == nil {
		return nil
	}
	if err := w.mem.WriteCompletion(ctx, in, c); err != nil {
		return fmt.Errorf("orchestrator: writeback memory %s: %w", c.ExecutorID, err)
	}
	return nil
}

// successSummary builds the chat-relay summary for a success: the executor's
// one-line status summary if present, else its result text, bounded.
func successSummary(in executor.Input, c executor.Completion) string {
	if c.Status != nil && strings.TrimSpace(c.Status.Summary) != "" {
		return clip(c.Status.Summary)
	}
	if c.Output != nil && strings.TrimSpace(c.Output.Result) != "" {
		return clip(c.Output.Result)
	}
	return clip("executor completed: " + in.Goal.Title)
}

// failureReason builds the block reason for a failure/crash, including the
// machine-readable kind and the human-readable detail (conventions §16).
func failureReason(c executor.Completion) string {
	var b strings.Builder
	if c.Kind == executor.OutcomeCrashed {
		b.WriteString("executor crashed (retryable): ")
	} else {
		b.WriteString("executor failed: ")
	}
	if c.Error != nil && strings.TrimSpace(c.Error.Message) != "" {
		if k := strings.TrimSpace(c.Error.Kind); k != "" {
			b.WriteString("[" + k + "] ")
		}
		b.WriteString(c.Error.Message)
	} else {
		b.WriteString("no error detail reported")
	}
	return clip(b.String())
}

// clip bounds relayed text to maxRelayChars, appending an ellipsis when truncated.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxRelayChars {
		return s
	}
	return s[:maxRelayChars] + "…"
}
