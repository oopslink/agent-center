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
				id := args[0]
				// Look up previous invocation (status + scope + triggers).
				var prevScopeKind, prevScopeKey, prevStatus string
				var prevTriggers []string
				if a.Client != nil {
					inv, err := a.Client.InvocationFindByID(ctx, id)
					if err != nil {
						if ce := new(ClientError); errors.As(err, &ce) && ce.IsNotFound() {
							return PrintError(errw, *format, "invocation_not_found",
								fmt.Sprintf("invocation %q not found", id), ExitNotFound)
						}
						return HandleClientError(errw, *format, err)
					}
					prevScopeKind, prevScopeKey, prevStatus = inv.ScopeKind, inv.ScopeKey, inv.Status
					prevTriggers = inv.TriggerEventIDs
				} else {
					prev, err := a.InvocationRepo.FindByID(ctx, cognition.InvocationID(id))
					if err != nil {
						if errors.Is(err, cognition.ErrInvocationNotFound) {
							return PrintError(errw, *format, "invocation_not_found",
								fmt.Sprintf("invocation %q not found", id), ExitNotFound)
						}
						return PrintError(errw, *format, "internal_error", err.Error(), ExitBusinessError)
					}
					prevScopeKind = string(prev.Scope().Kind())
					prevScopeKey = prev.Scope().Key()
					prevStatus = string(prev.Status())
					triggers := prev.TriggerEvents().IDs()
					prevTriggers = make([]string, len(triggers))
					for i, e := range triggers {
						prevTriggers[i] = string(e)
					}
				}
				if prevStatus != string(cognition.StatusFailed) && prevStatus != string(cognition.StatusTimedOut) {
					return PrintError(errw, *format, "invalid_status",
						"only failed/timed_out invocations can be retriggered; got "+prevStatus,
						ExitInvalidTransition)
				}
				var newID string
				if a.Client != nil {
					res, cerr := a.Client.SupervisorSpawn(ctx, SupervisorSpawnRequest{
						ScopeKind:     prevScopeKind,
						ScopeKey:      prevScopeKey,
						TriggerEvents: prevTriggers,
					})
					if cerr != nil {
						return HandleClientError(errw, *format, cerr)
					}
					newID = res.InvocationID
				} else {
					if a.SupervisorSpawner == nil {
						return PrintError(errw, *format, "spawner_not_wired",
							"supervisor spawner not wired in this CLI (server-only path)", ExitNotImplemented)
					}
					scope, err := cognition.NewInvocationScope(cognition.ScopeKind(prevScopeKind), prevScopeKey)
					if err != nil {
						return PrintError(errw, *format, "invalid_scope", err.Error(), ExitBusinessError)
					}
					eids := make([]observability.EventID, 0, len(prevTriggers))
					for _, t := range prevTriggers {
						if t != "" {
							eids = append(eids, observability.EventID(t))
						}
					}
					triggers, err := cognition.NewTriggerEventSet(eids)
					if err != nil {
						return PrintError(errw, *format, "invalid_triggers", err.Error(), ExitBusinessError)
					}
					nid, err := a.SupervisorSpawner.Spawn(ctx, scheduler.InvocationRequest{
						Scope:         scope,
						TriggerEvents: triggers,
					})
					if err != nil {
						return PrintError(errw, *format, "spawn_failed", err.Error(), ExitBusinessError)
					}
					newID = string(nid)
					// emit supervisor.retriggered (in-process Sink only).
					if a.Sink != nil {
						refs := refsForScopeKindKey(prevScopeKind, prevScopeKey)
						_ = persistence.RunInTx(ctx, a.DB, func(txCtx context.Context) error {
							_, e := a.Sink.Emit(txCtx, observability.EmitCommand{
								EventType: "supervisor.retriggered",
								Refs:      refs,
								Actor:     a.DefaultActor(),
								Payload: map[string]any{
									"prev_invocation_id": id,
									"new_invocation_id":  newID,
									"scope_kind":         prevScopeKind,
									"scope_key":          prevScopeKey,
									"operator":           string(a.DefaultActor()),
								},
							})
							return e
						})
					}
				}
				if *format == "json" {
					b, _ := json.Marshal(map[string]any{
						"prev_invocation_id": id,
						"new_invocation_id":  newID,
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
				var did string
				if a.Client != nil {
					res, cerr := a.Client.DecisionRecord(ctx, DecisionRecordRequest{
						InvocationID:   envInvocation,
						Kind:           string(cognition.DecisionNoOp),
						TargetRefsJSON: targetJSON(*target),
						Rationale:      *rationale,
						Outcome:        string(cognition.OutcomeSucceeded),
					})
					if cerr != nil {
						return HandleClientError(errw, *format, cerr)
					}
					did = res.DecisionID
				} else {
					actor := decision.Actor{Kind: "supervisor", ID: envInvocation, InvocationID: cognition.InvocationID(envInvocation)}
					var domainID cognition.DecisionID
					err := persistence.RunInTx(ctx, a.DB, func(txCtx context.Context) error {
						d, rerr := a.DecisionRecorder.Record(txCtx, actor, decision.RecordRequest{
							Kind:           cognition.DecisionNoOp,
							TargetRefsJSON: targetJSON(*target),
							Rationale:      *rationale,
							Outcome:        cognition.OutcomeSucceeded,
						})
						domainID = d
						return rerr
					})
					if err != nil {
						return PrintError(errw, *format, "decision_failed", err.Error(), ExitBusinessError)
					}
					did = string(domainID)
				}
				if *format == "json" {
					b, _ := json.Marshal(map[string]string{"decision_id": did})
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
		Summary: "Supervisor escalates a pending input request (audience=S)",
		Flags: func(fs *flag.FlagSet) Handler {
			channel := fs.String("channel", "", "notification channel hint (web / cli / ...)")
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
				// In-process path: pre-load the IR (we never mutate it; the
				// original handler did the same as a sanity check). With the
				// Client there is no equivalent admin endpoint for an IR FindByID
				// today, so we skip the check — the underlying admin event-emit
				// path doesn't need IR existence (events are append-only).
				if a.Client == nil {
					if _, err := a.IRRepo.FindByID(ctx, taskruntime.InputRequestID(irID)); err != nil {
						return PrintError(errw, *format, "input_request_not_found", err.Error(), ExitNotFound)
					}
				}
				if a.Client != nil {
					// Supervisor escalation maps to a decision record over the
					// admin endpoint. The corresponding event_emit is performed
					// by the admin server-side decision recorder hook when
					// scheduled (v2.3); v2.2 records the decision only.
					if actor.IsSupervisor() {
						refsJSON, _ := json.Marshal(map[string]any{
							"input_request_id": irID,
							"channel":          *channel,
						})
						_, cerr := a.Client.DecisionRecord(ctx, DecisionRecordRequest{
							InvocationID:   envInvocation,
							Kind:           string(cognition.DecisionEscalateInputRequest),
							TargetRefsJSON: string(refsJSON),
							Rationale:      *rationale,
							Outcome:        string(cognition.OutcomeSucceeded),
						})
						if cerr != nil {
							return HandleClientError(errw, *format, cerr)
						}
					}
				} else {
					err := persistence.RunInTx(ctx, a.DB, func(txCtx context.Context) error {
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
							Payload: map[string]any{
								"input_request_id":     irID,
								"notification_channel": *channel,
								"reason":               "supervisor_escalation",
								"message":              *rationale,
							},
							CorrelationID: envInvocation,
						})
						return e
					})
					if err != nil {
						return PrintError(errw, *format, "escalate_failed", err.Error(), ExitBusinessError)
					}
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
	return refsForScopeKindKey(string(scope.Kind()), scope.Key())
}

func refsForScopeKindKey(kind, key string) observability.EventRefs {
	switch cognition.ScopeKind(kind) {
	case cognition.ScopeTask:
		return observability.EventRefs{TaskID: key}
	case cognition.ScopeIssue:
		return observability.EventRefs{IssueID: key}
	case cognition.ScopeConversation:
		return observability.EventRefs{ConversationID: key}
	case cognition.ScopeWorker:
		return observability.EventRefs{WorkerID: key}
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

// recordSupervisorDecisionViaClient is the Client-mode counterpart to
// recordSupervisorDecision. It posts the DecisionRecord through the
// admin endpoint when the caller is a supervisor; no-op for user /
// system actors.
//
// Note: unlike recordSupervisorDecisionInTx, this helper CANNOT share a
// transaction with the preceding action call (they're separate HTTP
// roundtrips). v2.2 Phase B accepts the looser atomicity because the
// admin endpoint itself wraps each action in its own tx; the
// DecisionRecord lands in a sibling tx milliseconds later. v2.3 will
// re-bundle these via a server-side composite endpoint.
func recordSupervisorDecisionViaClient(ctx context.Context, app *App, kind cognition.DecisionKind, refsJSON, rationale string) error {
	if app == nil || app.Client == nil {
		return nil
	}
	envInvocation := os.Getenv("AGENT_CENTER_INVOCATION_ID")
	if envInvocation == "" {
		return nil // user / system caller — no DecisionRecord required
	}
	actor := decision.InferActorFromEnv(os.LookupEnv, app.Config.Identity.DefaultUser)
	if !actor.IsSupervisor() {
		return nil
	}
	_, err := app.Client.DecisionRecord(ctx, DecisionRecordRequest{
		InvocationID:   envInvocation,
		Kind:           string(kind),
		TargetRefsJSON: refsJSON,
		Rationale:      rationale,
		Outcome:        string(cognition.OutcomeSucceeded),
	})
	return err
}

// clockToPersistence is a no-op type alias to keep the imports tidy.
var _ = clock.SystemClock{}
var _ trservice.TaskCreateInput // silence import
