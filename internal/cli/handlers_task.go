package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// TaskCommands returns the user/supervisor-facing `task` commands.
func (a *App) TaskCommands() []*Command {
	return []*Command{
		{Name: "create", Summary: "Create a new task", Flags: a.taskCreateHandler},
		{Name: "bind-conversation", Summary: "Bind a task to a conversation", Flags: a.taskBindConversationHandler},
		{Name: "unbind-conversation", Summary: "(v1) reserved — not implemented", Flags: a.taskUnbindConversationHandler},
	}
}

// DispatchCommand returns the `dispatch` top-level command.
func (a *App) DispatchCommand() *Command {
	return &Command{Name: "dispatch", Summary: "Dispatch a task to a worker", Flags: a.dispatchHandler}
}

// KillExecutionCommand returns `kill-execution`.
func (a *App) KillExecutionCommand() *Command {
	return &Command{Name: "kill-execution", Summary: "Kill a running execution", Flags: a.killExecutionHandler}
}

// AgentRuntimeCommands returns the agent-facing commands.
func (a *App) AgentRuntimeCommands() []*Command {
	return []*Command{
		{Name: "request-input", Summary: "Agent requests input (writes IR + blocks)", Flags: a.requestInputHandler},
		{Name: "report-progress", Summary: "Agent reports progress milestone", Flags: a.reportProgressHandler},
		{Name: "report-artifact", Summary: "Agent reports an artifact", Flags: a.reportArtifactHandler},
		{Name: "report-failure", Summary: "Agent reports failure", Flags: a.reportFailureHandler},
		{Name: "read-task-context", Summary: "Agent reads task context", Flags: a.readTaskContextHandler},
	}
}

// =============================================================================
// task create
// =============================================================================

func (a *App) taskCreateHandler(fs *flag.FlagSet) Handler {
	description := fs.String("description", "", "task description")
	parent := fs.String("parent", "", "parent task id")
	fromIssue := fs.String("from-issue", "", "from issue id")
	priority := fs.String("priority", "medium", "task priority (high|medium|low)")
	requiresWorktree := fs.Bool("worktree", true, "requires worktree workspace")
	noConv := fs.Bool("no-conversation", false, "skip conversation creation (b/c/d path)")
	fromConversation := fs.String("from-conversation", "", "(CV4 derive) source conversation id; switches to derive flow")
	selectMessages := fs.String("select-messages", "", "(CV4 derive) comma-separated source message ids to carry over")
	agentInstance := fs.String("agent", "", "(CV4 derive) target AgentInstance id (required for derive)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 2 {
			return PrintError(errw, *format, "usage_error", "usage: task create <project_id> <title> [flags]", ExitUsage)
		}
		projectID := args[0]
		title := strings.Join(args[1:], " ")
		// CV4 derive path.
		if *fromConversation != "" {
			if a.DerivationSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"derivation service not wired", ExitNotImplemented)
			}
			msgIDs := parseMessageIDs(*selectMessages)
			actor := a.DefaultActor()
			res, err := a.DerivationSvc.DeriveTask(ctx, convservice.DeriveTaskCommand{
				SourceConversationID: conversation.ConversationID(*fromConversation),
				SourceMessageIDs:     msgIDs,
				ProjectID:            projectID,
				Title:                title,
				Description:          *description,
				AgentInstanceID:      *agentInstance,
				CreatedBy:            conversation.IdentityRef(actor),
				Actor:                actor,
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			if *format == "json" {
				b, _ := json.Marshal(map[string]any{
					"task_id":             res.TaskID,
					"conversation_id":     string(res.ChildConversationID),
					"reference_count":     res.ReferenceCount,
					"task_event_id":       string(res.TaskEventID),
					"carry_over_event_id": string(res.CarryOverEventID),
				})
				writeOut(out, string(b))
			} else {
				fmt.Fprintf(out, "created task %s from conversation %s (conv=%s, refs=%d)\n",
					res.TaskID, *fromConversation, res.ChildConversationID, res.ReferenceCount)
			}
			return ExitOK
		}
		pr, err := task.ParsePriority(*priority)
		if err != nil {
			return PrintError(errw, *format, "task_invalid_priority", err.Error(), ExitUsage)
		}
		var taskID, convID string
		if a.Client != nil {
			res, cerr := a.Client.TaskCreate(ctx, TaskCreateRequest{
				ProjectID:         projectID,
				Title:             title,
				Description:       *description,
				ParentTaskID:      *parent,
				FromIssueID:       *fromIssue,
				Priority:          string(pr),
				RequiresWorktree:  *requiresWorktree,
				WithConversation:  !*noConv,
				ConversationTitle: title,
			})
			if cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
			taskID, convID = res.TaskID, res.ConversationID
		} else {
			res, cerr := a.TaskSvc.Create(ctx, trservice.TaskCreateInput{
				ProjectID:         projectID,
				Title:             title,
				Description:       *description,
				ParentTaskID:      taskruntime.TaskID(*parent),
				FromIssueID:       *fromIssue,
				Priority:          pr,
				RequiresWorktree:  *requiresWorktree,
				WithConversation:  !*noConv,
				ConversationTitle: title,
				Actor:             a.DefaultActor(),
			})
			if cerr != nil {
				return HandleDomainError(errw, *format, cerr)
			}
			taskID, convID = string(res.TaskID), string(res.ConversationID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"task_id":         taskID,
				"conversation_id": convID,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created task %s (conversation %s)\n", taskID, convID)
		}
		return ExitOK
	}
}

