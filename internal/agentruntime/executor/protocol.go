// Package executor implements F2 of the agent-concurrent-execution design
// (docs/design/features/agent-concurrent-execution.md §6.D / §7 / §9 / §12):
// the file-exchange protocol and workspace (git worktree) isolation between an
// agent's resident orchestrator (监工) and its on-demand executors.
//
// The orchestrator is the ONLY party that talks to the center / mcp; an executor
// is a pure-compute worker that never connects to the center and communicates
// with the orchestrator EXCLUSIVELY through files under its own directory:
//
//	<agent_root>/executors/<executor_id>/
//	  input.json      # orchestrator writes: goal + aggregated context + model + source refs
//	  workspace/      # the executor's git worktree (isolation; executor only touches files here)
//	  progress.jsonl  # executor appends streaming progress (orchestrator relays to chat)
//	  output.json     # executor writes: final result / error detail
//	  status          # completion signal + detail (state/model/started_at/last_progress_at/error/summary)
//
// This package owns ONLY the worker-host filesystem protocol — the value objects,
// their on-disk lifecycle, the containment guard, and worktree provisioning. It
// is the F2 analog of internal/agentruntime/taskexec (per-execution dir承载本机状态,
// ADR-0018) and intentionally connects to NO center DB / AppService: there is no
// domain state here, only the durable file contract the two processes agree on.
// Orchestrator-side decisions (model routing, center writeback, spawning, watchdog)
// belong to F1/F3/F5 and consume this package — they are not implemented here.
//
// Naming note (conventions §12): the isolation directory's CONCEPT is a Worktree;
// the on-disk leaf is named "workspace/" because that string is part of the
// file-exchange wire contract fixed by design §7, which both the orchestrator and
// the (separately-spawned) executor must agree on byte-for-byte.
package executor

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// State is the executor lifecycle state recorded in the status file (design §9).
// Unknown values are rejected by Validate rather than silently defaulted
// (conventions §17: unknown protocol values must surface, never noop).
type State string

const (
	// StateRunning — the executor is working; progress is being streamed.
	StateRunning State = "running"
	// StateDone — the executor finished successfully; output.json holds the result.
	StateDone State = "done"
	// StateFailed — the executor failed; Error carries the detail.
	StateFailed State = "failed"
)

// Valid reports whether s is one of the three known states.
func (s State) Valid() bool {
	switch s {
	case StateRunning, StateDone, StateFailed:
		return true
	default:
		return false
	}
}

// ErrorDetail is the machine+human error pair carried by a failed Output / Status
// (conventions §16: every reason-bearing field carries a human-readable message).
// Kind is the machine-readable reason enum the orchestrator routes on; Message is
// the human-readable detail relayed to chat / inspect / logs.
type ErrorDetail struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// Validate requires both halves of the pair to be present.
func (e ErrorDetail) Validate() error {
	if strings.TrimSpace(e.Kind) == "" {
		return errors.New("executor: error.kind required")
	}
	if strings.TrimSpace(e.Message) == "" {
		return errors.New("executor: error.message required")
	}
	return nil
}

// Goal is the per-executor task description the orchestrator assembles (design
// §6.B task layer): the title/description plus any issue spec text.
type Goal struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	IssueSpec   string `json:"issue_spec,omitempty"`
}

// SourceRefs records where the work came from so the orchestrator can route the
// result back to the originating chat / issue / task after reading output.json
// (design §7: the executor never sends results itself — only the orchestrator
// connects to the center).
type SourceRefs struct {
	ChatIDs  []string `json:"chat_ids,omitempty"`
	IssueRef string   `json:"issue_ref,omitempty"`
	TaskRef  string   `json:"task_ref,omitempty"`
}

