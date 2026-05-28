package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// ConversationCommands returns the `conversation` subcommand tree.
//
// v2.2 Phase B (per docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md):
// every handler in this file routes through a.Client (admin endpoint)
// when a Client is configured. A transitional fallback to direct
// Service / Repo access remains for the test path that constructs an
// App via newTestApp() without a Client. The Client path skips
// runSupervisorActionTx since DecisionRecord lives server-side and the
// CLI cannot open a DB tx without an open *sql.DB; supervisor-driven
// CLI flows (cognition § 4.7) therefore should configure DecisionRecorder
// on the server, not in the client process.
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
		var convID, eventID string
		if a.Client != nil {
			res, err := a.Client.ConversationOpen(ctx, ConversationOpenRequest{
				Kind:        string(kind),
				Name:        *name,
				Description: *description,
				CreatedBy:   string(a.DefaultActor()),
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			convID, eventID = res.ConversationID, res.EventID
		} else {
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
			convID, eventID = string(res.ConversationID), string(res.EventID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"conversation_id": convID,
				"event_id":        eventID,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "opened conversation %s\n", convID)
		}
		_ = eventID
		return ExitOK
	}
}

func (a *App) convAddMessageHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "text", "content kind")
	content := fs.String("content", "", "message content")
	dirStr := fs.String("direction", "internal", "direction (inbound|outbound|internal)")
	sender := fs.String("actor", "", "sender identity (defaults to configured user)")
	inputReq := fs.String("input-request-ref", "", "associated input_request id")
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
		senderID := *sender
		if senderID == "" {
			senderID = string(a.DefaultActor())
		}
		var msgID, eventID string
		if a.Client != nil {
			res, err := a.Client.MessageAppend(ctx, MsgAppendRequest{
				ConversationID:   args[0],
				SenderIdentityID: senderID,
				ContentKind:      string(ck),
				Content:          *content,
				Direction:        string(dir),
				InputRequestRef:  *inputReq,
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			msgID, eventID = res.MessageID, res.EventID
		} else {
			res, err := a.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
				ConversationID:   conversation.ConversationID(args[0]),
				SenderIdentityID: conversation.IdentityRef(senderID),
				ContentKind:      ck,
				Content:          *content,
				Direction:        dir,
				InputRequestRef:  *inputReq,
				Actor:            a.DefaultActor(),
			})
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			msgID, eventID = string(res.MessageID), string(res.EventID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"message_id": msgID,
				"event_id":   eventID,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "added message %s\n", msgID)
		}
		return ExitOK
	}
}

