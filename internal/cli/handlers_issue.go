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

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// IssueCommands returns the user/supervisor-facing `issue` subcommands.
func (a *App) IssueCommands() []*Command {
	return []*Command{
		{Name: "open", Summary: "Open a new issue (议事 thread)", Flags: a.issueOpenHandler},
		{Name: "comment", Summary: "Write a message on an issue's conversation", Flags: a.issueCommentHandler},
		{Name: "conclude", Summary: "Conclude an issue (with optional task spawn)", Flags: a.issueConcludeHandler},
		{Name: "withdraw", Summary: "Withdraw an issue (terminal)", Flags: a.issueWithdrawHandler},
		{Name: "bind-conversation", Summary: "Bind an issue to a kind=issue Conversation", Flags: a.issueBindConversationHandler},
		{Name: "link-conversation", Summary: "Link an issue to a related conversation (weak)", Flags: a.issueLinkConversationHandler},
	}
}

// OpenIssueCommand returns the agent-facing top-level `open-issue` verb.
// Per conventions § 1: workers / agents cannot fabricate Tasks; the only
// way for an agent to request new work is via Issue.
func (a *App) OpenIssueCommand() *Command {
	return &Command{Name: "open-issue", Summary: "Agent verb: open an issue from a worker context", Flags: a.openIssueAgentHandler}
}

// =============================================================================
// issue open
// =============================================================================

func (a *App) issueOpenHandler(fs *flag.FlagSet) Handler {
	description := fs.String("description", "", "issue description (markdown; >10KB goes to BlobStore in future phases)")
	origin := fs.String("origin", "cli", "issue origin (cli|web_console|supervisor|agent_open_issue|derived_from_conversation)")
	openedBy := fs.String("opened-by", "", "opener identity id (defaults to config default user)")
	channelHint := fs.String("channel", "", "primary channel hint (used for sync-build origins)")
	fromConversation := fs.String("from-conversation", "", "(CV4 derive) source conversation id; switches to derive flow")
	selectMessages := fs.String("select-messages", "", "(CV4 derive) comma-separated source message ids to carry over")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 2 {
			return PrintError(errw, *format, "usage_error",
				"usage: issue open <project_id> <title> [--description=...] [--origin=...] [--from-conversation=...]", ExitUsage)
		}
		projectID := args[0]
		title := strings.Join(args[1:], " ")
		// CV4 derive path: when --from-conversation is set, route through
		// MessageDerivationService (validates source / carry-over).
		if *fromConversation != "" {
			if a.DerivationSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"derivation service not wired", ExitNotImplemented)
			}
			opener := *openedBy
			if opener == "" {
				opener = "user:" + a.Config.Identity.DefaultUser
			}
			msgIDs := parseMessageIDs(*selectMessages)
			res, err := a.DerivationSvc.DeriveIssue(ctx, convservice.DeriveIssueCommand{
				SourceConversationID: conversation.ConversationID(*fromConversation),
				SourceMessageIDs:     msgIDs,
				ProjectID:            projectID,
				Title:                title,
				Description:          *description,
				CreatedBy:            conversation.IdentityRef(opener),
				Actor:                a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			if *format == "json" {
				b, _ := json.Marshal(map[string]any{
					"issue_id":              res.IssueID,
					"conversation_id":       string(res.ChildConversationID),
					"reference_count":       res.ReferenceCount,
					"issue_event_id":        string(res.IssueEventID),
					"carry_over_event_id":   string(res.CarryOverEventID),
				})
				writeOut(out, string(b))
			} else {
				fmt.Fprintf(out, "opened issue %s from conversation %s (conv=%s, refs=%d)\n",
					res.IssueID, *fromConversation, res.ChildConversationID, res.ReferenceCount)
			}
			return ExitOK
		}
		o, err := discussion.ParseOrigin(*origin)
		if err != nil {
			return PrintError(errw, *format, "issue_invalid_origin", err.Error(), ExitUsage)
		}
		if err := requireSupervisorRationale(*rationale); err != nil {
			return PrintError(errw, *format, "rationale_required", err.Error(), ExitUsage)
		}
		opener := *openedBy
		if opener == "" {
			opener = "user:" + a.Config.Identity.DefaultUser
		}
		actor := a.DefaultActor()
		var res *disservice.OpenIssueResult
		err = runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
			r, oerr := a.IssueLifecycleSvc.Open(txCtx, disservice.OpenIssueCommand{
				ProjectID:          projectID,
				Title:              title,
				Description:        *description,
				OpenedByIdentityID: opener,
				Origin:             o,
				PrimaryChannelHint: *channelHint,
				Actor:              actor,
			})
			if oerr != nil {
				return oerr
			}
			res = r
			return nil
		}, cognition.DecisionOpenIssue,
			fmt.Sprintf(`{"project_id":%q,"title":%q}`, projectID, title),
			*rationale)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"issue_id":        string(res.IssueID),
				"conversation_id": string(res.ConversationID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "opened issue %s (conversation %s)\n", res.IssueID, res.ConversationID)
		}
		return ExitOK
	}
}

