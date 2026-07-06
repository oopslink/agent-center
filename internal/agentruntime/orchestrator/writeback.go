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
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
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
	// ResetTask resets a confirmed-dead running task back to the pool (T862 tier-3
	// recovery): running→open, assignee/lease cleared, re-dispatched to a fresh
	// executor. confirmedDead is the owner's tier-3 assertion that lets the reset skip
	// the live-lease guard (the owner is still renewing a lease that would never lapse);
	// without it the center hard-rejects a task whose lease is still live.
	ResetTask(ctx context.Context, agentID, taskID string, confirmedDead bool) error
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

// UsageReporter is the optional seam (v2.20.0 F2 / T613) that relays a finished
// executor run's aggregate token usage to the center's report_usage tool. The
// executor itself never connects to the center (F1 isolation); it records the
// usage in output.json and the orchestrator — the sole writer, already authed —
// reports it here. nil disables usage reporting (the writeback degrades unchanged).
type UsageReporter interface {
	ReportUsage(ctx context.Context, s UsageSample) error
}

// UsageSample is one executor run's aggregate token usage, resolved from the run's
// input.json + output.json. TaskID is the fork-time Source.TaskRef bound at launch
// ("" = no task binding — kept empty, NEVER fabricated, so a task-less run is not
// mis-attributed; T613 acceptance ②). Model is input.json's resolved model.
type UsageSample struct {
	AgentID string
	TaskID  string
	Model   string
	Usage   executor.TokenUsage
	At      time.Time
}

// CenterWriteback implements executor.Writeback. One per agent. Its mutex makes
// the orchestrator the effective SOLE WRITER even though the daemon reaps each
// executor on its own goroutine (design §3) — concurrent completions serialize, so
// center writes (and a future memory write) never interleave/clobber.
type CenterWriteback struct {
	client  CenterClient
	fx      *executor.FileExchange
	agentID string
	mem     MemoryWriter  // optional; nil in W2
	usage   UsageReporter // optional; nil disables usage reporting (T613)
	// inject delivers a judgment prompt to the agent's supervisor session (option b,
	// issue-68ccb310): the supervisor reviews the executor's REAL delivery and calls
	// complete_task/block_task ITSELF. Replaces the daemon-side auto-writeback
	// (CompleteTask/BlockTask on exit outcome) — the "binding" + "complete without
	// delivering" root cause. nil ⇒ no supervisor to judge (single-claude/degraded);
	// the task path then errors rather than silently auto-completing.
	inject func(ctx context.Context, text string) error
	mu     sync.Mutex
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

// WithUsageReporter attaches the optional UsageReporter (the T613 usage seam).
func (w *CenterWriteback) WithUsageReporter(u UsageReporter) *CenterWriteback {
	w.usage = u
	return w
}

// WithSupervisorInjector wires the option-b seam (issue-68ccb310): it injects a
// judgment prompt into the agent's supervisor session so the supervisor reviews the
// executor's REAL delivery and calls complete_task/block_task itself, instead of the
// daemon auto-completing on exit outcome. Always wired in production (a concurrent
// agent that forks executors has a supervisor); nil only on the degraded/test path.
func (w *CenterWriteback) WithSupervisorInjector(fn func(ctx context.Context, text string) error) *CenterWriteback {
	w.inject = fn
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

	// Relay this run's token usage (T613) BEFORE the result routing — best-effort so
	// a usage-report failure never blocks the task completion/block (and so never
	// stalls teardown into a re-report that would double-complete the task).
	w.reportUsage(ctx, in, c)

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
		// option b (issue-68ccb310): do NOT auto-complete. Deliver the result to the
		// supervisor as a judgment turn; the supervisor reviews REAL delivery and calls
		// complete_task/block_task itself.
		if err := w.deliverJudgment(ctx, taskRef, "succeeded", summary); err != nil {
			return err
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
		// option b: deliver the failure to the supervisor for a JUDGED outcome. The
		// supervisor still decides — a failed/crashed run usually blocks (retryable),
		// but partial delivery may warrant complete; either way the supervisor writes.
		outcome := "failed"
		if c.Kind == executor.OutcomeCrashed {
			outcome = "crashed"
		}
		if err := w.deliverJudgment(ctx, taskRef, outcome, reason); err != nil {
			return err
		}
		return w.writeMemory(ctx, in, c)
	}
	if err := w.relayToChat(ctx, in, reason); err != nil {
		return err
	}
	return w.writeMemory(ctx, in, c)
}

// deliverJudgment injects a judgment prompt into the supervisor session (option b,
// issue-68ccb310): the supervisor reviews the executor's REAL delivery and calls
// complete_task/block_task itself. Errors if no injector is wired (no supervisor to
// judge) — the task is NEVER silently auto-completed on exit outcome.
func (w *CenterWriteback) deliverJudgment(ctx context.Context, taskRef, outcome, summary string) error {
	if w.inject == nil {
		return fmt.Errorf("orchestrator: writeback no supervisor injector for task %s (cannot judge — refusing to auto-complete)", taskRef)
	}
	if err := w.inject(ctx, judgmentPrompt(taskRef, outcome, summary)); err != nil {
		return fmt.Errorf("orchestrator: writeback inject judgment for task %s: %w", taskRef, err)
	}
	return nil
}

// judgmentPrompt renders the supervisor-facing judgment turn for a finished executor
// (option b). It instructs the supervisor to judge REAL delivery — not exit status —
// before completing or blocking, which is what roots out "complete without delivering".
func judgmentPrompt(taskRef, outcome, summary string) string {
	s := strings.TrimSpace(summary)
	if len(s) > maxRelayChars {
		s = s[:maxRelayChars]
	}
	return fmt.Sprintf(
		"[executor finished] Your forked executor for task %s exited: outcome=%s.\n"+
			"Its self-reported summary/reason:\n%s\n\n"+
			"Now JUDGE the real delivery — check git (a new commit / pushed to the branch?), "+
			"whether the task's objective was actually met — then call complete_task(task_id=%q) "+
			"if it TRULY delivered, or block_task(task_id=%q, reason=...) if it did not deliver or "+
			"failed. Do NOT complete on exit status alone: a run that produced nothing must be "+
			"blocked (retryable), never completed.",
		taskRef, outcome, s, taskRef, taskRef,
	)
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

// reportUsage relays the run's aggregate token usage to the center (T613). It is
// BEST-EFFORT: with no reporter wired, no usage recorded, or an all-zero usage it
// is a no-op; a reporter error is swallowed (the usage path must never fail the
// writeback). The task_id is input.json's Source.TaskRef VERBATIM — empty stays
// empty so a task-less run is never mis-attributed (acceptance ②); when present,
// the center uses it directly and skips its sole-running-task fallback (T605:
// "源头已带则中心不再兜底"). The model is the run's resolved input.json model.
func (w *CenterWriteback) reportUsage(ctx context.Context, in executor.Input, c executor.Completion) {
	if w.usage == nil || c.Output == nil || c.Output.Usage == nil || c.Output.Usage.IsZero() {
		return
	}
	at := c.Output.FinishedAt // turn-time for point-in-time pricing; zero → center stamps now
	if err := w.usage.ReportUsage(ctx, UsageSample{
		AgentID: w.agentID,
		TaskID:  strings.TrimSpace(in.Source.TaskRef),
		Model:   in.Model,
		Usage:   *c.Output.Usage,
		At:      at,
	}); err != nil {
		// No logger seam here; a usage-report failure is non-fatal accounting loss,
		// never a reason to retain the executor dir or fail the task writeback.
		_ = err
	}
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
