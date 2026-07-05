package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/mention"
	"github.com/oopslink/agent-center/internal/persistence"
)

// MaxUnreadItems caps the get_my_unread digest so the agent reads a bounded
// inbox, not the firehose. (v2.8.1 #278 D PR4b dual-stream.)
const MaxUnreadItems = 100

// perConvUnreadScanCap bounds the unread tail scanned in any single conversation
// (mirrors the read-state countMentions cap idiom).
const perConvUnreadScanCap = 200

// UnreadItem is one unread message directed at the agent — a DM message, or a
// channel @mention of the agent — with enough content to read and reply.
type UnreadItem struct {
	ConversationID   conversation.ConversationID
	ConversationKind conversation.ConversationKind
	ConversationName string
	MessageID        conversation.MessageID
	SenderRef        conversation.IdentityRef
	Content          string
	// v2.10.0 [T74]: the message's attachments (file_uri + filename/mime/size).
	// Surfaced so a human can send a screenshot to an agent and the agent can
	// perceive + download it (download_file) — inbound parity with the outbound
	// post_message path. Empty when the message carries no attachments. The
	// inbox only scans conversations the agent participates in, so every uri
	// here is from a participating conversation (download_file authz fail-closed).
	Attachments []conversation.MessageAttachment
	PostedAt    time.Time
	// ActorKind classifies the sender (human/agent/system) for the I7-D2 reply
	// semantic: a HUMAN directed message must be answered; an AGENT-authored
	// mention is "可回可不回" — reply only if content warrants, else SilentAck
	// (mark_seen). Derived from SenderRef (cognition/04-wake-guardrail.md §3.6).
	ActorKind conversation.MentionActorKind
	// QuotedMessageID (引用) is the raw pointer to the message this one quotes
	// (empty when it quotes nothing). The get_my_unread handler resolves it into a
	// preview so the agent sees WHAT was quoted — inbound parity with the UI, which
	// otherwise leaves an agent blind to a quote a human attached to its @mention.
	QuotedMessageID conversation.MessageID
}

// AgentInboxService answers "what unread messages are directed at this agent?"
// across all its conversations — the read side of v2.8.1 #278 D dual-stream
// (PR4b, the get_my_unread tool). Directed-at = every unread message in a DM the
// agent participates in + every unread @mention of the agent in a channel it
// participates in. Org-scoped; the agent's own messages are excluded; the result
// is bounded (MaxUnreadItems). The precise-mention model (write-time projection)
// stays deferred to v2.9 — this reuses the same content-scan matcher as the wake
// projector + UnreadWithMentions, so the inbox matches exactly what would wake.
type AgentInboxService struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	rsRepo   conversation.UserConversationReadStateRepository
}

// NewAgentInboxService constructs the service.
func NewAgentInboxService(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	rsRepo conversation.UserConversationReadStateRepository,
) *AgentInboxService {
	return &AgentInboxService{db: db, convRepo: convRepo, rsRepo: rsRepo}
}

// ListUnreadForIdentity returns the unread messages directed at the agent in org
// orgID. refs are the agent's identity refs that may appear as a conversation
// participant / read-state cursor / message sender — an agent can be referenced
// by EITHER "agent:<execution-id>" OR "agent:<identity-member-id>" depending on
// the path (the wake projector matches both, env wake_projector.go), so the
// caller passes both. displayName is the agent's @-handle used to match channel
// mentions. DMs surface ALL unread messages; channels surface only @mentions.
// The agent's own messages (sender ∈ refs) are skipped. Bounded by MaxUnreadItems.
func (s *AgentInboxService) ListUnreadForIdentity(
	ctx context.Context, refs []conversation.IdentityRef, orgID, displayName string,
) ([]UnreadItem, error) {
	if len(refs) == 0 {
		return nil, errors.New("list unread: at least one identity ref required")
	}
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, fmt.Errorf("list unread: identity: %w", err)
		}
	}
	if orgID == "" {
		return nil, errors.New("list unread: org id required")
	}
	active := conversation.ConversationActive
	convs, err := s.convRepo.Find(ctx, conversation.ConversationFilter{
		OrganizationID: orgID,
		Status:         &active,
		Limit:          conversation.DefaultConversationLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list unread: find conversations: %w", err)
	}
	out := make([]UnreadItem, 0, 16)
	for _, c := range convs {
		// The ref the agent ACTUALLY participates as in this conversation (so the
		// read-state cursor + own-message exclusion use the matching ref). Skip
		// conversations the agent does not actively participate in.
		selfRef, ok := participantRef(c.Participants(), refs)
		if !ok {
			continue
		}
		// Only DMs (all unread) and channels (@mentions). task/issue conversations
		// flow through the work queue (PR4a), not the message stream.
		kind := c.Kind()
		if kind != conversation.ConversationKindDM && kind != conversation.ConversationKindChannel {
			continue
		}
		var lastSeen conversation.MessageID
		rs, err := s.rsRepo.FindByUserAndConv(ctx, selfRef, c.ID())
		if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
			return nil, fmt.Errorf("list unread: read-state %s: %w", c.ID(), err)
		}
		if rs != nil {
			lastSeen = rs.LastSeenMessageID
		}
		items, err := s.scanUnread(ctx, c, lastSeen, refs, displayName)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= MaxUnreadItems {
			return out[:MaxUnreadItems], nil
		}
	}
	return out, nil
}

