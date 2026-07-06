package orchestrator

// recovery.go — §4.3 executor recovery ladder: the pure "recover primitive" the
// boot reconcile driver (D4) calls for each in-flight executor it decides should
// CONTINUE after an orchestrator restart. It classifies exactly one of three
// degradation rungs and hands back everything the driver needs to relaunch —
// performing NO side effects itself (no spawn, no git, no fs writes), mirroring
// executor.Reconciler's no-loss/no-duplication discipline. The driver executes
// the returned plan.
//
// The ladder (design §4.3), best → worst:
//   - RecoverResume (tier 1, full context): session-id present AND the workspace/
//     worktree still exists → re-run the SAME argv rewritten to `--resume <sid>`,
//     so claude continues the exact conversation. LLM context preserved.
//   - RecoverRerun (tier 2, degraded): workspace exists but the session cannot be
//     resumed (session-less CLI, or no session was bound) → re-run the persisted
//     RunnerCmd in the SAME workspace. Conversation context is lost, but progress
//     already committed to the worktree survives.
//   - RecoverFresh (tier 3, from scratch): the workspace/worktree is gone → the
//     driver must re-provision a fresh workspace and launch anew (re-derive the
//     runner). Nothing to resume or reuse.

import (
	"errors"
	"os"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

// RecoveryAction names a rung of the §4.3 degradation ladder.
type RecoveryAction int

const (
	// RecoverResume — tier 1: --resume the existing session in the existing workspace.
	RecoverResume RecoveryAction = iota + 1
	// RecoverRerun — tier 2: re-run the persisted RunnerCmd in the existing workspace.
	RecoverRerun
	// RecoverFresh — tier 3: workspace gone; provision fresh + launch anew.
	RecoverFresh
)

// String renders the action for logs.
func (a RecoveryAction) String() string {
	switch a {
	case RecoverResume:
		return "resume"
	case RecoverRerun:
		return "rerun"
	case RecoverFresh:
		return "fresh"
	default:
		return "unknown"
	}
}

// RecoveryPlan is the primitive's verdict for one executor: which rung, where to
// run, and the argv to run. The driver (D4) is responsible for enacting it.
type RecoveryPlan struct {
	ExecutorID string
	Action     RecoveryAction
	// Workspace is the existing directory to relaunch in for tier 1/2; empty for
	// tier 3 (the driver provisions a fresh one).
	Workspace string
	// RunnerCmd is the argv to run: the `--resume`-rewritten argv for tier 1, the
	// persisted fresh argv for tier 2, and nil for tier 3 (the driver re-derives a
	// fresh runner via the normal launch path).
	RunnerCmd []string
	// SessionID / BaseRef / RepoKey / SourcePath are carried from the durable Record
	// so the driver has the worktree teardown + provisioning handles it needs (esp.
	// for tier 3 re-provisioning).
	SessionID  string
	BaseRef    string
	RepoKey    string
	SourcePath string
}

// WorkspaceProbe reports whether an executor's workspace directory still exists on
// disk. Injected so the ladder is unit-testable without a real filesystem.
type WorkspaceProbe interface {
	Exists(path string) bool
}

// dirProbe is the production probe: a directory that stat's as a dir is present.
type dirProbe struct{}

func (dirProbe) Exists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// RecoveryPlanner turns a durable executor.Record into a RecoveryPlan by probing
// the executor's workspace and the resumability of its persisted argv.
type RecoveryPlanner struct {
	layout *executor.Layout
	probe  WorkspaceProbe
}

// NewRecoveryPlanner wires a planner over the executor Layout. A nil probe defaults
// to the production directory probe.
func NewRecoveryPlanner(layout *executor.Layout, probe WorkspaceProbe) (*RecoveryPlanner, error) {
	if layout == nil {
		return nil, errors.New("orchestrator: recovery planner layout required")
	}
	if probe == nil {
		probe = dirProbe{}
	}
	return &RecoveryPlanner{layout: layout, probe: probe}, nil
}

// Plan classifies one executor onto the §4.3 ladder. rec is the orchestrator's
// durable Record (nil when the executor was never tracked — no durable state, so
// there is nothing to resume or reuse → tier 3 fresh under the given id).
//
// It is a PURE decision: it stats the workspace and inspects the argv, but never
// spawns, kills, or writes. The reconcile driver enacts the returned plan.
func (p *RecoveryPlanner) Plan(executorID string, rec *executor.Record) RecoveryPlan {
	plan := RecoveryPlan{ExecutorID: executorID}
	if rec != nil {
		plan.SessionID = rec.SessionID
		plan.BaseRef = rec.BaseRef
		plan.RepoKey = rec.RepoKey
		plan.SourcePath = rec.SourcePath
	}

	ws, err := p.layout.WorkspaceDir(executorID)
	wsPresent := err == nil && p.probe.Exists(ws)

	// Tier 3: no durable record, or the workspace/worktree is gone → start over.
	if rec == nil || !wsPresent {
		plan.Action = RecoverFresh
		return plan
	}

	// Tier 1: a bound session AND a resumable argv → --resume in place, full context.
	if strings.TrimSpace(rec.SessionID) != "" {
		if resumeCmd, ok := resumeSessionArgv(rec.RunnerCmd); ok {
			plan.Action = RecoverResume
			plan.Workspace = ws
			plan.RunnerCmd = resumeCmd
			return plan
		}
	}

	// Tier 2: workspace survives but the session cannot be resumed → rerun the
	// persisted argv in the same workspace (progress on disk survives, context lost).
	plan.Action = RecoverRerun
	plan.Workspace = ws
	plan.RunnerCmd = rec.RunnerCmd
	return plan
}