// =============================================================================
// open-issue (agent verb; conventions § 1 / 03-cli § 8.2)
// =============================================================================

func (a *App) openIssueAgentHandler(fs *flag.FlagSet) Handler {
	description := fs.String("description", "", "issue description")
	openedBy := fs.String("opened-by", "", "agent identity (e.g. agent:session-X)")
	format := fs.String("format", FormatJSON, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 2 {
			return PrintError(errw, *format, "usage_error",
				"usage: open-issue <project_id> <title> [--description=...] [--opened-by=...]", ExitUsage)
		}
		projectID := args[0]
		title := strings.Join(args[1:], " ")
		opener := *openedBy
		if opener == "" {
			opener = "agent:" + a.Config.Identity.DefaultUser
		}
		actor := observability.Actor(opener)
		if err := actor.Validate(); err != nil {
			return PrintError(errw, *format, "usage_error", err.Error(), ExitUsage)
		}
		res, err := a.IssueLifecycleSvc.Open(ctx, disservice.OpenIssueCommand{
			ProjectID:          projectID,
			Title:              title,
			Description:        *description,
			OpenedByIdentityID: opener,
			Origin:             discussion.OriginAgentOpenIssue,
			Actor:              actor,
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		b, _ := json.Marshal(map[string]any{
			"issue_id":        string(res.IssueID),
			"conversation_id": string(res.ConversationID),
		})
		writeOut(out, string(b))
		return ExitOK
	}
}

// =============================================================================
// issue comment
// =============================================================================

func (a *App) issueCommentHandler(fs *flag.FlagSet) Handler {
	content := fs.String("content", "", "message content")
	kind := fs.String("kind", "text", "content kind (text|system|agent_finding|supervisor_summary|conclusion_draft|task_proposal)")
	actorFlag := fs.String("actor", "", "actor identity (defaults to config default user)")
	direction := fs.String("direction", "internal", "message direction (inbound|outbound|internal)")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: issue comment <issue_id> --content=...", ExitUsage)
		}
		if *content == "" {
			return PrintError(errw, *format, "usage_error", "--content required", ExitUsage)
		}
		if err := requireSupervisorRationale(*rationale); err != nil {
			return PrintError(errw, *format, "rationale_required", err.Error(), ExitUsage)
		}
		actor := observability.Actor(*actorFlag)
		if *actorFlag == "" {
			actor = a.DefaultActor()
		}
		sender := conversation.IdentityRef(*actorFlag)
		if *actorFlag == "" {
			sender = conversation.IdentityRef("user:" + a.Config.Identity.DefaultUser)
		}
		var res *disservice.CommentResult
		err := runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
			r, cerr := a.IssueCommentSvc.Comment(txCtx, disservice.CommentInput{
				IssueID:          discussion.IssueID(args[0]),
				Content:          *content,
				ContentKind:      conversation.MessageContentKind(*kind),
				SenderIdentityID: sender,
				Direction:        conversation.MessageDirection(*direction),
				Actor:            actor,
			})
			if cerr != nil {
				return cerr
			}
			res = r
			return nil
		}, cognition.DecisionIssueComment,
			fmt.Sprintf(`{"issue_id":%q}`, args[0]),
			*rationale)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"message_id": string(res.MessageID)})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "comment added: message %s\n", res.MessageID)
		}
		return ExitOK
	}
}

// =============================================================================
// issue conclude
// =============================================================================