// =============================================================================
// task bind-conversation
// =============================================================================

func (a *App) taskBindConversationHandler(fs *flag.FlagSet) Handler {
	auto := fs.Bool("auto", false, "create a new conversation")
	to := fs.String("to", "", "bind to existing conversation_id")
	channel := fs.String("channel", "", "channel hint (web / cli etc.)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: task bind-conversation <task_id> --auto|--to=<conv_id>", ExitUsage)
		}
		taskID := taskruntime.TaskID(args[0])
		mode := ""
		switch {
		case *auto:
			mode = "auto"
		case *to != "":
			mode = "to"
		default:
			return PrintError(errw, *format, "usage_error", "either --auto or --to=<conv_id> required", ExitUsage)
		}
		var convOut string
		if a.Client != nil {
			res, cerr := a.Client.TaskBindConversation(ctx, TaskBindConversationRequest{
				TaskID:         string(taskID),
				Mode:           mode,
				ExistingConvID: *to,
				ChannelHint:    *channel,
			})
			if cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
			convOut = res.ConversationID
		} else {
			convID, cerr := a.TaskSvc.BindConversation(ctx, trservice.BindConversationInput{
				TaskID:         taskID,
				Mode:           mode,
				ExistingConvID: conversation.ConversationID(*to),
				ChannelHint:    *channel,
				Actor:          a.DefaultActor(),
			})
			if cerr != nil {
				return HandleDomainError(errw, *format, cerr)
			}
			convOut = string(convID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"task_id": string(taskID), "conversation_id": convOut})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "task %s bound to conversation %s\n", taskID, convOut)
		}
		return ExitOK
	}
}

// =============================================================================
// task unbind-conversation (v1: not implemented)
// =============================================================================

func (a *App) taskUnbindConversationHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(_ context.Context, args []string, _ io.Writer, errw io.Writer) ExitCode {
		_ = args
		return PrintError(errw, *format, "not_implemented_v1",
			"task unbind-conversation is reserved for v2 (single-channel binding is permanent in v1)",
			ExitNotImplemented)
	}
}

// =============================================================================
// dispatch
// =============================================================================

