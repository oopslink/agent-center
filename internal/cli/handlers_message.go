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
		if a.CarryOverSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"carry-over service not wired", ExitNotImplemented)
		}
		refs, err := a.CarryOverSvc.FindBySourceMsg(ctx, conversation.MessageID(args[0]))
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(refs))
			for i, r := range refs {
				arr[i] = map[string]any{
					"id":                     r.ID,
					"child_conversation_id":  string(r.ChildConversationID),
					"source_conversation_id": string(r.SourceConversationID),
					"source_message_id":      string(r.SourceMessageID),
					"created_by":             string(r.CreatedBy),
					"created_at":             r.CreatedAt.Format(time.RFC3339Nano),
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
