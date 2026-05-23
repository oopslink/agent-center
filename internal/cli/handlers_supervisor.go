package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oopslink/agent-center/internal/cli/supervisor"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/decision"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// SupervisorRunCommand returns `agent-center supervisor --scope=... --invocation-id=...`.
// The handler runs the per-invocation subprocess loop (PromptAssembler +
// claudecode adapter). Audience = Sys (center-internal spawn target).
//
// Per [04-configuration § 1] this subcommand explicitly does NOT load
// agent-center.yaml — every parameter must come from CLI flags + env.
func SupervisorRunCommand() *Command {
	return &Command{
		Name:    "supervisor",
		Summary: "Run a supervisor invocation (spawned by center, audience=Sys)",
		LongHelp: "Runs one supervisor invocation: assembles a prompt from " +
			"trigger events, spawns the claude CLI, parses stream-json, writes " +
			"usage to a file the parent reads on exit. Audience: Sys (not user-facing).",
		Flags: func(fs *flag.FlagSet) Handler {
			scope := fs.String("scope", "", "scope kind:key (e.g. task:T-1)")
			invID := fs.String("invocation-id", "", "supervisor invocation ULID")
			triggers := fs.String("trigger-events", "", "comma-separated event ULIDs")
			memDir := fs.String("memory-dir", "", "memory dir override (defaults to $AGENT_CENTER_MEMORY_DIR)")
			usageDir := fs.String("usage-dir", "", "usage file dir override (defaults to $AGENT_CENTER_USAGE_DIR)")
			claudeBinary := fs.String("claude-binary", "", "override claude binary path")
			return func(ctx context.Context, _ []string, out, errw io.Writer) ExitCode {
				res, err := supervisor.Run(ctx, supervisor.RunInput{
					Scope:        *scope,
					InvocationID: *invID,
					TriggerCSV:   *triggers,
					MemoryDir:    *memDir,
					UsageDir:     *usageDir,
					ClaudeBinary: *claudeBinary,
				}, out, errw)
				if err != nil {
					return PrintError(errw, "human", "supervisor_run_failed", err.Error(), ExitBusinessError)
				}
				if res.ExitCode != 0 {
					return ExitCode(res.ExitCode)
				}
				return ExitOK
			}
		},
	}
}

// SupervisorRetriggerCommand returns `agent-center supervisor retrigger <invocation_id>`.
func (a *App) SupervisorRetriggerCommand() *Command {
	return &Command{
		Name:    "retrigger",
		Summary: "Restart a failed/timed_out supervisor invocation (audience=U)",
		Flags: func(fs *flag.FlagSet) Handler {
			format := fs.String("format", FormatTable, formatFlagHelp())
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) < 1 {
					return PrintError(errw, *format, "usage_error", "usage: supervisor retrigger <invocation_id>", ExitUsage)
				}
				id := cognition.InvocationID(args[0])
				prev, err := a.InvocationRepo.FindByID(ctx, id)
				if err != nil {
					if errors.Is(err, cognition.ErrInvocationNotFound) {
						return PrintError(errw, *format, "invocation_not_found",
							fmt.Sprintf("invocation %q not found", id), ExitNotFound)
					}
					return PrintError(errw, *format, "internal_error", err.Error(), ExitBusinessError)
				}
				if prev.Status() != cognition.StatusFailed && prev.Status() != cognition.StatusTimedOut {
					return PrintError(errw, *format, "invalid_status",
						"only failed/timed_out invocations can be retriggered; got "+prev.Status().String(),
						ExitInvalidTransition)
				}
				if a.SupervisorSpawner == nil {
					return PrintError(errw, *format, "spawner_not_wired",
						"supervisor spawner not wired in this CLI (server-only path)", ExitNotImplemented)
				}
				newID, err := a.SupervisorSpawner.Spawn(ctx, scheduler.InvocationRequest{
					Scope:         prev.Scope(),
					TriggerEvents: prev.TriggerEvents(),
				})
				if err != nil {
					return PrintError(errw, *format, "spawn_failed", err.Error(), ExitBusinessError)
				}
				// emit supervisor.retriggered
				if a.Sink != nil {
					_ = persistence.RunInTx(ctx, a.DB, func(txCtx context.Context) error {
						_, e := a.Sink.Emit(txCtx, observability.EmitCommand{
							EventType: "supervisor.retriggered",
							Refs:      refsForScope(prev.Scope()),
							Actor:     a.DefaultActor(),
							Payload: map[string]any{
								"prev_invocation_id": string(id),
								"new_invocation_id":  string(newID),
								"scope_kind":         string(prev.Scope().Kind()),
								"scope_key":          prev.Scope().Key(),
								"operator":           string(a.DefaultActor()),
							},
						})
						return e
					})
				}
				if *format == "json" {
					b, _ := json.Marshal(map[string]any{
						"prev_invocation_id": string(id),
						"new_invocation_id":  string(newID),
					})
					writeOut(out, string(b))
				} else {
					fmt.Fprintf(out, "retriggered %s → %s\n", id, newID)
				}
				return ExitOK
			}
		},
	}
}