func (a *App) convListHandler(fs *flag.FlagSet) Handler {
	kindStr := fs.String("kind", "", "filter by kind")
	statusStr := fs.String("status", "", "filter by status (open|closed)")
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if *kindStr != "" {
			k := conversation.ConversationKind(*kindStr)
			if !k.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --kind", ExitUsage)
			}
		}
		if *statusStr != "" {
			s := conversation.ConversationStatus(*statusStr)
			if !s.IsValid() {
				return PrintError(errw, *format, "usage_error", "invalid --status", ExitUsage)
			}
		}
		var convs []ConversationDTO
		if a.Client != nil {
			cs, err := a.Client.ConversationFind(ctx, *kindStr, *statusStr)
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			convs = cs
		} else {
			filter := conversation.ConversationFilter{}
			if *kindStr != "" {
				k := conversation.ConversationKind(*kindStr)
				filter.Kind = &k
			}
			if *statusStr != "" {
				s := conversation.ConversationStatus(*statusStr)
				filter.Status = &s
			}
			cs, err := a.ConvRepo.Find(ctx, filter)
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			convs = convsDomainToDTOs(cs)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(convs))
			for i, c := range convs {
				arr[i] = convDTOToMap(c)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "%-32s %-15s %-8s %s\n", "ID", "KIND", "STATUS", "NAME")
			for _, c := range convs {
				fmt.Fprintf(out, "%-32s %-15s %-8s %s\n", c.ID, c.Kind, c.Status, c.Name)
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
		convID := args[0]
		// --since flag validation (RFC3339).
		if *since != "" {
			if _, err := time.Parse(time.RFC3339, *since); err != nil {
				return PrintError(errw, *format, "usage_error",
					"invalid --since (RFC3339 required)", ExitUsage)
			}
		}
		var msgs []MessageDTO
		if a.Client != nil {
			// v2.3-1: prefer the dedicated find-recent route when --tail
			// alone is set; otherwise fall back to the broader fetch +
			// optional client-side --since filter.
			if *tail > 0 && *since == "" {
				ms, err := a.Client.MessageFindRecent(ctx, convID, *tail)
				if err != nil {
					return HandleClientError(errw, *format, err)
				}
				msgs = ms
			} else {
				ms, err := a.Client.MessageFindByConversationID(ctx, convID)
				if err != nil {
					return HandleClientError(errw, *format, err)
				}
				msgs = filterMessagesClientSide(ms, *tail, *since)
			}
		} else {
			filter := conversation.MessageFilter{Tail: *tail}
			if *since != "" {
				t, _ := time.Parse(time.RFC3339, *since)
				filter.Since = &t
			}
			var ms []*conversation.Message
			var err error
			if *tail > 0 && *since == "" {
				ms, err = a.MsgRepo.FindRecent(ctx, conversation.ConversationID(convID), *tail)
			} else {
				ms, err = a.MsgRepo.FindByConversationID(ctx, conversation.ConversationID(convID), filter)
			}
			if err != nil {
				return HandleDomainError(errw, *format, err)
			}
			msgs = msgsDomainToDTOs(ms)
		}
		if *format == "json" {
			arr := make([]map[string]any, len(msgs))
			for i, m := range msgs {
				arr[i] = msgDTOToMap(m)
			}
			b, _ := json.Marshal(arr)
			writeOut(out, string(b))
		} else {
			for _, m := range msgs {
				fmt.Fprintf(out, "[%s] %s (%s/%s): %s\n",
					m.PostedAt, m.SenderIdentityID,
					m.ContentKind, m.Direction, m.Content)
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
		if a.Client != nil {
			if _, err := a.Client.ConversationClose(ctx, ConversationCloseRequest{
				ConversationID: args[0],
				Version:        *versionFlag,
				Reason:         *reason,
				Message:        *message,
			}); err != nil {
				return HandleClientError(errw, *format, err)
			}
		} else {
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
		}
		writeOut(out, fmt.Sprintf("closed conversation %s", args[0]))
		return ExitOK
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
		var msgID, eventID string
		if a.Client != nil {
			res, err := a.Client.MessageAppend(ctx, MsgAppendRequest{
				ConversationID:   args[0],
				SenderIdentityID: string(a.DefaultActor()),
				ContentKind:      string(conversation.MessageContentText),
				Content:          body,
				Direction:        string(dir),
			})
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			msgID, eventID = res.MessageID, res.EventID
		} else {
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
			msgID, eventID = string(res.MessageID), string(res.EventID)
		}
		if *format == "json" {
			b, _ := json.Marshal(map[string]any{
				"message_id": msgID,
				"event_id":   eventID,
			})
			writeOut(out, string(b))
		} else {
			fmt.Fprintf(out, "sent %s\n", msgID)
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
		convID := args[0]
		seen := map[string]bool{}
		first, err := tailFetch(ctx, a, convID, *tail)
		if err != nil {
			return HandleEitherError(errw, *format, err)
		}
		for _, m := range first {
			writeMsgDTO(out, *format, m)
			seen[m.ID] = true
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
				newMsgs, err := tailFetchAll(ctx, a, convID)
				if err != nil {
					continue
				}
				for _, m := range newMsgs {
					if !seen[m.ID] {
						writeMsgDTO(out, *format, m)
						seen[m.ID] = true
					}
				}
			}
		}
	}
}

// tailFetch returns the last `tail` messages from either Client or
// direct MsgRepo (whichever is wired). v2.3-1: Client path now uses
// the dedicated find-recent endpoint instead of the prior
// find-by-conversation-id + client-side trim hack.
func tailFetch(ctx context.Context, a *App, convID string, tail int) ([]MessageDTO, error) {
	if a.Client != nil {
		return a.Client.MessageFindRecent(ctx, convID, tail)
	}
	ms, err := a.MsgRepo.FindRecent(ctx, conversation.ConversationID(convID), tail)
	if err != nil {
		return nil, err
	}
	return msgsDomainToDTOs(ms), nil
}

// tailFetchAll returns up to 200 most-recent messages — used by the
// follow loop to spot new arrivals against the `seen` set.
func tailFetchAll(ctx context.Context, a *App, convID string) ([]MessageDTO, error) {
	if a.Client != nil {
		return a.Client.MessageFindByConversationID(ctx, convID)
	}
	ms, err := a.MsgRepo.FindByConversationID(ctx, conversation.ConversationID(convID),
		conversation.MessageFilter{Limit: 200})
	if err != nil {
		return nil, err
	}
	return msgsDomainToDTOs(ms), nil
}

// HandleEitherError chooses the right error-mapping helper based on the
// error type. Client errors arrive as *ClientError (HandleClientError);
// everything else routes through HandleDomainError.
func HandleEitherError(w io.Writer, format string, err error) ExitCode {
	var ce *ClientError
	if asClientError(err, &ce) {
		return HandleClientError(w, format, err)
	}
	return HandleDomainError(w, format, err)
}

func writeMsgDTO(out io.Writer, format string, m MessageDTO) {
	if format == "json" {
		b, _ := json.Marshal(msgDTOToMap(m))
		writeOut(out, string(b))
		return
	}
	fmt.Fprintf(out, "[%s] %s (%s/%s): %s\n",
		m.PostedAt, m.SenderIdentityID,
		m.ContentKind, m.Direction, m.Content)
}

// convShowHandler returns full details for a conversation id.
func (a *App) convShowHandler(fs *flag.FlagSet) Handler {
	format := fs.String("format", FormatTable, formatFlagHelp())
	return func(ctx context.Context, args []string, out, errw io.Writer) ExitCode {
		if len(args) < 1 {
			return PrintError(errw, *format, "usage_error", "conversation show <id>", ExitUsage)
		}
		var dto ConversationDTO
		if a.Client != nil {
			c, err := a.Client.ConversationFindByID(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			dto = c
		} else {
			conv, err := a.ConvRepo.FindByID(ctx, conversation.ConversationID(args[0]))
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
			fmt.Fprintf(out, "conversation %s\n  kind: %s\n  name: %s\n  status: %s\n  parent: %s\n  participants: %d\n",
				dto.ID, dto.Kind, dto.Name, dto.Status, dto.ParentConversationID, len(dto.Participants))
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
		var refs []ConversationMessageReferenceDTO
		if a.Client != nil {
			rs, err := a.Client.CarryOverFindByChildConv(ctx, args[0])
			if err != nil {
				return HandleClientError(errw, *format, err)
			}
			refs = rs
		} else {
			if a.CarryOverSvc == nil {
				return PrintError(errw, *format, "internal_error",
					"carry-over service not wired", ExitNotImplemented)
			}
			rs, err := a.CarryOverSvc.FindByChildConv(ctx, conversation.ConversationID(args[0]))
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
			fmt.Fprintf(out, "%-32s %-32s %s\n", "REF_ID", "SOURCE_CONV", "SOURCE_MSG")
			for _, r := range refs {
				fmt.Fprintf(out, "%-32s %-32s %s\n", r.ID, r.SourceConversationID, r.SourceMessageID)
			}
		}
		return ExitOK
	}
}

// =============================================================================
// Domain <-> DTO + projection helpers
// =============================================================================

func convDTOToMap(c ConversationDTO) map[string]any {
	return map[string]any{
		"conversation_id":        c.ID,
		"kind":                   c.Kind,
		"status":                 c.Status,
		"name":                   c.Name,
		"description":            c.Description,
		"parent_conversation_id": c.ParentConversationID,
		"created_by":             c.CreatedBy,
		"version":                c.Version,
	}
}

func msgDTOToMap(m MessageDTO) map[string]any {
	return map[string]any{
		"message_id":        m.ID,
		"conversation_id":   m.ConversationID,
		"sender":            m.SenderIdentityID,
		"content_kind":      m.ContentKind,
		"direction":         m.Direction,
		"content":           m.Content,
		"input_request_ref": m.InputRequestRef,
		"posted_at":         m.PostedAt,
	}
}

func participantDTOToMap(p ParticipantDTO) map[string]any {
	return map[string]any{
		"identity_id": p.IdentityID,
		"role":        p.Role,
		"joined_at":   p.JoinedAt,
		"joined_by":   p.JoinedBy,
		"left_at":     p.LeftAt,
		"left_reason": p.LeftReason,
	}
}

// convToMap is the legacy projection helper preserved here for
// transitional callers (handlers that still build maps from the
// domain Conversation).
func convToMap(c *conversation.Conversation) map[string]any {
	return convDTOToMap(convDomainToDTO(c))
}

// msgToMap is the legacy projection helper for the domain Message.
func msgToMap(m *conversation.Message) map[string]any {
	return msgDTOToMap(msgDomainToDTO(m))
}

func convDomainToDTO(c *conversation.Conversation) ConversationDTO {
	dto := ConversationDTO{
		ID:                   string(c.ID()),
		Kind:                 string(c.Kind()),
		Name:                 c.Name(),
		Description:          c.Description(),
		Status:               string(c.Status()),
		ParentConversationID: string(c.ParentConversationID()),
		CreatedBy:            string(c.CreatedBy()),
		CreatedAt:            c.CreatedAt().Format(time.RFC3339Nano),
		UpdatedAt:            c.UpdatedAt().Format(time.RFC3339Nano),
		Version:              c.Version(),
		Participants:         participantsDomainToDTOs(c.Participants()),
	}
	if a := c.ArchivedAt(); a != nil {
		dto.ArchivedAt = a.Format(time.RFC3339Nano)
		dto.ArchivedBy = string(c.ArchivedBy())
	}
	return dto
}

func convsDomainToDTOs(cs []*conversation.Conversation) []ConversationDTO {
	out := make([]ConversationDTO, len(cs))
	for i, c := range cs {
		out[i] = convDomainToDTO(c)
	}
	return out
}

func msgDomainToDTO(m *conversation.Message) MessageDTO {
	return MessageDTO{
		ID:               string(m.ID()),
		ConversationID:   string(m.ConversationID()),
		SenderIdentityID: string(m.SenderIdentityID()),
		ContentKind:      string(m.ContentKind()),
		Content:          m.Content(),
		Direction:        string(m.Direction()),
		InputRequestRef:  m.InputRequestRef(),
		PostedAt:         m.PostedAt().Format(time.RFC3339Nano),
	}
}

func msgsDomainToDTOs(ms []*conversation.Message) []MessageDTO {
	out := make([]MessageDTO, len(ms))
	for i, m := range ms {
		out[i] = msgDomainToDTO(m)
	}
	return out
}

func participantsDomainToDTOs(parts []conversation.ParticipantElement) []ParticipantDTO {
	out := make([]ParticipantDTO, len(parts))
	for i, p := range parts {
		out[i] = ParticipantDTO{
			IdentityID: string(p.IdentityID),
			Role:       p.Role,
			JoinedAt:   p.JoinedAt,
			JoinedBy:   string(p.JoinedBy),
			LeftAt:     p.LeftAt,
			LeftReason: p.LeftReason,
		}
	}
	return out
}

// filterMessagesClientSide applies --tail / --since against an already
// fetched MessageDTO slice (oldest→newest order from the admin endpoint).
// This compensates for the admin endpoint hard-coding MessageFilter
// {Limit: 200} with no since / tail params (see admin_client_conversation.go
// header note).
func filterMessagesClientSide(ms []MessageDTO, tail int, since string) []MessageDTO {
	out := ms
	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			cutoff := t
			filtered := out[:0]
			for _, m := range out {
				if pt, perr := time.Parse(time.RFC3339Nano, m.PostedAt); perr == nil && !pt.Before(cutoff) {
					filtered = append(filtered, m)
				}
			}
			out = filtered
		}
	}
	if tail > 0 && len(out) > tail {
		out = out[len(out)-tail:]
	}
	return out
}

// asClientError is a tiny wrapper around errors.As.
func asClientError(err error, target **ClientError) bool {
	return errors.As(err, target)
}
