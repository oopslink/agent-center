package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
		{Name: "list", Summary: "List conversations", Flags: a.convListHandler},
		{Name: "read", Summary: "Read messages", Flags: a.convReadHandler},
		{Name: "close", Summary: "Close a conversation", Flags: a.convCloseHandler},
	}
}

func (a *App) convOpenHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "kind (dm|group_thread|adhoc|notification)")
	title := fs.String("title", "", "title")
	format := fs.String("format", "human", "")
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		kind := conversation.ConversationKind(*kindStr)
		if !kind.IsValid() {
			return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
		}
		res, err := a.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
			Kind: kind, Title: *title, Actor: a.DefaultActor(),
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
	format := fs.String("format", "human", "")
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
	format := fs.String("format", "human", "")
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
			fmt.Fprintf(out, "%-32s %-15s %-8s %s\n", "ID", "KIND", "STATUS", "TITLE")
			for _, c := range convs {
				fmt.Fprintf(out, "%-32s %-15s %-8s %s\n", c.ID(), c.Kind(), c.Status(), c.Title())
			}
		}
		return ExitOK
	}
}

func (a *App) convReadHandler(fs *flag.FlagSet) Handler {
	tail := fs.Int("tail", 0, "show last N messages")
	since := fs.String("since", "", "show messages since RFC3339 time")
	format := fs.String("format", "human", "")
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
	format := fs.String("format", "human", "")
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
		"conversation_id": string(c.ID()),
		"kind":            string(c.Kind()),
		"status":          string(c.Status()),
		"title":           c.Title(),
		"version":         c.Version(),
	}
}

func msgToMap(m *conversation.Message) map[string]any {
	return map[string]any{
		"message_id":       string(m.ID()),
		"conversation_id":  string(m.ConversationID()),
		"sender":           string(m.SenderIdentityID()),
		"content_kind":     string(m.ContentKind()),
		"direction":        string(m.Direction()),
		"content":          m.Content(),
		"vendor_msg_ref":   m.VendorMsgRef(),
		"input_request_ref": m.InputRequestRef(),
		"posted_at":        m.PostedAt().Format(time.RFC3339Nano),
	}
}
