package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// MessageCommands returns the `message` subcommand tree. v2 only
// exposes `refs` — message read sits under `conversation read`.
//
// v2.2 Phase B: handlers route through a.Client when configured.
// A transitional fallback to direct CarryOverSvc access remains for
// in-process tests that build an App via newTestApp() with no Client.
func (a *App) MessageCommands() []*Command {
	return []*Command{
		{Name: "refs", Summary: "Find conversations that carry-over reference this message", Flags: a.messageRefsHandler},
	}
}

func (a *App) messageRefsHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "message refs <message_id>", ExitUsage)
		}
		var refs []ConversationMessageReferenceDTO
		if a.Client != nil {
			rs, err := a.Client.CarryOverFindBySourceMsg(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			refs = rs
		} else {
			if a.CarryOverSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"carry-over service not wired", ExitNotImplemented)
			}
			rs, err := a.CarryOverSvc.FindBySourceMsg(ctx, conversation.MessageID(args[0]))
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			refs = refsDomainToDTOs(rs)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(refs))
			for i, r := range refs {
				arr[i] = map[string]any{
					"id":                     r.ID,
					"child_conversation_id":  r.ChildConversationID,
					"source_conversation_id": r.SourceConversationID,
					"source_message_id":      r.SourceMessageID,
					"created_by":             r.CreatedBy,
					"created_at":             r.CreatedAt,
				}
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-32s %s\n", "REF_ID", "CHILD_CONV", "CREATED_BY")
			for _, r := range refs {
				fmt.Fprintf(out, "%-32s %-32s %s\n", r.ID, r.ChildConversationID, r.CreatedBy)
			}
		}
		return ExitOK
	}
}

// refsDomainToDTOs adapts the domain ConversationMessageReference list to
// the Client DTO shape, used by the transitional in-process fallback.
func refsDomainToDTOs(refs []*conversation.ConversationMessageReference) []ConversationMessageReferenceDTO {
	out := make([]ConversationMessageReferenceDTO, len(refs))
	for i, r := range refs {
		out[i] = ConversationMessageReferenceDTO{
			ID:                   r.ID,
			ChildConversationID:  string(r.ChildConversationID),
			SourceConversationID: string(r.SourceConversationID),
			SourceMessageID:      string(r.SourceMessageID),
			CreatedBy:            string(r.CreatedBy),
			CreatedAt:            r.CreatedAt.Format(time.RFC3339Nano),
		}
	}
	return out
}
