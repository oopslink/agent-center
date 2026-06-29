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
// is the F2 analog of internal/workerdaemon/taskexec (per-execution dir承载本机状态,
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
}

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

// Output is output.json — written by the executor, read by the orchestrator
// (design §7 / §9). Success drives the orchestrator's dual completion signal
// together with the process exit code.
type Output struct {
	ExecutorID string       `json:"executor_id"`
	Success    bool         `json:"success"`
	Result     string       `json:"result,omitempty"`
	Error      *ErrorDetail `json:"error,omitempty"`
	FinishedAt time.Time    `json:"finished_at"`
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
