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

// perConvObligationScanCap bounds the recent tail scanned per conversation when
// deriving reply obligations (mirrors perConvUnreadScanCap). A conversation with
// more than this many recent messages without an agent reply is still bounded —
// the derivation is a best-effort safety net, not an exhaustive audit.
const perConvObligationScanCap = 200

// MaxObligations caps the per-agent obligation digest (mirrors MaxUnreadItems).
const MaxObligations = 100

// ReplyObligation is one outstanding "directed reply" the agent owes: a
// human-authored directed message (a DM, or an @mention by a human in a
// channel/task/issue/plan) that the agent has PERCEIVED and not yet DISCHARGED
// (no agent message posted to the same conversation after it). It is the runtime
// hardening target of the soft system-prompt contract (T341,
// docs/.../conversation/03-reply-guardrail.md).
type ReplyObligation struct {
	ConversationID   conversation.ConversationID
	ConversationKind conversation.ConversationKind
	ConversationName string
	// TriggerMessageID is the LATEST undischarged human-directed message in the
	// conversation — the one the nudge points the agent at.
	TriggerMessageID conversation.MessageID
	SenderRef        conversation.IdentityRef
	Content          string
	PostedAt         time.Time
	ActorKind        conversation.MentionActorKind
}

// ReplyObligationService answers "what directed replies does this agent still
// owe?" — the detection half of the reply-guardrail (方案 A). It is read-only and
// derives obligations from the existing message log + read-state (derived-first,
// no new table — design §3.1):
//
//   - Directed   = a DM message, or an @mention of the agent in a
//     channel/task/issue/plan conversation it participates in.
//   - Author     = HUMAN or AGENT directed messages BOTH create an obligation
//     (§5-① revised, oopslink). The actor kind is carried on the obligation so
//     the enforcement layer can gate agent-authored nudges through the
//     wake-guardrail (a nudge that the guardrail would drop means "不用回" — the
//     anti-storm gates keep agent↔agent reply nudges from ping-ponging), while
//     human-authored nudges always deliver. System-authored messages never
//     create an obligation.
//   - Perceived  = the agent advanced its read cursor past the message
//     (mark_seen — definitive), OR the message has been deliverable for at least
//     idleGrace (woken-but-not-acked). "Perceived" is the chance-to-see gate; it
//     is NOT discharge.
//   - Discharged = the agent posted ANY message to the SAME conversation after
//     the trigger (id > trigger.id). mark_seen alone NEVER discharges a human
//     obligation (§5-③). A reply to a DIFFERENT destination does not count
//     (§5-④ — the obligation is per-conversation).
type ReplyObligationService struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	rsRepo   conversation.UserConversationReadStateRepository
}

// NewReplyObligationService constructs the service.
func NewReplyObligationService(
	db *sql.DB,
	convRepo conversation.ConversationRepository,
	rsRepo conversation.UserConversationReadStateRepository,
) *ReplyObligationService {
	return &ReplyObligationService{db: db, convRepo: convRepo, rsRepo: rsRepo}
}

// OutstandingForIdentity returns the directed replies the agent currently owes in
// org orgID, one per conversation (the latest undischarged human-directed
// message). refs are the agent's identity refs (execution-id and/or
// identity-member-id — same dual-ref contract as the inbox); displayName is the
// @-handle used to match mentions. ttl bounds how old a trigger may be;
// idleGrace is the chance-to-see window for an unread (not-yet-mark_seen)
// trigger. Bounded by MaxObligations.
func (s *ReplyObligationService) OutstandingForIdentity(
	ctx context.Context, refs []conversation.IdentityRef, orgID, displayName string,
	ttl, idleGrace time.Duration, now time.Time,
) ([]ReplyObligation, error) {
	if len(refs) == 0 {
		return nil, errors.New("outstanding obligations: at least one identity ref required")
	}
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, fmt.Errorf("outstanding obligations: identity: %w", err)
		}
	}
	if orgID == "" {
		return nil, errors.New("outstanding obligations: org id required")
	}
	active := conversation.ConversationActive
	convs, err := s.convRepo.Find(ctx, conversation.ConversationFilter{
		OrganizationID: orgID,
		Status:         &active,
		Limit:          conversation.DefaultConversationLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("outstanding obligations: find conversations: %w", err)
	}
	out := make([]ReplyObligation, 0, 8)
	for _, c := range convs {
		selfRef, ok := participantRef(c.Participants(), refs)
		if !ok {
			continue
		}
		var lastSeen conversation.MessageID
		rs, err := s.rsRepo.FindByUserAndConv(ctx, selfRef, c.ID())
		if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
			return nil, fmt.Errorf("outstanding obligations: read-state %s: %w", c.ID(), err)
		}
		if rs != nil {
			lastSeen = rs.LastSeenMessageID
		}
		ob, ok, err := s.deriveForConversation(ctx, c, lastSeen, refs, displayName, ttl, idleGrace, now)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, ob)
			if len(out) >= MaxObligations {
				return out, nil
			}
		}
	}
	return out, nil
}