func (a *App) dispatchHandler(fs *flag.FlagSet) Handler {
	worker := fs.String("worker", "", "target worker id")
	agentCLI := fs.String("agent-cli", "claude-code", "agent CLI (claude-code|codex|opencode)")
	baseBranch := fs.String("base-branch", "main", "worktree base branch")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: dispatch <task_id> --worker=<id>", ExitUsage)
		}
		if *worker == "" {
			return PrintError(errw, *format, "usage_error", "--worker required", ExitUsage)
		}
		if err := requireSupervisorRationale(*rationale); err != nil {
			return PrintError(errw, *format, "rationale_required", err.Error(), ExitUsage)
		}
		// ADR-0014 § 2: state UPDATE + event INSERT + DecisionRecord INSERT
		// in one tx. DispatchSvc internally calls persistence.RunInTx which
		// is tx-reentrant — it joins this outer tx.
		//
		// v2.2-B mismatch: the Client path skips the local DecisionRecord
		// write because there is no admin endpoint that bundles
		// dispatch + decision record in one tx (the admin
		// `dispatch.Dispatch` handler records its own event but doesn't
		// touch DecisionRepo). Server-mode tests still go through the
		// runSupervisorActionTx wrapper below — Client-mode CLI loses the
		// rationale persistence. Filed for v2.3 follow-up: extend
		// /admin/taskruntime/dispatch/dispatch to accept rationale and
		// internally call DecisionRecorder.Record in the same tx.
		var execIDOut string
		if a.Client != nil {
			res, derr := a.Client.Dispatch(ctx, DispatchRequest{
				TaskID:     args[0],
				WorkerID:   *worker,
				AgentCLI:   *agentCLI,
				BaseBranch: *baseBranch,
			})
			if derr != nil {
				return HandleClientError(errw, *format, derr)
			}
			execIDOut = res.ExecutionID
		} else {
			var res *dispatch.DispatchResult
			err := runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
				r, derr := a.DispatchSvc.Dispatch(txCtx, dispatchInputFromArgs(args[0], *worker, *agentCLI, *baseBranch, a.DefaultActor()))
				if derr != nil {
					return derr
				}
				res = r
				return nil
			}, cognition.DecisionDispatch,
				fmt.Sprintf(`{"task_id":%q,"worker_id":%q}`, args[0], *worker),
				*rationale)
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			execIDOut = string(res.ExecutionID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"execution_id": execIDOut})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "dispatched: execution %s\n", execIDOut)
		}
		return ExitOK
	}
}

// =============================================================================
// kill-execution
// =============================================================================

func (a *App) killExecutionHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "", "killed reason (user_request|supervisor_request|abandon_precondition|suspend_precondition|reconcile_stale|reconcile_unknown|timeout_kill)")
	message := fs.String("message", "", "human-readable message")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: kill-execution <execution_id> --reason --message", ExitUsage)
		}
		if *reason == "" {
			return PrintError(errw, *format, "usage_error", "--reason required (conventions § 16)", ExitUsage)
		}
		if *message == "" {
			return PrintError(errw, *format, "usage_error", "--message required (conventions § 16)", ExitUsage)
		}
		if err := requireSupervisorRationale(*rationale); err != nil {
			return PrintError(errw, *format, "rationale_required", err.Error(), ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		killReason := killReasonFromString(*reason)
		// Pick the right DecisionKind based on kill reason (cognition/01 § 4.4):
		// abandon_precondition → abandon_task; suspend_precondition → suspend_task;
		// everything else → kill_execution.
		//
		// v2.2-B mismatch: as with dispatch above, the Client path skips
		// the local DecisionRecord write because /admin/taskruntime/kill/
		// request doesn't accept rationale + record a decision in the
		// same tx. v2.3 follow-up.
		kind := cognition.DecisionKillExecution
		switch killReason {
		case execution.KilledAbandonPrecondition:
			kind = cognition.DecisionAbandonTask
		case execution.KilledSuspendPrecondition:
			kind = cognition.DecisionSuspendTask
		}
		if a.Client != nil {
			if _, err := a.Client.KillRequest(ctx, KillExecutionRequest{
				ExecutionID: string(execID),
				Reason:      string(killReason),
				Message:     *message,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			refsJSON := fmt.Sprintf(`{"execution_id":%q,"reason":%q}`, execID, *reason)
			err := runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
				return a.KillCoordinator.RequestKill(txCtx, execID, killReason, *message, a.DefaultActor())
			}, kind, refsJSON, *rationale)
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"execution_id": string(execID), "status": "kill_requested"})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "kill requested for execution %s\n", execID)
		}
		return ExitOK
	}
}