// Input is input.json — written by the orchestrator, read by the executor
// (design §7). It is the executor's complete starting context: it carries no
// credentials and no center handle, by design.
type Input struct {
	ExecutorID string `json:"executor_id"`
	ProblemID  string `json:"problem_id,omitempty"`
	Goal       Goal   `json:"goal"`
	Model      string `json:"model"`
	// CLI is the executor CLI the orchestrator routed (claude-code|codex), persisted
	// for observability — the real-time concurrency snapshot reads it back from
	// input.json (v2.19.0). Empty for pre-v2.19 launches; not required at startup.
	CLI       string     `json:"cli,omitempty"`
	Context   string     `json:"context,omitempty"`
	Source    SourceRefs `json:"source"`
	CreatedAt time.Time  `json:"created_at"`
	// DispatchMode records how the center routed this node (issue-f30b7e7b N2/N4). The
	// center stamps it at dispatch so the worker-side writeback — which only sees
	// input.json — can tell an executor-fork Dev node from a supervisor-inline node that
	// should NOT have forked at all. Empty = unstamped / legacy (writeback keeps its prior
	// behavior). See DispatchMode* consts.
	DispatchMode string `json:"dispatch_mode,omitempty"`
}

// DispatchMode values for Input.DispatchMode (issue-f30b7e7b). The center sets one at
// dispatch (N2); the writeback reads it (N4). Empty = unstamped / legacy.
const (
	// DispatchModeExecutorFork — a normal Dev node dispatched to a forked executor.
	DispatchModeExecutorFork = "executor_fork"
	// DispatchModeSupervisorInline — a node the dispatch gate routes to the supervisor
	// inline (MCP-native / review-only); it should NOT fork. If one nonetheless reaches a
	// forked executor (bootstrap / race / legacy), the writeback treats a non-delivery as a
	// terminal auto-block, not a retryable judgment (retrying an inline fork is futile).
	DispatchModeSupervisorInline = "supervisor_inline"
)

// Validate enforces the invariants the executor relies on at startup.
func (in Input) Validate() error {
	if err := validateExecutorID(in.ExecutorID); err != nil {
		return err
	}
	if strings.TrimSpace(in.Goal.Title) == "" {
		return errors.New("executor: input.goal.title required")
	}
	if strings.TrimSpace(in.Model) == "" {
		// Per design §5 the orchestrator always resolves a model (hard override /
		// LLM difficulty / allowed_models / default) BEFORE spawning, so an empty
		// model in input.json is a protocol violation, not a "pick a default" hint.
		return errors.New("executor: input.model required")
	}
	if in.CreatedAt.IsZero() {
		return errors.New("executor: input.created_at required")
	}
	return nil
}

// TokenUsage is the aggregate token usage an executor observed from its runner's
// output, summed across the run's turns (v2.20.0 F2 / T613). The executor parses
// it from the model-routed agent CLI's stream and records it in output.json; the
// orchestrator — the only party with center credentials (F1 isolation) — relays it
// to the center's report_usage, tagged with input.json's Source.TaskRef. All
// fields are non-negative; an all-zero usage means "nothing observed" and is
// omitted from output.json (its pointer left nil).
type TokenUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

// IsZero reports whether no tokens were observed (nothing to account).
func (u TokenUsage) IsZero() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.CacheReadTokens == 0 && u.CacheWriteTokens == 0
}

// Validate guards the shape before persistence: a token count is never negative
// (conventions §17 — a malformed count must surface, not silently persist).
func (u TokenUsage) Validate() error {
	if u.InputTokens < 0 || u.OutputTokens < 0 ||
		u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 {
		return errors.New("executor: negative token count")
	}
	return nil
}

