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

// ChannelCommands returns the `channel` subcommand tree per ADR-0032 § 6
// + ADR-0034 (participants).
//
// v2.2 Phase B (per docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md):
// every handler in this file routes through a.Client (admin endpoint)
// when a Client is configured. A transitional fallback to direct
// Service / Repo access remains for the test path that constructs an
// App via newTestApp() without a Client. The `channel leave` handler
// has no admin proxy yet (ParticipantMgmtSvc.Leave lacks a route) so
// it stays on the legacy direct path unconditionally — flagged in
// admin_client_conversation.go file header.
func (a *App) ChannelCommands() []*Command {
	return []*Command{
		{
			Name: "create", Summary: "Create a kind=channel conversation", Flags: a.channelCreateHandler,
			Examples: []string{
				`agent-center channel create --name=alpha --description="planning"`,
				`agent-center channel create --name=ops --format=json`,
			},
		},
		{
			Name: "list", Summary: "List channels", Flags: a.channelListHandler,
			Examples: []string{
				`agent-center channel list`,
				`agent-center channel list --status=active --format=json`,
				`agent-center channel list --format=text | xargs -L1 agent-center channel show`,
			},
		},
		{
			Name: "show", Summary: "Show a channel by name", Flags: a.channelShowHandler,
			Examples: []string{
				`agent-center channel show alpha`,
				`agent-center channel show alpha --format=json`,
			},
		},
		{Name: "archive", Summary: "Archive a channel (terminal, read-only)", Flags: a.channelArchiveHandler},
		{Name: "invite", Summary: "Invite an identity into a channel", Flags: a.channelInviteHandler},
		{Name: "leave", Summary: "Leave a channel as the configured user", Flags: a.channelLeaveHandler},
		{Name: "kick", Summary: "Remove a participant (channel owner only)", Flags: a.channelKickHandler},
		{Name: "participants", Summary: "List participants of a channel", Flags: a.channelParticipantsHandler},
	}
}

func (a *App) channelCreateHandler(fs *flag.FlagSet) Handler {
	name := fs.String("name", "", "channel name (required, globally unique)")
	description := fs.String("description", "", "channel description")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *name == "" {
			return PrintError(errw, *format, "usage_error", "--name required", ExitUsage)
		}
		var convID, eventID string
		if a.Client != nil {
			res, err := a.Client.ChannelCreate(ctx, ChannelCreateRequest{
				Name:        *name,
				Description: *description,
				CreatedBy:   string(a.DefaultActor()),
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			convID, eventID = res.ConversationID, res.EventID
		} else {
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
			convID, eventID = string(res.ConversationID), string(res.EventID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"conversation_id": convID,
				"event_id":        eventID,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "created channel %q (id=%s)\n", *name, convID)
		}
		return ExitOK
	}
}

func (a *App) channelListHandler(fs *flag.FlagSet) Handler {
	statusFlag := fs.String("status", "", "filter by status (active|closed|archived)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *statusFlag != "" {
			s := conversation.ConversationStatus(*statusFlag)
			if !s.IsValid() {
				return PrintError(errw, *format, "usage_error",
					"--status must be active|closed|archived", ExitUsage)
			}
		}
		var convs []ConversationDTO
		if a.Client != nil {
			cs, err := a.Client.ConversationFind(ctx, string(conversation.ConversationKindChannel), *statusFlag)
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			convs = cs
		} else {
			k := conversation.ConversationKindChannel
			filter := conversation.ConversationFilter{Kind: &k}
			if *statusFlag != "" {
				s := conversation.ConversationStatus(*statusFlag)
				filter.Status = &s
			}
			cs, err := a.ConvRepo.Find(ctx, filter)
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			convs = convsDomainToDTOs(cs)
		}
		switch *format {
		case FormatJSON:
			arr := make([]map[string]any, len(convs))
			for i, c := range convs {
				arr[i] = convDTOToMap(c)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		case FormatText:
			ids := make([]string, len(convs))
			for i, c := range convs {
				ids[i] = c.Name
			}
			writeTextLines(out, ids)
		default:
			fmt.Fprintf(out, "%-32s %-12s %s\n", "NAME", "STATUS", "DESCRIPTION")
			for _, c := range convs {
				fmt.Fprintf(out, "%-32s %-12s %s\n", c.Name, c.Status, c.Description)
			}
		}
		return ExitOK
	}
}

func (a *App) channelShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel show <name>", ExitUsage)
		}
		var dto ConversationDTO
		if a.Client != nil {
			c, err := a.Client.ConversationFindByName(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			dto = c
		} else {
			conv, err := a.ConvRepo.FindByName(ctx, args[0])
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			dto = convDomainToDTO(conv)
		}
		m := convDTOToMap(dto)
		partArr := make([]map[string]any, len(dto.Participants))
		for i, p := range dto.Participants {
			partArr[i] = participantDTOToMap(p)
		}
		m["participants"] = partArr
		if *format == "json" {
			b, _ := json.Marshal(m)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "channel %q\n  id: %s\n  status: %s\n  created_by: %s\n  participants: %d\n  description: %s\n",
				dto.Name, dto.ID, dto.Status, dto.CreatedBy, len(dto.Participants), dto.Description)
		}
		return ExitOK
	}
}