func (a *App) issueConcludeHandler(fs *flag.FlagSet) Handler {
	resolutionFlag := fs.String("resolution", "", "resolution kind (closed_no_action|closed_with_tasks|withdrawn)")
	summary := fs.String("summary", "", "conclusion summary (free text; required)")
	spawnTasks := fs.String("spawn-tasks", "", "tasks JSON (inline or @path/to/file); required for closed_with_tasks")
	concludedBy := fs.String("concluded-by", "", "concluded_by identity (defaults to config default user)")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"usage: issue conclude <issue_id> --resolution=... --summary=... [--spawn-tasks=<inline|@path>]", ExitUsage)
		}
		if *resolutionFlag == "" {
			return PrintError(errw, *format, "usage_error", "--resolution required", ExitUsage)
		}
		if *summary == "" {
			return PrintError(errw, *format, "usage_error", "--summary required", ExitUsage)
		}
		if err := requireSupervisorRationale(*rationale); err != nil {
			return PrintError(errw, *format, "rationale_required", err.Error(), ExitUsage)
		}
		kind := discussion.ResolutionKind(*resolutionFlag)
		if !kind.IsValid() {
			return PrintError(errw, *format, "issue_invalid_resolution",
				fmt.Sprintf("unknown resolution %q", *resolutionFlag), ExitUsage)
		}
		var tasks []dispatch.IssueConcludeTaskSpec
		if kind == discussion.ResolutionClosedWithTasks {
			if *spawnTasks == "" {
				return PrintError(errw, *format, "usage_error",
					"--spawn-tasks required when --resolution=closed_with_tasks", ExitUsage)
			}
			parsed, err := parseSpawnTasks(*spawnTasks)
			if err != nil {
				return PrintError(errw, *format, "issue_invalid_spawn_tasks", err.Error(), ExitUsage)
			}
			tasks = parsed
		} else if *spawnTasks != "" {
			return PrintError(errw, *format, "usage_error",
				fmt.Sprintf("--spawn-tasks only valid when --resolution=closed_with_tasks (got %s)", kind), ExitUsage)
		}
		concBy := *concludedBy
		if concBy == "" {
			concBy = "user:" + a.Config.Identity.DefaultUser
		}
		// Pick DecisionKind: withdrawn → close_issue; otherwise → conclude_issue.
		decKind := cognition.DecisionConcludeIssue
		if kind == discussion.ResolutionWithdrawn {
			decKind = cognition.DecisionCloseIssue
		}
		var res *disservice.ConcludeIssueResult
		err := runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
			r, cerr := a.IssueLifecycleSvc.Conclude(txCtx, disservice.ConcludeIssueCommand{
				IssueID:     discussion.IssueID(args[0]),
				Resolution:  discussion.Resolution{Kind: kind, Summary: *summary, Tasks: tasks},
				ConcludedBy: concBy,
				Actor:       a.DefaultActor(),
			})
			if cerr != nil {
				return cerr
			}
			res = r
			return nil
		}, decKind,
			fmt.Sprintf(`{"issue_id":%q,"resolution":%q}`, args[0], kind),
			*rationale)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			ids := make([]string, len(res.TaskIDs))
			for i, t := range res.TaskIDs {
				ids[i] = string(t)
			}
			b, _ := json.Marshal(map[string]any{
				"issue_id":    string(res.IssueID),
				"task_ids":    ids,
				"resolution":  string(kind),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "issue %s → %s (%d tasks spawned)\n", res.IssueID, kind, len(res.TaskIDs))
		}
		return ExitOK
	}
}

// =============================================================================
// issue withdraw
// =============================================================================

func (a *App) issueWithdrawHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "", "withdraw reason (required; conventions § 16)")
	message := fs.String("message", "", "withdraw human-readable message (required; conventions § 16)")
	withdrawnBy := fs.String("withdrawn-by", "", "withdrawn_by identity (defaults to default user)")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "usage: issue withdraw <issue_id> --reason=... --message=...", ExitUsage)
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
		by := *withdrawnBy
		if by == "" {
			by = "user:" + a.Config.Identity.DefaultUser
		}
		err := runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
			_, werr := a.IssueLifecycleSvc.Withdraw(txCtx, disservice.WithdrawIssueCommand{
				IssueID:     discussion.IssueID(args[0]),
				Reason:      *reason,
				Message:     *message,
				WithdrawnBy: by,
				Actor:       a.DefaultActor(),
			})
			return werr
		}, cognition.DecisionCloseIssue,
			fmt.Sprintf(`{"issue_id":%q,"reason":%q}`, args[0], *reason),
			*rationale)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"issue_id": args[0], "status": "withdrawn"})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "issue %s withdrawn\n", args[0])
		}
		return ExitOK
	}
}

// =============================================================================
// issue bind-conversation
// =============================================================================

