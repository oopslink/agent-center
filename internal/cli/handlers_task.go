package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
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
	format := fs.String("format", "human", "output format (human|json)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 2 {
			return PrintError(errw, *format, "usage_error", "usage: task create <project_id> <title> [flags]", ExitUsage)
		}
		projectID := args[0]
		title := strings.Join(args[1:], " ")
		pr, err := task.ParsePriority(*priority)
		if err != nil {
			return PrintError(errw, *format, "task_invalid_priority", err.Error(), ExitUsage)
		}
		res, err := a.TaskSvc.Create(ctx, trservice.TaskCreateInput{
			ProjectID:        projectID,
			Title:            title,
			Description:      *description,
			ParentTaskID:     taskruntime.TaskID(*parent),
			FromIssueID:      *fromIssue,
			Priority:         pr,
			RequiresWorktree: *requiresWorktree,
			WithConversation: !*noConv,
			ConversationTitle: title,
			Actor:            a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"task_id":         string(res.TaskID),
				"conversation_id": string(res.ConversationID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created task %s (conversation %s)\n", res.TaskID, res.ConversationID)
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
	channel := fs.String("channel", "", "channel hint (feishu / web etc.)")
	format := fs.String("format", "human", "output format (human|json)")
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
		convID, err := a.TaskSvc.BindConversation(ctx, trservice.BindConversationInput{
			TaskID:         taskID,
			Mode:           mode,
			ExistingConvID: conversation.ConversationID(*to),
			ChannelHint:    *channel,
			Actor:          a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"task_id": string(taskID), "conversation_id": string(convID)})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "task %s bound to conversation %s\n", taskID, convID)
		}
		return ExitOK
	}
}

// =============================================================================
// task unbind-conversation (v1: not implemented)
// =============================================================================

func (a *App) taskUnbindConversationHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", "human", "output format (human|json)")
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
	format := fs.String("format", "human", "output format (human|json)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: dispatch <task_id> --worker=<id>", ExitUsage)
		}
		if *worker == "" {
			return PrintError(errw, *format, "usage_error", "--worker required", ExitUsage)
		}
		res, err := a.DispatchSvc.Dispatch(ctx, dispatchInputFromArgs(args[0], *worker, *agentCLI, *baseBranch, a.DefaultActor()))
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"execution_id": string(res.ExecutionID)})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "dispatched: execution %s\n", res.ExecutionID)
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
	format := fs.String("format", "human", "output format (human|json)")
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
		if err := a.KillCoordinator.RequestKill(ctx, execID, killReasonFromString(*reason), *message, a.DefaultActor()); err != nil {
			return HandleDomainError(errw, *format, err)
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
	format := fs.String("format", "json", "output format (human|json)")
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
		res, err := a.IRSvc.Create(ctx, trservice.CreateInput{
			ExecutionID: execID,
			Question:    *question,
			Options:     splitNonEmpty(*options, ","),
			Urgency:     urg,
			Actor:       observability.Actor("agent:" + string(execID)),
		})
		if err != nil {
			if errors.Is(err, trservice.ErrNoInputChannel) {
				return PrintError(errw, *format, "no_input_channel", err.Error(), ExitBusinessError)
			}
			return HandleDomainError(errw, *format, err)
		}
		// Phase 2: return immediately with the IR id; actual blocking wait
		// happens at the daemon-side RPC bridge in Phase 5/7. For now the
		// agent CLI gets the IR id so it can poll or be re-invoked later.
		b, _ := json.Marshal(map[string]any{
			"input_request_id": string(res.InputRequestID),
			"conversation_id":  string(res.ConversationID),
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
	format := fs.String("format", "human", "output format (human|json)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: report-progress <execution_id> --content=<text>", ExitUsage)
		}
		if *content == "" {
			return PrintError(errw, *format, "usage_error", "--content required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		if err := a.ExecSvc.ReportProgress(ctx, trservice.ReportProgressInput{
			ExecutionID: execID,
			Kind:        *kind,
			Content:     *content,
			Actor:       observability.Actor("agent:" + string(execID)),
		}); err != nil {
			return HandleDomainError(errw, *format, err)
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
	format := fs.String("format", "human", "output format (human|json)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: report-artifact <execution_id> --kind --title [--blob-ref|--url|--metadata]", ExitUsage)
		}
		if *kind == "" || *title == "" {
			return PrintError(errw, *format, "usage_error", "--kind and --title required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		res, err := a.ArtifactSvc.Append(ctx, trservice.AppendInput{
			ExecutionID:  execID,
			Kind:         *kind,
			Title:        *title,
			BlobRef:      *blobRef,
			URL:          *url,
			MetadataJSON: *metadata,
			Actor:        observability.Actor("agent:" + string(execID)),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"artifact_id": string(res.ArtifactID)})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "artifact %s recorded\n", res.ArtifactID)
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
	format := fs.String("format", "human", "output format (human|json)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: report-failure <execution_id> --message=<msg>", ExitUsage)
		}
		if *message == "" {
			return PrintError(errw, *format, "usage_error", "--message required", ExitUsage)
		}
		execID := taskruntime.TaskExecutionID(args[0])
		if err := a.ExecSvc.ReportFailure(ctx, trservice.ReportFailureInput{
			ExecutionID: execID,
			Reason:      *reason,
			Message:     *message,
			Actor:       observability.Actor("agent:" + string(execID)),
		}); err != nil {
			return HandleDomainError(errw, *format, err)
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
	format := fs.String("format", "json", "output format (human|json)")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: read-task-context <task_id>", ExitUsage)
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