// scanUnread reads the unread tail (id > lastSeen, bounded) of one conversation
// and selects the messages directed at the agent: ALL in a DM, only @mentions in
// a channel; never the agent's own messages.
func (s *AgentInboxService) scanUnread(
	ctx context.Context, c *conversation.Conversation, lastSeen conversation.MessageID,
	refs []conversation.IdentityRef, displayName string,
) ([]UnreadItem, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	// v2.10.0 [T74]: also read `attachments` so inbound messages surface their
	// file_uri + metadata to the agent (inbound parity with outbound post_message).
	const stmt = `SELECT id, sender_identity_id, content, attachments, posted_at, quoted_message_id
		FROM messages
		WHERE conversation_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?`
	rows, err := exec.QueryContext(ctx, stmt, string(c.ID()), string(lastSeen), perConvUnreadScanCap)
	if err != nil {
		return nil, fmt.Errorf("list unread: scan %s: %w", c.ID(), err)
	}
	defer rows.Close()
	isChannel := c.Kind() == conversation.ConversationKindChannel
	var items []UnreadItem
	for rows.Next() {
		var id, sender, content, postedAt string
		var attachmentsJSON, quotedMessageID sql.NullString
		if err := rows.Scan(&id, &sender, &content, &attachmentsJSON, &postedAt, &quotedMessageID); err != nil {
			return nil, err
		}
		// Never surface the agent's own messages back to it (sender ∈ any of refs).
		if refContains(refs, conversation.IdentityRef(sender)) {
			continue
		}
		// Channels: only @mentions of the agent. DMs: all messages (direct).
		if isChannel && !mention.Present(content, displayName) {
			continue
		}
		pt, err := time.Parse(time.RFC3339Nano, postedAt)
		if err != nil {
			return nil, fmt.Errorf("list unread: parse posted_at: %w", err)
		}
		atts, err := conversation.UnmarshalAttachmentsJSON(attachmentsJSON.String)
		if err != nil {
			return nil, fmt.Errorf("list unread: parse attachments %s: %w", id, err)
		}
		items = append(items, UnreadItem{
			ConversationID:   c.ID(),
			ConversationKind: c.Kind(),
			ConversationName: c.Name(),
			MessageID:        conversation.MessageID(id),
			SenderRef:        conversation.IdentityRef(sender),
			Content:          content,
			Attachments:      atts,
			PostedAt:         pt,
			ActorKind:        conversation.IdentityRef(sender).ActorKind(),
			QuotedMessageID:  conversation.MessageID(quotedMessageID.String),
		})
	}
	return items, rows.Err()
}

// participantRef returns the IdentityID under which the agent (any of refs) is an
// active (not-left) participant, plus ok=false if it is not a participant. The
// matched ref is the one to key read-state + own-message exclusion on.
func participantRef(parts []conversation.ParticipantElement, refs []conversation.IdentityRef) (conversation.IdentityRef, bool) {
	for _, p := range parts {
		if p.LeftAt != "" {
			continue
		}
		if refContains(refs, p.IdentityID) {
			return p.IdentityID, true
		}
	}
	return "", false
}

// refContains reports whether ref is in refs.
func refContains(refs []conversation.IdentityRef, ref conversation.IdentityRef) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