// =============================================================================
// request-input  (agent CLI)
// =============================================================================

func (a *App) requestInputHandler(fs *flag.FlagSet) Handler {
	question := fs.String("question", "", "question to ask user/supervisor")
	options := fs.String("options", "", "comma-separated options")
	urgency := fs.String("urgency", "normal", "urgency (normal|urgent)")
	format := fs.String("format", FormatJSON, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: request-input <execution_id> --question=<q>", ExitUsage)
		}
		if *question == "" {
			return PrintError(errw, *format, "usage_error", "--question required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		urg, err := parseUrgency(*urgency)
		if err != nil {
			return PrintError(errw, *format, "input_request_invalid_urgency", err.Error(), ExitUsage)
		}
		var irID, convID string
		if a.Client != nil {
			res, cerr := a.Client.IRCreate(ctx, IRCreateRequest{
				ExecutionID: string(execID),
				Question:    *question,
				Options:     splitNonEmpty(*options, ","),
				Urgency:     string(urg),
			})
			if cerr != nil {
				// ErrNoInputChannel maps to a server-side error envelope;
				// surface the original code string so the caller's UX
				// matches the legacy domain-error path.
				if ce, ok := cerr.(*ClientError); ok && ce.Code == "no_input_channel" {
					return PrintError(errw, *format, "no_input_channel", ce.Message, ExitBusinessError)
				}
				return HandleClientError(errw, *format, cerr)
			}
			irID, convID = res.InputRequestID, res.ConversationID
		} else {
			res, cerr := a.IRSvc.Create(ctx, trservice.CreateInput{
				ExecutionID: execID,
				Question:    *question,
				Options:     splitNonEmpty(*options, ","),
				Urgency:     urg,
				Actor:       observability.Actor("agent:" + string(execID)),
			})
			if cerr != nil {
				if errors.Is(cerr, trservice.ErrNoInputChannel) {
					return PrintError(errw, *format, "no_input_channel", cerr.Error(), ExitBusinessError)
				}
				return HandleDomainError(errw, *format, cerr)
			}
			irID, convID = string(res.InputRequestID), string(res.ConversationID)
		}
		// Phase 2: return immediately with the IR id; the agent CLI gets the
		// IR id so it can poll or be re-invoked later. (Long-poll wait wire is
		// a v2.1 stretch — see v2.1-backlog.md.)
		b, _ := json.Marshal(map[string]any{
			"input_request_id": irID,
			"conversation_id":  convID,
		})
		writeOut(out, string(b))
		return ExitOK
	}
}

// =============================================================================
// report-progress (agent CLI)
// =============================================================================

func (a *App) reportProgressHandler(fs *flag.FlagSet) Handler {
	kind := fs.String("kind", "agent_finding", "progress kind")
	content := fs.String("content", "", "progress content")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: report-progress <execution_id> --content=<text>", ExitUsage)
		}
		if *content == "" {
			return PrintError(errw, *format, "usage_error", "--content required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		if a.Client != nil {
			if _, err := a.Client.ExecReportProgress(ctx, ExecReportProgressRequest{
				ExecutionID: string(execID),
				Kind:        *kind,
				Content:     *content,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if err := a.ExecSvc.ReportProgress(ctx, trservice.ReportProgressInput{
				ExecutionID: execID,
				Kind:        *kind,
				Content:     *content,
				Actor:       observability.Actor("agent:" + string(execID)),
			}); err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		if *format == "json" {
			writeOut(out, `{"status":"ok"}`)
		} else {
			writeOut(out, "progress recorded")
		}
		return ExitOK
	}
}

// =============================================================================
// report-artifact (agent CLI)
// =============================================================================

func (a *App) reportArtifactHandler(fs *flag.FlagSet) Handler {
	kind := fs.String("kind", "", "artifact kind (pr_url|file|report|...)")
	title := fs.String("title", "", "short title")
	blobRef := fs.String("blob-ref", "", "blob ref")
	url := fs.String("url", "", "external url")
	metadata := fs.String("metadata", "{}", "metadata JSON")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: report-artifact <execution_id> --kind --title [--blob-ref|--url|--metadata]", ExitUsage)
		}
		if *kind == "" || *title == "" {
			return PrintError(errw, *format, "usage_error", "--kind and --title required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		var artifactID string
		if a.Client != nil {
			res, cerr := a.Client.ArtifactAppend(ctx, ArtifactAppendRequest{
				ExecutionID:  string(execID),
				Kind:         *kind,
				Title:        *title,
				BlobRef:      *blobRef,
				URL:          *url,
				MetadataJSON: *metadata,
			})
			if cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
			artifactID = res.ArtifactID
		} else {
			res, cerr := a.ArtifactSvc.Append(ctx, trservice.AppendInput{
				ExecutionID:  execID,
				Kind:         *kind,
				Title:        *title,
				BlobRef:      *blobRef,
				URL:          *url,
				MetadataJSON: *metadata,
				Actor:        observability.Actor("agent:" + string(execID)),
			})
			if cerr != nil {
				return HandleDomainError(errw, *format, cerr)
			}
			artifactID = string(res.ArtifactID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"artifact_id": artifactID})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "artifact %s recorded\n", artifactID)
		}
		return ExitOK
	}
}

// =============================================================================
// report-failure (agent CLI)
// =============================================================================

func (a *App) reportFailureHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "", "agent-side reason tag (free-form)")
	message := fs.String("message", "", "human-readable detail")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: report-failure <execution_id> --message=<msg>", ExitUsage)
		}
		if *message == "" {
			return PrintError(errw, *format, "usage_error", "--message required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		if a.Client != nil {
			if _, err := a.Client.ExecReportFailure(ctx, ExecReportFailureRequest{
				ExecutionID: string(execID),
				Reason:      *reason,
				Message:     *message,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if err := a.ExecSvc.ReportFailure(ctx, trservice.ReportFailureInput{
				ExecutionID: execID,
				Reason:      *reason,
				Message:     *message,
				Actor:       observability.Actor("agent:" + string(execID)),
			}); err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		if *format == "json" {
			writeOut(out, `{"status":"failed"}`)
		} else {
			writeOut(out, "execution marked failed")
		}
		return ExitOK
	}
}

// =============================================================================
// read-task-context (agent CLI)
// =============================================================================

func (a *App) readTaskContextHandler(fs *flag.FlagSet) Handler {
	recent := fs.Int("recent-messages", 20, "recent message count")
	format := fs.String("format", FormatJSON, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: read-task-context <task_id>", ExitUsage)
		}
		// v2.2-B mismatch: TaskSvc.ReadContext is the only Service method
		// in the TaskRuntime BC without a 1:1 admin endpoint counterpart
		// (admin/api/taskruntime.go exposes Create + BindConversation but
		// no read-context handler). The Client path therefore returns
		// ExitNotImplemented; Service-mode (server boot + tests) still
		// works. Filed for v2.3 follow-up: add
		// /admin/taskruntime/task/read-context.
		if a.TaskSvc == nil {
			return PrintError(errw, *format, "not_implemented_v22",
				"read-task-context has no admin endpoint yet; run inside server mode "+
					"or wait for the v2.3 /admin/taskruntime/task/read-context endpoint",
				ExitNotImplemented)
		}
		ctxRes, err := a.TaskSvc.ReadContext(ctx, taskruntime.TaskID(args[0]), *recent)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		b, _ := json.Marshal(ctxRes)
		writeOut(out, string(b))
		return ExitOK
	}
}

// splitNonEmpty is shared with handlers_worker.go.