// RecordDecisionCommand returns `agent-center record-decision`.
// Audience: S (supervisor agent). Only valid kind is `no_op` — concrete
// actions write DecisionRecords through their own handlers.
func (a *App) RecordDecisionCommand() *Command {
	return &Command{
		Name:    "record-decision",
		Summary: "Supervisor records a no_op decision with rationale (audience=S)",
		Flags: func(fs *flag.FlagSet) Handler {
			invID := fs.String("invocation", "", "invocation id (must match $AGENT_CENTER_INVOCATION_ID)")
			kind := fs.String("kind", "no_op", "decision kind (only 'no_op' allowed here)")
			target := fs.String("target", "", "target refs (e.g. task:T-1)")
			rationale := fs.String("rationale", "", "(required) reason for the decision")
			format := fs.String("format", FormatTable, formatFlagHelp())
			return func(ctx context.Context, _ []string, out, errw io.Writer) ExitCode {
				envInvocation := os.Getenv("AGENT_CENTER_INVOCATION_ID")
				if envInvocation == "" {
					return PrintError(errw, *format, "not_supervisor_context",
						"record-decision requires AGENT_CENTER_INVOCATION_ID env (audience=S only)",
						ExitBusinessError)
				}
				if *invID != envInvocation {
					return PrintError(errw, *format, "invocation_mismatch",
						fmt.Sprintf("--invocation=%s does not match env AGENT_CENTER_INVOCATION_ID=%s", *invID, envInvocation),
						ExitUsage)
				}
				if *kind != string(cognition.DecisionNoOp) {
					return PrintError(errw, *format, "kind_not_allowed",
						"only --kind=no_op is allowed via record-decision; concrete actions write their own decision records",
						ExitUsage)
				}
				if strings.TrimSpace(*rationale) == "" {
					return PrintError(errw, *format, "rationale_required",
						"--rationale required (cognition § 4.7)", ExitUsage)
				}
				actor := decision.Actor{Kind: "supervisor", ID: envInvocation, InvocationID: cognition.InvocationID(envInvocation)}
				var did cognition.DecisionID
				err := persistence.RunInTx(ctx, a.DB, func(txCtx context.Context) error {
					d, rerr := a.DecisionRecorder.Record(txCtx, actor, decision.RecordRequest{
						Kind:           cognition.DecisionNoOp,
						TargetRefsJSON: targetJSON(*target),
						Rationale:      *rationale,
						Outcome:        cognition.OutcomeSucceeded,
					})
					did = d
					return rerr
				})
				if err != nil {
					return PrintError(errw, *format, "decision_failed", err.Error(), ExitBusinessError)
				}
				if *format == "json" {
					b, _ := json.Marshal(map[string]string{"decision_id": string(did)})
					writeOut(out, string(b))
				} else {
					fmt.Fprintf(out, "recorded decision %s (kind=no_op)\n", did)
				}
				return ExitOK
			}
		},
	}
}

