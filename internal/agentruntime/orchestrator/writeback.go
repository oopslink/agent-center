package orchestrator

// writeback.go — W2 (agent-concurrent-execution phase 2) center writeback: the
// Supervisor control plane's SOLE-WRITER result sink (design §3 "唯一写入者" / §11.2 step g).
// It implements the executor.Writeback port the F5 Monitor calls in Finalize,
// BEFORE it tears down the executor dir — so a writeback failure preserves the
// durable state for a retry (no silent loss).
//
// An executor never connects to the center (F1 isolation); the Supervisor control
// plane reads its result files and is the only party that writes back. On completion this:
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
// agent's business identity (the sole writer is the Supervisor control plane on the agent's behalf).
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
	// PostToTask posts content to a TASK's conversation (issue-f30b7e7b P0-A ch2): the
	// eager-push delivery evidence (branch + SHA + pushed) is posted onto the task so the
	// review / PD / integration nodes can SEE which branch to check out / merge — not just
	// the judging supervisor (ch1). Distinct from PostMessage (which needs a conversation id;
	// the writeback only holds the task ref).
	PostToTask(ctx context.Context, agentID, taskID, content string) error
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
// usage in output.json and the Supervisor control plane — the sole writer, already authed —
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

// DeliveryReporter is the optional seam (issue-f30b7e7b) that relays a terminal
// executor's structured git delivery status to the center's report_delivery tool, so
// the center-side stuck-node reconcile can tell a recoverable dead process from a
// terminal-but-never-pushed (review-only) executor. Same shape as UsageReporter: the
// executor never connects to the center (F1 isolation); the Supervisor control plane — sole writer,
// already authed — reports it here. nil disables it (the writeback degrades unchanged).
type DeliveryReporter interface {
	ReportDelivery(ctx context.Context, s DeliverySample) error
}

// DeliverySample is one terminal executor's delivery-audit signal: the task it was
// bound to plus the worktree git status probed at finalize. TaskID is Source.TaskRef
// VERBATIM ("" ⇒ a task-less run, never reported — delivery is task-scoped). Git is
// non-nil only when the worktree was actually probed (a git workspace).
type DeliverySample struct {
	AgentID string
	TaskID  string
	Git     *executor.FinalizedGitStatus
}