// Output is output.json — written by the executor, read by the orchestrator
// (design §7 / §9). Success drives the orchestrator's dual completion signal
// together with the process exit code.
type Output struct {
	ExecutorID string       `json:"executor_id"`
	Success    bool         `json:"success"`
	Result     string       `json:"result,omitempty"`
	Error      *ErrorDetail `json:"error,omitempty"`
	FinishedAt time.Time    `json:"finished_at"`
	// Usage is the run's aggregate token usage (v2.20.0 F2 / T613), nil when the
	// executor observed none. The orchestrator's writeback relays it to the center's
	// report_usage; nil/zero means there is nothing to account for this run.
	Usage *TokenUsage `json:"usage,omitempty"`
	// ThreadID is the codex thread_id captured from a cli=codex `--json` run's
	// thread.started event (T969). Empty for claude executors (claude uses a
	// pre-allocated session id) and for a codex run whose thread.started never
	// arrived. The orchestrator persists a non-empty value into Record.SessionID so a
	// later resume can `codex exec resume <thread_id>`; empty → tier-2 rerun.
	ThreadID string `json:"thread_id,omitempty"`
}

// Validate enforces the success/error symmetry: a failure MUST carry an
// ErrorDetail; a success MUST NOT (an error on a success is a contradiction the
// orchestrator would mis-route).
func (o Output) Validate() error {
	if err := validateExecutorID(o.ExecutorID); err != nil {
		return err
	}
	if o.FinishedAt.IsZero() {
		return errors.New("executor: output.finished_at required")
	}
	if o.Usage != nil {
		if err := o.Usage.Validate(); err != nil {
			return err
		}
	}
	if o.Success {
		if o.Error != nil {
			return errors.New("executor: output.error must be nil when success")
		}
		return nil
	}
	if o.Error == nil {
		return errors.New("executor: output.error required when not success")
	}
	if err := o.Error.Validate(); err != nil {
		return err
	}
	return nil
}

// Status is the status file (JSON) — written by the executor, read by the
// orchestrator for the completion signal and the watchdog (design §9). State is
// the source of truth for "running vs terminal"; LastProgressAt feeds the
// orchestrator's stall detection.
type Status struct {
	ExecutorID     string       `json:"executor_id"`
	State          State        `json:"state"`
	Model          string       `json:"model"`
	StartedAt      time.Time    `json:"started_at"`
	LastProgressAt time.Time    `json:"last_progress_at"`
	Error          *ErrorDetail `json:"error,omitempty"`
	Summary        string       `json:"summary,omitempty"`
	// Detail is the current-activity note while running ("读 task.go", "跑 go test") —
	// a SHORT, SANITIZED hint of what the runner is doing, refreshed on each heartbeat
	// (T880). It rides the executor.progress event so an operator sees the live action;
	// unlike Summary (the final one-line result, set at done) it changes during the run.
	Detail string `json:"detail,omitempty"`
}

// Validate enforces the state/error correspondence (design §9): only a failed
// status carries an error; a known state is mandatory.
func (s Status) Validate() error {
	if err := validateExecutorID(s.ExecutorID); err != nil {
		return err
	}
	if !s.State.Valid() {
		return fmt.Errorf("executor: status.state %q invalid", string(s.State))
	}
	if s.StartedAt.IsZero() {
		return errors.New("executor: status.started_at required")
	}
	if s.State == StateFailed {
		if s.Error == nil {
			return errors.New("executor: status.error required when failed")
		}
		if err := s.Error.Validate(); err != nil {
			return err
		}
	} else if s.Error != nil {
		return fmt.Errorf("executor: status.error must be nil when state=%s", string(s.State))
	}
	return nil
}

// ProgressEntry is one line of progress.jsonl — appended by the executor, tailed
// by the orchestrator to relay status to chat (design §7). Append-only: never
// rewritten, so a crash mid-run leaves a readable prefix.
type ProgressEntry struct {
	At      time.Time `json:"at"`
	Phase   string    `json:"phase,omitempty"`
	Message string    `json:"message"`
}

// Validate requires a timestamp and a message (the relayable payload).
func (p ProgressEntry) Validate() error {
	if p.At.IsZero() {
		return errors.New("executor: progress.at required")
	}
	if strings.TrimSpace(p.Message) == "" {
		return errors.New("executor: progress.message required")
	}
	return nil
}
