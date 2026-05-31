package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
)

// TaskCommands returns the user/supervisor-facing `task` commands.
func (a *App) TaskCommands() []*Command {
	return []*Command{
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
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: dispatch <task_id> --worker=<id>", ExitUsage)
		}
		if *worker == "" {
			return PrintError(errw, *format, "usage_error", "--worker required", ExitUsage)
		}
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
			res, derr := a.DispatchSvc.Dispatch(ctx, dispatchInputFromArgs(args[0], *worker, *agentCLI, *baseBranch, a.DefaultActor()))
			if derr != nil {
				return HandleDomainError(errw, *format, derr)
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
		execID := taskruntime.TaskExecutionID(args[0])
		killReason := killReasonFromString(*reason)
		if a.Client != nil {
			if _, err := a.Client.KillRequest(ctx, KillExecutionRequest{
				ExecutionID: string(execID),
				Reason:      string(killReason),
				Message:     *message,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if err := a.KillCoordinator.RequestKill(ctx, execID, killReason, *message, a.DefaultActor()); err != nil {
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
		// v2.3-1: Client mode now reaches /admin/taskruntime/task/read-context
		// (was ExitNotImplemented in v2.2). Test path with newTestApp still
		// uses the direct service.
		if a.Client != nil {
			raw, err := a.Client.TaskReadContext(ctx, args[0], *recent)
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			writeOut(out, string(raw))
			return ExitOK
		}
		if a.TaskSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"task service not wired", ExitNotImplemented)
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
