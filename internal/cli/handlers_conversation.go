package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// ConversationCommands returns the `conversation` subcommand tree.
func (a *App) ConversationCommands() []*Command {
	return []*Command{
		{Name: "open", Summary: "Open a conversation (admin)", Flags: a.convOpenHandler},
		{Name: "add-message", Summary: "Append a message", Flags: a.convAddMessageHandler},
		{Name: "send", Summary: "Send a text message (alias for add-message)", Flags: a.convSendHandler},
		{Name: "list", Summary: "List conversations", Flags: a.convListHandler},
		{Name: "read", Summary: "Read messages", Flags: a.convReadHandler},
		{Name: "tail", Summary: "Tail messages (with -f follow)", Flags: a.convTailHandler},
		{Name: "show", Summary: "Show a conversation by id", Flags: a.convShowHandler},
		{Name: "refs", Summary: "List carry-over references into this conversation", Flags: a.convRefsHandler},
		{Name: "close", Summary: "Close a conversation", Flags: a.convCloseHandler},
	}
}

func (a *App) convOpenHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "kind (dm|channel|adhoc|notification)")
	name := fs.String("name", "", "name (channel kind requires non-empty)")
	description := fs.String("description", "", "description")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		kind := conversation.ConversationKind(*kindStr)
		if !kind.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
		}
		res, err := a.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
			Kind:        kind,
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
			fmt.Fprintf(out, "opened conversation %s\n", res.ConversationID)
		}
		return ExitOK
	}
}

func (a *App) convAddMessageHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "text", "content kind")
	content := fs.String("content", "", "message content")
	dirStr := fs.String("direction", "internal", "direction (inbound|outbound|internal)")
	sender := fs.String("actor", "", "sender identity (defaults to configured user)")
	inputReq := fs.String("input-request-ref", "", "associated input_request id")
	rationale := fs.String("rationale", "", "(supervisor only, required) decision rationale")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation add-message <conversation_id>", ExitUsage)
		}
		ck := conversation.MessageContentKind(*kindStr)
		if !ck.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
		}
		dir := conversation.MessageDirection(*dirStr)
		if !dir.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --direction", ExitUsage)
		}
		if err := requireSupervisorRationale(*rationale); err != nil {
			return PrintError(errw, *format, "rationale_required", err.Error(), ExitUsage)
		}
		senderID := conversation.IdentityRef(*sender)
		if senderID == "" {
			senderID = conversation.IdentityRef(a.DefaultActor())
		}
		var res convservice.AddMessageResult
		err := runSupervisorActionTx(ctx, a, func(txCtx context.Context) error {
			r, aerr := a.MessageWriter.AddMessage(txCtx, convservice.AddMessageCommand{
				ConversationID:   conversation.ConversationID(args[0]),
				SenderIdentityID: senderID,
				ContentKind:      ck,
				Content:          *content,
				Direction:        dir,
				InputRequestRef:  *inputReq,
				Actor:            a.DefaultActor(),
			})
			if aerr != nil {
				return aerr
			}
			res = r
			return nil
		}, cognition.DecisionConversationMessage,
			fmt.Sprintf(`{"conversation_id":%q}`, args[0]),
			*rationale)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"message_id": string(res.MessageID),
				"event_id":   string(res.EventID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "added message %s\n", res.MessageID)
		}
		return ExitOK
	}
}

func (a *App) convListHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "filter by kind")
	statusStr := fs.String("status", "", "filter by status (open|closed)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		filter := conversation.ConversationFilter{}
		if *kindStr != "" {
			k := conversation.ConversationKind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
			}
			filter.Kind = &k
		}
		if *statusStr != "" {
			s := conversation.ConversationStatus(*statusStr)
			if !s.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --status", ExitUsage)
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
			fmt.Fprintf(out, "%-32s %-15s %-8s %s\n", "ID", "KIND", "STATUS", "NAME")
			for _, c := range convs {
				fmt.Fprintf(out, "%-32s %-15s %-8s %s\n", c.ID(), c.Kind(), c.Status(), c.Name())
			}
		}
		return ExitOK
	}
}

func (a *App) convReadHandler(fs *flag.FlagSet) Handler {
	tail := fs.Int("tail", 0, "show last N messages")
	since := fs.String("since", "", "show messages since RFC3339 time")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation read <id>", ExitUsage)
		}
		convID := conversation.ConversationID(args[0])
		filter := conversation.MessageFilter{Tail: *tail}
		if *since != "" {
			t, err := time.Parse(time.RFC3339, *since)
			if err != nil {
				return PrintError(errw, *format, "usage_error",
					"invalid --since (RFC3339 required)", ExitUsage)
			}
			filter.Since = &t
		}
		var msgs []*conversation.Message
		var err error
		if *tail > 0 && *since == "" {
			msgs, err = a.MsgRepo.FindRecent(ctx, convID, *tail)
		} else {
			msgs, err = a.MsgRepo.FindByConversationID(ctx, convID, filter)
		}
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(msgs))
			for i, m := range msgs {
				arr[i] = msgToMap(m)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			for _, m := range msgs {
				fmt.Fprintf(out, "[%s] %s (%s/%s): %s\n",
					m.PostedAt().Format(time.RFC3339), m.SenderIdentityID(),
					m.ContentKind(), m.Direction(), m.Content())
			}
		}
		return ExitOK
	}
}