// deriveForConversation scans the recent tail of one conversation and returns the
// single outstanding obligation (the latest undischarged + perceived + in-ttl
// human-directed message), or ok=false when the agent owes nothing here.
func (s *ReplyObligationService) deriveForConversation(
	ctx context.Context, c *conversation.Conversation, lastSeen conversation.MessageID,
	refs []conversation.IdentityRef, displayName string, ttl, idleGrace time.Duration, now time.Time,
) (ReplyObligation, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	// Scan the most-recent tail (id DESC, bounded) — newest messages decide both
	// the last agent reply and the latest unanswered human message.
	const stmt = `SELECT id, sender_identity_id, content, posted_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY id DESC
		LIMIT ?`
	rows, err := exec.QueryContext(ctx, stmt, string(c.ID()), perConvObligationScanCap)
	if err != nil {
		return ReplyObligation{}, false, fmt.Errorf("outstanding obligations: scan %s: %w", c.ID(), err)
	}
	defer rows.Close()

	type msg struct {
		id, sender, content, postedAt string
	}
	var msgs []msg
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.id, &m.sender, &m.content, &m.postedAt); err != nil {
			return ReplyObligation{}, false, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return ReplyObligation{}, false, err
	}

	// Pass 1: the latest agent reply id (rows are id DESC, so the first own
	// message wins). Any human message with id ≤ this is already discharged.
	var lastAgentReplyID string
	for _, m := range msgs {
		if refContains(refs, conversation.IdentityRef(m.sender)) {
			lastAgentReplyID = m.id
			break
		}
	}

	// Pass 2: the latest human-directed, undischarged, perceived, in-ttl message.
	// rows are id DESC, so the FIRST qualifying message is the latest.
	isMentionKind := c.Kind() != conversation.ConversationKindDM
	for _, m := range msgs {
		if refContains(refs, conversation.IdentityRef(m.sender)) {
			continue // own message
		}
		// §5-① (revised): HUMAN and AGENT directed messages both create an
		// obligation; only SYSTEM-authored messages are excluded. The actor kind
		// is carried on the obligation so agent-authored nudges can be gated
		// through the wake-guardrail downstream.
		actorKind := conversation.IdentityRef(m.sender).ActorKind()
		if actorKind == conversation.ActorKindSystem {
			continue
		}
		// Channels/task/issue/plan: only @mentions of the agent are directed.
		if isMentionKind && !mention.Present(m.content, displayName) {
			continue
		}
		// §5-③④ discharged if an agent reply followed it IN THIS conversation.
		if lastAgentReplyID != "" && m.id <= lastAgentReplyID {
			continue
		}
		pt, err := time.Parse(time.RFC3339Nano, m.postedAt)
		if err != nil {
			return ReplyObligation{}, false, fmt.Errorf("outstanding obligations: parse posted_at: %w", err)
		}
		// TTL: ignore triggers older than ttl (stale — no longer nudged).
		if now.Sub(pt) > ttl {
			continue
		}
		// Perceived: cursor advanced past it (mark_seen) OR deliverable ≥ idleGrace.
		perceived := (lastSeen != "" && m.id <= string(lastSeen)) || now.Sub(pt) >= idleGrace
		if !perceived {
			continue
		}
		return ReplyObligation{
			ConversationID:   c.ID(),
			ConversationKind: c.Kind(),
			ConversationName: c.Name(),
			TriggerMessageID: conversation.MessageID(m.id),
			SenderRef:        conversation.IdentityRef(m.sender),
			Content:          m.content,
			PostedAt:         pt,
			ActorKind:        actorKind,
		}, true, nil
	}
	return ReplyObligation{}, false, nil
}
