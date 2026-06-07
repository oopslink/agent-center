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
	PostedAt         time.Time
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

// ListUnreadForIdentity returns the unread messages directed at identityRef in
// org orgID. displayName is the agent's @-handle used to match channel mentions.
// DMs surface ALL unread messages; channels surface only @mentions of the agent.
// The agent's own messages are skipped. Bounded by MaxUnreadItems.
func (s *AgentInboxService) ListUnreadForIdentity(
	ctx context.Context, identityRef conversation.IdentityRef, orgID, displayName string,
) ([]UnreadItem, error) {
	if err := identityRef.Validate(); err != nil {
		return nil, fmt.Errorf("list unread: identity: %w", err)
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
		// Only conversations the agent actively participates in.
		if !participantActive(c.Participants(), identityRef) {
			continue
		}
		// Only DMs (all unread) and channels (@mentions). task/issue conversations
		// flow through the work queue (PR4a), not the message stream.
		kind := c.Kind()
		if kind != conversation.ConversationKindDM && kind != conversation.ConversationKindChannel {
			continue
		}
		var lastSeen conversation.MessageID
		rs, err := s.rsRepo.FindByUserAndConv(ctx, identityRef, c.ID())
		if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
			return nil, fmt.Errorf("list unread: read-state %s: %w", c.ID(), err)
		}
		if rs != nil {
			lastSeen = rs.LastSeenMessageID
		}
		items, err := s.scanUnread(ctx, c, lastSeen, identityRef, displayName)
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
	identityRef conversation.IdentityRef, displayName string,
) ([]UnreadItem, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	const stmt = `SELECT id, sender_identity_id, content, posted_at
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
		if err := rows.Scan(&id, &sender, &content, &postedAt); err != nil {
			return nil, err
		}
		// Never surface the agent's own messages back to it.
		if conversation.IdentityRef(sender) == identityRef {
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
		items = append(items, UnreadItem{
			ConversationID:   c.ID(),
			ConversationKind: c.Kind(),
			ConversationName: c.Name(),
			MessageID:        conversation.MessageID(id),
			SenderRef:        conversation.IdentityRef(sender),
			Content:          content,
			PostedAt:         pt,
		})
	}
	return items, rows.Err()
}

// participantActive reports whether ref is an active (not-left) participant.
func participantActive(parts []conversation.ParticipantElement, ref conversation.IdentityRef) bool {
	for _, p := range parts {
		if p.IdentityID == ref && p.LeftAt == "" {
			return true
		}
	}
	return false
}
