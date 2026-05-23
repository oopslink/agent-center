package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// ChannelCommands returns the `channel` subcommand tree per ADR-0032 § 6.
func (a *App) ChannelCommands() []*Command {
	return []*Command{
		{Name: "create", Summary: "Create a kind=channel conversation", Flags: a.channelCreateHandler},
		{Name: "list", Summary: "List channels", Flags: a.channelListHandler},
		{Name: "show", Summary: "Show a channel by name", Flags: a.channelShowHandler},
		{Name: "archive", Summary: "Archive a channel (terminal, read-only)", Flags: a.channelArchiveHandler},
	}
}

func (a *App) channelCreateHandler(fs *flag.FlagSet) Handler {
	name := fs.String("name", "", "channel name (required, globally unique)")
	description := fs.String("description", "", "channel description")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *name == "" {
			return PrintError(errw, *format, "usage_error", "--name required", ExitUsage)
		}
		if a.ChannelMgmtSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"channel management service not wired", ExitNotImplemented)
		}
		res, err := a.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
			Name:        *name,
			Description: *description,
			CreatedBy:   conversation.IdentityRef(a.DefaultActor()),
			Actor:       a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"conversation_id": string(res.ConversationID),
				"event_id":        string(res.EventID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created channel %q (id=%s)\n", *name, res.ConversationID)
		}
		return ExitOK
	}
}

func (a *App) channelListHandler(fs *flag.FlagSet) Handler {
	statusFlag := fs.String("status", "", "filter by status (active|closed|archived)")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		k := conversation.ConversationKindChannel
		filter := conversation.ConversationFilter{Kind: &k}
		if *statusFlag != "" {
			s := conversation.ConversationStatus(*statusFlag)
			if !s.IsValid() {
				return PrintError(errw, *format, "usage_error",
					"--status must be active|closed|archived", ExitUsage)
			}
			filter.Status = &s
		}
		convs, err := a.ConvRepo.Find(ctx, filter)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(convs))
			for i, c := range convs {
				arr[i] = convToMap(c)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-12s %s\n", "NAME", "STATUS", "DESCRIPTION")
			for _, c := range convs {
				fmt.Fprintf(out, "%-32s %-12s %s\n", c.Name(), c.Status(), c.Description())
			}
		}
		return ExitOK
	}
}

func (a *App) channelShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel show <name>", ExitUsage)
		}
		conv, err := a.ConvRepo.FindByName(ctx, args[0])
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		m := convToMap(conv)
		parts := conv.Participants()
		partArr := make([]map[string]any, len(parts))
		for i, p := range parts {
			partArr[i] = map[string]any{
				"identity_id": string(p.IdentityID),
				"role":        p.Role,
				"joined_at":   p.JoinedAt,
				"joined_by":   string(p.JoinedBy),
				"left_at":     p.LeftAt,
				"left_reason": p.LeftReason,
			}
		}
		m["participants"] = partArr
		if *format == "json" {
			b, _ := json.Marshal(m)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "channel %q\n  id: %s\n  status: %s\n  created_by: %s\n  participants: %d\n  description: %s\n",
				conv.Name(), conv.ID(), conv.Status(), conv.CreatedBy(), len(parts), conv.Description())
		}
		return ExitOK
	}
}

func (a *App) channelArchiveHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel archive <name>", ExitUsage)
		}
		if a.ChannelMgmtSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"channel management service not wired", ExitNotImplemented)
		}
		_, err := a.ChannelMgmtSvc.ArchiveChannel(ctx, convservice.ArchiveChannelCommand{
			Name:       args[0],
			ArchivedBy: conversation.IdentityRef(a.DefaultActor()),
			Actor:      a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("archived channel %s", args[0]))
		return ExitOK
	}
}

// _ ensures errors import doesn't get dropped if all callers vanish.
var _ = errors.New