// EscalateInputRequestCommand returns `agent-center escalate-input-request <input_request_id>`.
// Audience: S (supervisor).
func (a *App) EscalateInputRequestCommand() *Command {
	return &Command{
		Name:    "escalate-input-request",
		Summary: "Supervisor escalates a pending input request via Bridge (audience=S)",
		Flags: func(fs *flag.FlagSet) Handler {
			channel := fs.String("channel", "", "notification channel hint (feishu / dingtalk / ...)")
			rationale := fs.String("rationale", "", "(required) reason for escalation")
			format := fs.String("format", FormatTable, formatFlagHelp())
			return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
				if len(args) < 1 {
					return PrintError(errw, *format, "usage_error", "usage: escalate-input-request <input_request_id>", ExitUsage)
				}
				if strings.TrimSpace(*rationale) == "" {
					return PrintError(errw, *format, "rationale_required", "--rationale required", ExitUsage)
				}
				irID := args[0]
				envInvocation := os.Getenv("AGENT_CENTER_INVOCATION_ID")
				actor := decision.InferActorFromEnv(os.LookupEnv, a.Config.Identity.DefaultUser)
				ir, err := a.IRRepo.FindByID(ctx, taskruntime.InputRequestID(irID))
				if err != nil {
					return PrintError(errw, *format, "input_request_not_found", err.Error(), ExitNotFound)
				}
				// We don't mutate IR status (cognition just emits an event +
				// optionally writes a decision record); v1 IR doesn't carry
				// "escalated" state — Phase 7 may extend it.
				_ = ir
				err = persistence.RunInTx(ctx, a.DB, func(txCtx context.Context) error {
					if a.DecisionRecorder != nil && actor.IsSupervisor() {
						refsJSON, _ := json.Marshal(map[string]any{
							"input_request_id": irID,
							"channel":          *channel,
						})
						if _, rerr := a.DecisionRecorder.Record(txCtx, actor, decision.RecordRequest{
							Kind:           cognition.DecisionEscalateInputRequest,
							TargetRefsJSON: string(refsJSON),
							Rationale:      *rationale,
							Outcome:        cognition.OutcomeSucceeded,
						}); rerr != nil {
							return rerr
						}
					}
					_, e := a.Sink.Emit(txCtx, observability.EmitCommand{
						EventType:     "input_request.escalated",
						Refs:          observability.EventRefs{InputRequestID: irID},
						Actor:         observability.Actor(actor.ActorString()),
						Payload:       map[string]any{
							"input_request_id":    irID,
							"notification_channel": *channel,
							"reason":              "supervisor_escalation",
							"message":             *rationale,
						},
						CorrelationID: envInvocation,
					})
					return e
				})
				if err != nil {
					return PrintError(errw, *format, "escalate_failed", err.Error(), ExitBusinessError)
				}
				if *format == "json" {
					writeOut(out, fmt.Sprintf(`{"input_request_id":"%s","status":"escalated"}`, irID))
				} else {
					fmt.Fprintf(out, "escalated %s\n", irID)
				}
				return ExitOK
			}
		},
	}
}