func (a *App) issueBindConversationHandler(fs *flag.FlagSet) Handler {
	auto := fs.Bool("auto", false, "create a fresh kind=issue Conversation")
	to := fs.String("to", "", "bind to existing conversation_id (must be kind=issue, open, unowned)")
	channel := fs.String("channel", "", "channel hint (web / cli etc.; --auto only)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"usage: issue bind-conversation <issue_id> --auto|--to=<conv_id>", ExitUsage)
		}
		if *auto && *to != "" {
			return PrintError(errw, *format, "usage_error",
				"--auto and --to are mutually exclusive", ExitUsage)
		}
		if !*auto && *to == "" {
			return PrintError(errw, *format, "usage_error",
				"either --auto or --to=<conv_id> required", ExitUsage)
		}
		issueID := discussion.IssueID(args[0])
		actor := a.DefaultActor()
		var convID conversation.ConversationID
		if *auto {
			id, err := a.IssueBindConversationSvc.BindAuto(ctx, disservice.BindAutoInput{
				IssueID: issueID, Channel: *channel, Actor: actor,
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			convID = id
		} else {
			if err := a.IssueBindConversationSvc.BindTo(ctx, disservice.BindToInput{
				IssueID: issueID, ConversationID: conversation.ConversationID(*to), Actor: actor,
			}); err != nil {
				return HandleDomainError(errw, *format, err)
			}
			convID = conversation.ConversationID(*to)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"issue_id":        string(issueID),
				"conversation_id": string(convID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "issue %s bound to conversation %s\n", issueID, convID)
		}
		return ExitOK
	}
}

// =============================================================================
// issue link-conversation
// =============================================================================

func (a *App) issueLinkConversationHandler(fs *flag.FlagSet) Handler {
	convID := fs.String("conversation", "", "related conversation_id (weak link)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error",
				"usage: issue link-conversation <issue_id> --conversation=<conv_id>", ExitUsage)
		}
		if *convID == "" {
			return PrintError(errw, *format, "usage_error", "--conversation required", ExitUsage)
		}
		if err := a.IssueLinkConversationSvc.Link(ctx, disservice.LinkInput{
			IssueID:        discussion.IssueID(args[0]),
			ConversationID: conversation.ConversationID(*convID),
			Actor:          a.DefaultActor(),
		}); err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{"issue_id": args[0], "linked": *convID})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "issue %s linked to conversation %s\n", args[0], *convID)
		}
		return ExitOK
	}
}

// parseSpawnTasks parses the --spawn-tasks input. Accepts:
//   - inline JSON array literal beginning with '['
//   - "@/path/to/file.json" to read from disk
//
// Schema:
//
//	[{"local_id":"a","title":"...","description":"...",
//	  "priority":"medium","requires_worktree":true,
//	  "depends_on":["b","c"]}, ...]
//
// depends_on items are batch-internal local_ids by default; per
// IssueConcludeSpawn dep resolution they're split into LocalIDs vs TaskIDs
// on the spawner side.
func parseSpawnTasks(input string) ([]dispatch.IssueConcludeTaskSpec, error) {
	if input == "" {
		return nil, errors.New("empty spawn-tasks input")
	}
	var raw []byte
	if strings.HasPrefix(input, "@") {
		path := strings.TrimPrefix(input, "@")
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read spawn-tasks file %q: %w", path, err)
		}
		raw = b
	} else {
		raw = []byte(input)
	}
	var entries []spawnTaskJSON
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse spawn-tasks JSON: %w", err)
	}
	out := make([]dispatch.IssueConcludeTaskSpec, len(entries))
	for i, e := range entries {
		spec := dispatch.IssueConcludeTaskSpec{
			LocalID:           e.LocalID,
			Title:             e.Title,
			Description:       e.Description,
			RequiresWorktree:  e.RequiresWorktree,
			DependsOnLocalIDs: e.DependsOn,
		}
		if e.Priority != "" {
			p, err := task.ParsePriority(e.Priority)
			if err != nil {
				return nil, fmt.Errorf("task[%s] priority: %w", e.LocalID, err)
			}
			spec.Priority = p
		}
		out[i] = spec
	}
	return out, nil
}

type spawnTaskJSON struct {
	LocalID          string   `json:"local_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Priority         string   `json:"priority"`
	RequiresWorktree bool     `json:"requires_worktree"`
	DependsOn        []string `json:"depends_on"`
}
