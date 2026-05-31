package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	disservice "github.com/oopslink/agent-center/internal/discussion/service"
)

// IssueCommands returns the user/supervisor-facing `issue` subcommands.
func (a *App) IssueCommands() []*Command {
	return []*Command{
		{Name: "bind-conversation", Summary: "Bind an issue to a kind=issue Conversation", Flags: a.issueBindConversationHandler},
		{Name: "link-conversation", Summary: "Link an issue to a related conversation (weak)", Flags: a.issueLinkConversationHandler},
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
		issueID := args[0]
		var convID string
		if a.Client != nil {
			if *auto {
				res, cerr := a.Client.IssueBindAuto(ctx, IssueBindAutoRequest{
					IssueID: issueID, Channel: *channel,
				})
				if cerr != nil {
					return HandleClientError(errw, *format, cerr)
				}
				convID = res.ConversationID
			} else {
				res, cerr := a.Client.IssueBindTo(ctx, IssueBindToRequest{
					IssueID: issueID, ConversationID: *to,
				})
				if cerr != nil {
					return HandleClientError(errw, *format, cerr)
				}
				convID = res.ConversationID
			}
		} else {
			actor := a.DefaultActor()
			if *auto {
				id, err := a.IssueBindConversationSvc.BindAuto(ctx, disservice.BindAutoInput{
					IssueID: discussion.IssueID(issueID), Channel: *channel, Actor: actor,
				})
				if err != nil {
					return HandleDomainError(errw, *format, err)
				}
				convID = string(id)
			} else {
				if err := a.IssueBindConversationSvc.BindTo(ctx, disservice.BindToInput{
					IssueID: discussion.IssueID(issueID), ConversationID: conversation.ConversationID(*to), Actor: actor,
				}); err != nil {
					return HandleDomainError(errw, *format, err)
				}
				convID = *to
			}
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"issue_id":        issueID,
				"conversation_id": convID,
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
		if a.Client != nil {
			if _, cerr := a.Client.IssueLink(ctx, IssueLinkRequest{
				IssueID:        args[0],
				ConversationID: *convID,
			}); cerr != nil {
				return HandleClientError(errw, *format, cerr)
			}
		} else {
			if err := a.IssueLinkConversationSvc.Link(ctx, disservice.LinkInput{
				IssueID:        discussion.IssueID(args[0]),
				ConversationID: conversation.ConversationID(*convID),
				Actor:          a.DefaultActor(),
			}); err != nil {
				return HandleDomainError(errw, *format, err)
			}
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