func (a *App) channelArchiveHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel archive <name>", ExitUsage)
		}
		if a.Client != nil {
			if _, err := a.Client.ChannelArchive(ctx, ChannelArchiveRequest{
				Name:       args[0],
				ArchivedBy: string(a.DefaultActor()),
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
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
		}
		writeOut(out, fmt.Sprintf("archived channel %s", args[0]))
		return ExitOK
	}
}

func (a *App) channelInviteHandler(fs *flag.FlagSet) Handler {
	channel := fs.String("channel", "", "channel name (required)")
	role := fs.String("role", "member", "role: owner|member|observer")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel invite <identity_id> --channel=<name>", ExitUsage)
		}
		if *channel == "" {
			return PrintError(errw, *format, "usage_error", "--channel required", ExitUsage)
		}
		if a.Client != nil {
			if _, err := a.Client.ParticipantInvite(ctx, ParticipantInviteRequest{
				ConversationName: *channel,
				IdentityID:       args[0],
				Role:             *role,
				InvitedBy:        string(a.DefaultActor()),
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if a.ParticipantMgmtSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"participant management service not wired", ExitNotImplemented)
			}
			_, err := a.ParticipantMgmtSvc.Invite(ctx, convservice.InviteCommand{
				ConversationName: *channel,
				IdentityID:       conversation.IdentityRef(args[0]),
				Role:             *role,
				InvitedBy:        conversation.IdentityRef(a.DefaultActor()),
				Actor:            a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		writeOut(out, fmt.Sprintf("invited %s into %s (role=%s)", args[0], *channel, *role))
		return ExitOK
	}
}

// channelLeaveHandler — v2.3-1 switched to the dual-mode pattern after
// `POST /admin/conversation/participant/leave` landed. Test-path apps
// (newTestApp without a Client) still use the direct service.
func (a *App) channelLeaveHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "", "reason for leaving (optional)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel leave <name>", ExitUsage)
		}
		if a.Client != nil {
			if _, err := a.Client.ParticipantLeave(ctx, ParticipantLeaveRequest{
				ConversationName: args[0],
				IdentityID:       string(a.DefaultActor()),
				Reason:           *reason,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if a.ParticipantMgmtSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"participant management service not wired", ExitNotImplemented)
			}
			_, err := a.ParticipantMgmtSvc.Leave(ctx, convservice.LeaveCommand{
				ConversationName: args[0],
				IdentityID:       conversation.IdentityRef(a.DefaultActor()),
				Reason:           *reason,
				Actor:            a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		writeOut(out, fmt.Sprintf("left %s", args[0]))
		return ExitOK
	}
}

func (a *App) channelKickHandler(fs *flag.FlagSet) Handler {
	channel := fs.String("channel", "", "channel name (required)")
	reason := fs.String("reason", "", "reason for kick (optional)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel kick <identity_id> --channel=<name>", ExitUsage)
		}
		if *channel == "" {
			return PrintError(errw, *format, "usage_error", "--channel required", ExitUsage)
		}
		if a.Client != nil {
			if _, err := a.Client.ParticipantKick(ctx, ParticipantKickRequest{
				ConversationName: *channel,
				IdentityID:       args[0],
				KickedBy:         string(a.DefaultActor()),
				Reason:           *reason,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
			if a.ParticipantMgmtSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"participant management service not wired", ExitNotImplemented)
			}
			_, err := a.ParticipantMgmtSvc.Kick(ctx, convservice.KickCommand{
				ConversationName: *channel,
				IdentityID:       conversation.IdentityRef(args[0]),
				KickedBy:         conversation.IdentityRef(a.DefaultActor()),
				Reason:           *reason,
				Actor:            a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
		}
		writeOut(out, fmt.Sprintf("kicked %s from %s", args[0], *channel))
		return ExitOK
	}
}

func (a *App) channelParticipantsHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "channel participants <name>", ExitUsage)
		}
		var parts []ParticipantDTO
		if a.Client != nil {
			c, err := a.Client.ConversationFindByName(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			parts = c.Participants
		} else {
			conv, err := a.ConvRepo.FindByName(ctx, args[0])
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			parts = participantsDomainToDTOs(conv.Participants())
		}
		if *format == "json" {
			arr := make([]map[string]any, len(parts))
			for i, p := range parts {
				arr[i] = participantDTOToMap(p)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-24s %-10s %-26s %s\n", "IDENTITY", "ROLE", "JOINED_AT", "STATE")
			for _, p := range parts {
				state := "active"
				if p.LeftReason != "" {
					state = "left:" + p.LeftReason
				}
				fmt.Fprintf(out, "%-24s %-10s %-26v %s\n", p.IdentityID, p.Role, p.JoinedAt, state)
			}
		}
		return ExitOK
	}
}

// _ ensures errors import doesn't get dropped if all callers vanish.
var _ = errors.New