// CenterWriteback implements executor.Writeback. One per agent. Its mutex makes
// the Supervisor control plane the effective SOLE WRITER even though the daemon reaps each
// executor on its own goroutine (design §3) — concurrent completions serialize, so
// center writes (and a future memory write) never interleave/clobber.
type CenterWriteback struct {
	client   CenterClient
	fx       *executor.FileExchange
	agentID  string
	mem      MemoryWriter     // optional; nil in W2
	usage    UsageReporter    // optional; nil disables usage reporting (T613)
	delivery DeliveryReporter // optional; nil disables delivery reporting (issue-f30b7e7b)
	// inject delivers a judgment prompt to the agent's Supervisor session (option b,
	// issue-68ccb310): the Supervisor reviews the executor's REAL delivery and calls
	// complete_task/block_task ITSELF. Replaces the daemon-side auto-writeback
	// (CompleteTask/BlockTask on exit outcome) — the "binding" + "complete without
	// delivering" root cause. nil ⇒ no Supervisor to judge (single-claude/degraded);
	// the task path then errors rather than silently auto-completing.
	inject func(ctx context.Context, taskRef, text string) error
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

// WithDeliveryReporter attaches the optional DeliveryReporter (issue-f30b7e7b).
func (w *CenterWriteback) WithDeliveryReporter(d DeliveryReporter) *CenterWriteback {
	w.delivery = d
	return w
}

// WithUsageReporter attaches the optional UsageReporter (the T613 usage seam).
func (w *CenterWriteback) WithUsageReporter(u UsageReporter) *CenterWriteback {
	w.usage = u
	return w
}

// WithSupervisorInjector wires the option-b seam (issue-68ccb310): it injects a
// judgment prompt into the agent's Supervisor session so the Supervisor reviews the
// executor's REAL delivery and calls complete_task/block_task itself, instead of the
// daemon auto-completing on exit outcome. Always wired in production (a concurrent
// agent that forks executors has a Supervisor); nil only on the degraded/test path.
func (w *CenterWriteback) WithSupervisorInjector(fn func(ctx context.Context, taskRef, text string) error) *CenterWriteback {
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
	// Relay this run's git delivery status (issue-f30b7e7b) BEFORE result routing —
	// best-effort, same as usage: it must never block or wedge the completion/block path.
	w.reportDelivery(ctx, in, c)

	// N4 defensive net (issue-f30b7e7b): a supervisor_inline node should be routed to the
	// supervisor WITHOUT a fork (N2 dispatch gate). If one nonetheless reaches a forked
	// executor (bootstrap / race / legacy) and did NOT produce a DURABLE (pushed) delivery,
	// auto-BLOCK it for a human — do NOT route it as success/judgment. This MUST run before
	// the success/failure split and gate on the git status (NOT c.Kind): the canonical
	// mis-fork is a deploy/verdict node whose deliverable is a CENTER action, forked into a
	// lone `claude -p` with an EMPTY, non-git workspace → probe !Probed → N3 TRUSTS it as
	// OutcomeSucceeded. So c.Kind would say "succeeded" and reportSuccess would false-succeed
	// + spin. Gating on !(Probed && Pushed) catches the empty-workspace case; only a genuine
	// durable push escapes — the safety valve for an N2 misclassification of a real code task.
	// Relaxes the pending-judgment "never write task state" rule to exactly ONE outcome:
	// auto-BLOCK (obstacle), never auto-complete.
	if taskRef := strings.TrimSpace(in.Source.TaskRef); taskRef != "" &&
		in.DispatchMode == executor.DispatchModeSupervisorInline &&
		!(c.Git != nil && c.Git.Probed && c.Git.Pushed) {
		if err := w.client.BlockTask(ctx, w.agentID, taskRef, inlineForkBlockReason(c), "obstacle"); err != nil {
			return err
		}
		return w.writeMemory(ctx, in, c)
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
		// issue-f30b7e7b P0-A ch2: surface the eager-push delivery evidence (branch + SHA +
		// pushed) onto the TASK conversation — BEFORE the judgment turn — so review / PD /
		// integration nodes can see which branch to check out / merge, not just the judging
		// Supervisor (ch1). Posting first means a post failure retries WITHOUT a duplicate
		// judgment inject. Skipped for a non-git (center-action) run.
		if note := deliveryNote(c.Git); note != "" {
			if err := w.client.PostToTask(ctx, w.agentID, taskRef, note); err != nil {
				return fmt.Errorf("orchestrator: writeback post delivery line to task %s: %w", taskRef, err)
			}
		}
		// option b (issue-68ccb310): do NOT auto-complete. Deliver the result to the
		// Supervisor as a judgment turn; the Supervisor reviews REAL delivery and calls
		// complete_task/block_task itself.
		if err := w.deliverJudgment(ctx, taskRef, "succeeded", summary, c.Git); err != nil {
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
		// option b: deliver the failure to the Supervisor for a JUDGED outcome. The
		// Supervisor still decides — a failed/crashed run usually blocks (retryable),
		// but partial delivery may warrant complete; either way the Supervisor writes.
		outcome := "failed"
		if c.Kind == executor.OutcomeCrashed {
			outcome = "crashed"
		}
		if err := w.deliverJudgment(ctx, taskRef, outcome, reason, c.Git); err != nil {
			return err
		}
		return w.writeMemory(ctx, in, c)
	}
	if err := w.relayToChat(ctx, in, reason); err != nil {
		return err
	}
	return w.writeMemory(ctx, in, c)
}

// deliverJudgment injects a judgment prompt into the Supervisor session (option b,
// issue-68ccb310): the Supervisor reviews the executor's REAL delivery and calls
// complete_task/block_task itself. Errors if no injector is wired (no Supervisor to
// judge) — the task is NEVER silently auto-completed on exit outcome.
func (w *CenterWriteback) deliverJudgment(ctx context.Context, taskRef, outcome, summary string, git *executor.FinalizedGitStatus) error {
	if w.inject == nil {
		return fmt.Errorf("orchestrator: writeback no supervisor injector for task %s (cannot judge — refusing to auto-complete)", taskRef)
	}
	if err := w.inject(ctx, taskRef, judgmentPrompt(taskRef, outcome, summary, git)); err != nil {
		return fmt.Errorf("orchestrator: writeback inject judgment for task %s: %w", taskRef, err)
	}
	return nil
}

// judgmentPrompt renders the Supervisor-facing judgment turn for a finished executor
// (option b). It instructs the Supervisor to judge REAL delivery — not exit status —
// before completing or blocking, which is what roots out "complete without delivering".
func judgmentPrompt(taskRef, outcome, summary string, git *executor.FinalizedGitStatus) string {
	s := strings.TrimSpace(summary)
	if len(s) > maxRelayChars {
		s = s[:maxRelayChars]
	}
	return fmt.Sprintf(
		"[executor finished] Your Agent's executor for task %s exited: outcome=%s.\n"+
			"Identity contract: you are this Agent's Supervisor control plane, and the executor is this same Agent's isolated execution unit. "+
			"Do not describe it as an external executor, outside agent, or someone else's delivery; final delivery remains YOUR judged responsibility.\n"+
			"Its self-reported summary/reason:\n%s\n%s\n"+
			"Now JUDGE the real delivery — check git (the reported delivery branch: is the commit "+
			"pushed on origin? does the SHA match? and verify its BASE is the EXPECTED baseline via "+
			"merge-base — not merely that it is recent; a branch cut from the wrong base silently "+
			"drops already-merged work), whether the task's objective was actually met "+
			"— then call complete_task(task_id=%q) if this Agent TRULY delivered, or "+
			"block_task(task_id=%q, reason=...) if it did not deliver or failed. Do NOT complete on "+
			"exit status alone: a run that produced nothing must be blocked (retryable), never "+
			"completed.",
		taskRef, outcome, s, deliveryLine(git), taskRef, taskRef,
	)
}

// deliveryLine renders the structured git delivery evidence (issue-f30b7e7b P0-A) so the
// judging supervisor knows EXACTLY which branch + SHA to inspect and whether the agent-
// runtime durably pushed it — closing the "pushed but nobody knows where to look = nominal
// delivery" gap (the reported ac-exec delivery branch is what review/integration checks out
// / merges). "" when there is no probed git status (a non-git / center-action work item).
func deliveryLine(git *executor.FinalizedGitStatus) string {
	if git == nil || !git.Probed {
		return ""
	}
	line := fmt.Sprintf("Reported delivery branch: %s (HEAD %s) pushed=%t.", git.Branch, git.HeadSHA, git.Pushed)
	// Surface the BASE lineage too (issue-f30b7e7b P0-A, hardened after a WebUI incident):
	// branch + SHA alone is not enough — a branch cut from the WRONG base silently drops
	// already-merged work, and only its base/merge-base reveals it. Show what the delivery
	// branched off so the reviewer can verify lineage, not just recency.
	switch {
	case git.BaseKnown:
		line += fmt.Sprintf(" Based on %s (ahead %d commit(s)).", git.BaseRef, git.AheadOfBase)
	case git.BaseRef != "":
		line += fmt.Sprintf(" Based on %s (ahead count unresolved — verify lineage).", git.BaseRef)
	}
	if git.PushError != "" {
		line += " eager-push FAILED: " + git.PushError + " — work is committed but NOT on origin."
	} else if git.Pushed {
		line += " The branch is on origin — check it out / merge it to review the delivery."
	}
	return "\n" + line
}

// deliveryNote renders the standalone delivery evidence posted onto the TASK conversation
// (issue-f30b7e7b P0-A ch2), so the review / PD / integration nodes can locate the branch to
// check out / merge — not just the judging supervisor (ch1). "" when there is no probed git
// status (a non-git / center-action work item — nothing to surface).
func deliveryNote(git *executor.FinalizedGitStatus) string {
	line := strings.TrimSpace(deliveryLine(git))
	if line == "" {
		return ""
	}
	return "📦 Executor delivery — " + line
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

// inlineForkBlockReason builds the human-facing block reason for a supervisor_inline node
// that was mis-forked as an executor and produced no durable delivery (N4). It names the
// shape (empty/non-git workspace vs committed-but-unpushed) and appends the executor's own
// failure detail when it reported one.
func inlineForkBlockReason(c executor.Completion) string {
	shape := "no durable delivery"
	switch {
	case c.Git == nil || !c.Git.Probed:
		shape = "empty / non-git workspace (this node's deliverable is a center action, not a git push)"
	case !c.Git.Pushed:
		shape = "committed but never pushed (work would die with the reaped worktree)"
	}
	reason := "supervisor-inline node was dispatched to a forked executor but produced " + shape +
		" — auto-blocked for human review; this node should run inline (supervisor), not as an executor fork (issue-f30b7e7b)"
	if c.Kind != executor.OutcomeSucceeded {
		if fr := strings.TrimSpace(failureReason(c)); fr != "" {
			reason += ": " + fr
		}
	}
	return reason
}

// reportDelivery relays a terminal executor's git delivery status to the center
// (issue-f30b7e7b). BEST-EFFORT: with no reporter wired, a task-less run, or no probed
// git status (c.Git nil ⇒ non-git / unresolvable workspace) it is a no-op; a reporter
// error is SWALLOWED — the delivery signal is a side-channel for the stuck-node
// reconcile, never a reason to fail or wedge the writeback (or retain the executor
// dir). task_id is Source.TaskRef VERBATIM (empty stays empty — delivery is
// task-scoped, never fabricated, mirroring reportUsage).
func (w *CenterWriteback) reportDelivery(ctx context.Context, in executor.Input, c executor.Completion) {
	taskRef := strings.TrimSpace(in.Source.TaskRef)
	if w.delivery == nil || taskRef == "" || c.Git == nil {
		return
	}
	if err := w.delivery.ReportDelivery(ctx, DeliverySample{
		AgentID: w.agentID,
		TaskID:  taskRef,
		Git:     c.Git,
	}); err != nil {
		// Non-fatal: a delivery-report failure is a lost side-channel signal, never a
		// reason to retain the dir or fail the task writeback.
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