func (a *App) convCloseHandler(fs *flag.FlagSet) Handler {
	reason := fs.String("reason", "", "close reason")
	message := fs.String("message", "", "close message")
	versionFlag := fs.Int("version", 0, "expected version (CAS)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation close <id>", ExitUsage)
		}
		if *reason == "" || *message == "" {
			return PrintError(errw, *format, "usage_error",
				"--reason and --message both required", ExitUsage)
		}
		if *versionFlag <= 0 {
			return PrintError(errw, *format, "usage_error", "--version required for CAS", ExitUsage)
		}
		_, err := a.MessageWriter.Close(ctx, convservice.CloseCommand{
			ConversationID: conversation.ConversationID(args[0]),
			Version:        *versionFlag,
			Reason:         *reason,
			Message:        *message,
			Actor:          a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		writeOut(out, fmt.Sprintf("closed conversation %s", args[0]))
		return ExitOK
	}
}

func convToMap(c *conversation.Conversation) map[string]any {
	return map[string]any{
		"conversation_id":        string(c.ID()),
		"kind":                   string(c.Kind()),
		"status":                 string(c.Status()),
		"name":                   c.Name(),
		"description":            c.Description(),
		"parent_conversation_id": string(c.ParentConversationID()),
		"created_by":             string(c.CreatedBy()),
		"version":                c.Version(),
	}
}

func msgToMap(m *conversation.Message) map[string]any {
	return map[string]any{
		"message_id":        string(m.ID()),
		"conversation_id":   string(m.ConversationID()),
		"sender":            string(m.SenderIdentityID()),
		"content_kind":      string(m.ContentKind()),
		"direction":         string(m.Direction()),
		"content":           m.Content(),
		"input_request_ref": m.InputRequestRef(),
		"posted_at":         m.PostedAt().Format(time.RFC3339Nano),
	}
}

// convSendHandler is a thin alias for add-message: text/internal by
// default, body taken from positional args.
func (a *App) convSendHandler(fs *flag.FlagSet) Handler {
	dirStr := fs.String("direction", "internal", "direction (inbound|outbound|internal)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 2 {
			return PrintError(errw, *format, "usage_error", "conversation send <conversation_id> <text...>", ExitUsage)
		}
		dir := conversation.MessageDirection(*dirStr)
		if !dir.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --direction", ExitUsage)
		}
		body := strings.Join(args[1:], " ")
		res, err := a.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   conversation.ConversationID(args[0]),
			SenderIdentityID: conversation.IdentityRef(a.DefaultActor()),
			ContentKind:      conversation.MessageContentText,
			Content:          body,
			Direction:        dir,
			Actor:            a.DefaultActor(),
		})
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"message_id": string(res.MessageID),
				"event_id":   string(res.EventID),
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "sent %s\n", res.MessageID)
		}
		return ExitOK
	}
}

// convTailHandler reads messages with optional -f follow polling.
func (a *App) convTailHandler(fs *flag.FlagSet) Handler {
	tail := fs.Int("tail", 10, "show last N messages")
	follow := fs.Bool("f", false, "follow: poll for new messages every --interval")
	intervalSec := fs.Int("interval", 1, "follow poll interval in seconds")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation tail <conversation_id> [-f]", ExitUsage)
		}
		convID := conversation.ConversationID(args[0])
		seen := map[conversation.MessageID]bool{}
		msgs, err := a.MsgRepo.FindRecent(ctx, convID, *tail)
		if err != nil {
			return HandleDomainError(errw, *format, err)
		}
		for _, m := range msgs {
			writeMsg(out, *format, m)
			seen[m.ID()] = true
		}
		if !*follow {
			return ExitOK
		}
		ticker := time.NewTicker(time.Duration(*intervalSec) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ExitOK
			case <-ticker.C:
				newMsgs, err := a.MsgRepo.FindByConversationID(ctx, convID, conversation.MessageFilter{Limit: 200})
				if err != nil {
					continue
				}
				for _, m := range newMsgs {
					if !seen[m.ID()] {
						writeMsg(out, *format, m)
						seen[m.ID()] = true
					}
				}
			}
		}
	}
}

func writeMsg(out io.Writer, format string, m *conversation.Message) {
	if format == "json" {
		b, _ := json.Marshal(msgToMap(m))
		writeOut(out, string(b))
		return
	}
	fmt.Fprintf(out, "[%s] %s (%s/%s): %s\n",
		m.PostedAt().Format(time.RFC3339), m.SenderIdentityID(),
		m.ContentKind(), m.Direction(), m.Content())
}

// convShowHandler returns full details for a conversation id.
func (a *App) convShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation show <id>", ExitUsage)
		}
		conv, err := a.ConvRepo.FindByID(ctx, conversation.ConversationID(args[0]))
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
			fmt.Fprintf(out, "conversation %s\n  kind: %s\n  name: %s\n  status: %s\n  parent: %s\n  participants: %d\n",
				conv.ID(), conv.Kind(), conv.Name(), conv.Status(), conv.ParentConversationID(), len(parts))
		}
		return ExitOK
	}
}

// convRefsHandler shows carry-over references that landed into this
// conversation (child side; reverse lookup via `message refs`).
func (a *App) convRefsHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation refs <conversation_id>", ExitUsage)
		}
		if a.CarryOverSvc == nil {
			return PrintError(errw, *format, "internal_error",
				"carry-over service not wired", ExitNotImplemented)
		}
		refs, err := a.CarryOverSvc.FindByChildConv(ctx, conversation.ConversationID(args[0]))
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
			fmt.Fprintf(out, "%-32s %-32s %s\n", "REF_ID", "SOURCE_CONV", "SOURCE_MSG")
			for _, r := range refs {
				fmt.Fprintf(out, "%-32s %-32s %s\n", r.ID, r.SourceConversationID, r.SourceMessageID)
			}
		}
		return ExitOK
	}
}