func targetJSON(target string) string {
	if strings.TrimSpace(target) == "" {
		return "{}"
	}
	// "task:T-1" → {"task_id":"T-1"}
	if kind, key, ok := strings.Cut(target, ":"); ok && kind != "" && key != "" {
		m := map[string]string{}
		switch kind {
		case "task":
			m["task_id"] = key
		case "issue":
			m["issue_id"] = key
		case "execution":
			m["execution_id"] = key
		case "conversation":
			m["conversation_id"] = key
		case "worker":
			m["worker_id"] = key
		case "input_request":
			m["input_request_id"] = key
		default:
			m[kind] = key
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	return fmt.Sprintf(`{"target":%q}`, target)
}

func refsForScope(scope cognition.InvocationScope) observability.EventRefs {
	switch scope.Kind() {
	case cognition.ScopeTask:
		return observability.EventRefs{TaskID: scope.Key()}
	case cognition.ScopeIssue:
		return observability.EventRefs{IssueID: scope.Key()}
	case cognition.ScopeConversation:
		return observability.EventRefs{ConversationID: scope.Key()}
	case cognition.ScopeWorker:
		return observability.EventRefs{WorkerID: scope.Key()}
	}
	return observability.EventRefs{}
}

// requireSupervisorRationale enforces the cognition § 4.7 rule that
// supervisor-issued action CLI calls must carry --rationale. User /
// system callers are exempt because they don't write to decision_records.
//
// Returns nil for non-supervisor callers OR when rationale is set.
func requireSupervisorRationale(rationale string) error {
	if os.Getenv("AGENT_CENTER_INVOCATION_ID") == "" {
		return nil // user / system caller
	}
	if strings.TrimSpace(rationale) == "" {
		return errors.New("--rationale required (supervisor action; cognition § 4.7)")
	}
	return nil
}

// recordSupervisorDecisionInTx writes a DecisionRecord (kind/refs/rationale)
// inside the caller's existing transaction. The caller MUST already be
// running under persistence.RunInTx; user-actor invocations silently no-op.
//
// Per ADR-0014 § 2: state UPDATE + event INSERT + DecisionRecord INSERT
// must all sit in the same tx. The action services (DispatchService /
// KillCoordinator / IssueLifecycleService / MessageWriter / ...) use
// tx-reentrant `persistence.RunInTx` so wrapping the CLI handler in an
// outer tx makes the action service join it (no service API churn,
// Phase 3 commit 421e92d).
func recordSupervisorDecisionInTx(ctx context.Context, app *App, kind cognition.DecisionKind, refsJSON, rationale string) error {
	if app == nil || app.DecisionRecorder == nil {
		return nil
	}
	actor := decision.InferActorFromEnv(os.LookupEnv, app.Config.Identity.DefaultUser)
	if !actor.IsSupervisor() {
		return nil
	}
	_, err := app.DecisionRecorder.Record(ctx, actor, decision.RecordRequest{
		Kind:           kind,
		TargetRefsJSON: refsJSON,
		Rationale:      rationale,
		Outcome:        cognition.OutcomeSucceeded,
	})
	return err
}

// recordSupervisorDecision is the legacy helper that opens its own tx
// before writing the DecisionRecord. Tests + a small number of paths
// that don't have an outer tx still use it. Prefer
// recordSupervisorDecisionInTx for ADR-0014 same-tx semantics.
func recordSupervisorDecision(ctx context.Context, app *App, kind cognition.DecisionKind, refsJSON, rationale string) error {
	if app == nil || app.DecisionRecorder == nil {
		return nil
	}
	return persistence.RunInTx(ctx, app.DB, func(txCtx context.Context) error {
		return recordSupervisorDecisionInTx(txCtx, app, kind, refsJSON, rationale)
	})
}

// runSupervisorActionTx wraps an action service call + DecisionRecord
// write in a single tx. The actionFn receives the tx-bearing ctx; its
// inner persistence.RunInTx will reuse the outer tx (tx-reentrant
// helper). After actionFn succeeds, a DecisionRecord is written (if
// caller is supervisor) in the same tx.
//
// Per ADR-0014 § 2: state UPDATE + event INSERT + DecisionRecord INSERT
// are atomic; any error rolls back all three.
func runSupervisorActionTx(
	ctx context.Context,
	app *App,
	actionFn func(txCtx context.Context) error,
	kind cognition.DecisionKind,
	refsJSON, rationale string,
) error {
	if app == nil {
		return errors.New("runSupervisorActionTx: nil app")
	}
	return persistence.RunInTx(ctx, app.DB, func(txCtx context.Context) error {
		if err := actionFn(txCtx); err != nil {
			return err
		}
		return recordSupervisorDecisionInTx(txCtx, app, kind, refsJSON, rationale)
	})
}

// clockToPersistence is a no-op type alias to keep the imports tidy.
var _ = clock.SystemClock{}
var _ trservice.TaskCreateInput // silence import
