package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/mention"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// MaxUnreadCount caps the Unread query result so the frontend can
// render a "999+" badge without an unbounded scan.
const MaxUnreadCount = 999

// ReadStateService owns read-state writes (MarkSeen) and the unread
// summary read (Unread). Per v2.1-C-2 audit § 2 the MarkSeen invariant
// is only-forward — a stale "seen up to" never regresses the cursor.
type ReadStateService struct {
	db      *sql.DB
	rsRepo  conversation.UserConversationReadStateRepository
	msgRepo conversation.MessageRepository
	sink    *observability.EventSink
	clock   clock.Clock
}

// NewReadStateService constructs the service.
func NewReadStateService(
	db *sql.DB,
	rsRepo conversation.UserConversationReadStateRepository,
	msgRepo conversation.MessageRepository,
	sink *observability.EventSink,
	clk clock.Clock,
) *ReadStateService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ReadStateService{
		db:      db,
		rsRepo:  rsRepo,
		msgRepo: msgRepo,
		sink:    sink,
		clock:   clk,
	}
}

// MarkSeenTrigger 标注一次 mark-seen 的来源，使下游（ack projector）能区分
// PUSH 路径系统自动盖章（delivery）、PULL 路径 agent 主动确认（agent_tool）、
// 人类已读（human）。仅用于事件标注，不改变 only-forward / 乐观锁语义。
type MarkSeenTrigger string

const (
	MarkSeenTriggerHuman     MarkSeenTrigger = "human"
	MarkSeenTriggerDelivery  MarkSeenTrigger = "delivery"
	MarkSeenTriggerAgentTool MarkSeenTrigger = "agent_tool"
)

// MarkSeenCommand asks the service to advance the cursor.
type MarkSeenCommand struct {
	UserID            conversation.IdentityRef
	ConversationID    conversation.ConversationID
	LastSeenMessageID conversation.MessageID
	Actor             observability.Actor
	// Trigger 标注来源（空 → 视为 human）。透传进 conversation.read_state.changed payload。
	Trigger MarkSeenTrigger
}

// triggerOrDefault 返回标注来源，空值落 human（保守：ack projector 只白名单 agent_tool）。
func (c MarkSeenCommand) triggerOrDefault() MarkSeenTrigger {
	if c.Trigger == "" {
		return MarkSeenTriggerHuman
	}
	return c.Trigger
}

// MarkSeenResult reports the post-operation state. Bumped == false
// means the request was a no-op (cursor was already at or past the
// supplied message id).
type MarkSeenResult struct {
	LastSeenMessageID conversation.MessageID
	Version           int
	EventID           observability.EventID
	Bumped            bool
}

// MarkSeen advances the cursor following the only-forward rule.
//
// Steps (audit § 2.1):
//  1. Validate identity refs.
//  2. Confirm the message belongs to the same conversation as the URL
//     path (guards against client-side bugs poisoning read-state across
//     conversations).
//  3. Inside a tx, read existing row → no-op if already past target;
//     otherwise UPSERT + emit conversation.read_state.changed.
func (s *ReadStateService) MarkSeen(ctx context.Context, cmd MarkSeenCommand) (MarkSeenResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return MarkSeenResult{}, err
	}
	if err := cmd.UserID.Validate(); err != nil {
		return MarkSeenResult{}, fmt.Errorf("mark seen: user_id: %w", err)
	}
	if cmd.LastSeenMessageID == "" {
		return MarkSeenResult{}, errors.New("mark seen: last_seen_message_id required")
	}
	if cmd.ConversationID == "" {
		return MarkSeenResult{}, errors.New("mark seen: conversation_id required")
	}
	// Verify the message id is in the named conversation. Read outside
	// the tx — message rows are append-only so a snapshot read is safe.
	m, err := s.msgRepo.FindByID(ctx, cmd.LastSeenMessageID)
	if err != nil {
		return MarkSeenResult{}, err
	}
	if m.ConversationID() != cmd.ConversationID {
		return MarkSeenResult{}, conversation.ErrReadStateMessageNotInConversation
	}
	var res MarkSeenResult
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		existing, err := s.rsRepo.FindByUserAndConv(txCtx, cmd.UserID, cmd.ConversationID)
		if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
			return err
		}
		var previousMsgID conversation.MessageID
		state := &conversation.UserConversationReadState{
			UserID:            cmd.UserID,
			ConversationID:    cmd.ConversationID,
			LastSeenMessageID: cmd.LastSeenMessageID,
			UpdatedAt:         s.clock.Now(),
		}
		if existing != nil {
			// Only-forward guard: ULIDs sort lexically.
			if string(existing.LastSeenMessageID) >= string(cmd.LastSeenMessageID) {
				res = MarkSeenResult{
					LastSeenMessageID: existing.LastSeenMessageID,
					Version:           existing.Version,
					Bumped:            false,
				}
				return nil
			}
			previousMsgID = existing.LastSeenMessageID
			state.Version = existing.Version
		}
		if err := s.rsRepo.Upsert(txCtx, state); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "conversation.read_state.changed",
			Refs: observability.EventRefs{
				ConversationID: string(cmd.ConversationID),
				MessageID:      string(cmd.LastSeenMessageID),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"conversation_id":               string(cmd.ConversationID),
				"user_id":                       string(cmd.UserID),
				"last_seen_message_id":          string(cmd.LastSeenMessageID),
				"previous_last_seen_message_id": string(previousMsgID),
				"trigger":                       string(cmd.triggerOrDefault()),
			},
		})
		if err != nil {
			return err
		}
		res = MarkSeenResult{
			LastSeenMessageID: state.LastSeenMessageID,
			Version:           state.Version,
			EventID:           evID,
			Bumped:            true,
		}
		return nil
	})
	if err != nil {
		return MarkSeenResult{}, err
	}
	return res, nil
}

// UnreadSummary is what the GET endpoint returns.
type UnreadSummary struct {
	LastSeenMessageID conversation.MessageID
	UnreadCount       int
}

// Unread computes the per-(user, conv) summary. Absent row =
// "everything unread" (count of all messages, capped at MaxUnreadCount).
func (s *ReadStateService) Unread(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) (UnreadSummary, error) {
	if err := userID.Validate(); err != nil {
		return UnreadSummary{}, fmt.Errorf("unread: user_id: %w", err)
	}
	var lastSeen conversation.MessageID
	existing, err := s.rsRepo.FindByUserAndConv(ctx, userID, convID)
	if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
		return UnreadSummary{}, err
	}
	if existing != nil {
		lastSeen = existing.LastSeenMessageID
	}
	count, err := s.countUnread(ctx, convID, lastSeen)
	if err != nil {
		return UnreadSummary{}, err
	}
	return UnreadSummary{
		LastSeenMessageID: lastSeen,
		UnreadCount:       count,
	}, nil
}

// countUnread runs SELECT COUNT(*) WHERE conversation_id = ? AND id > ?
// LIMIT (cap+1) so we can detect "cap or more" without scanning the
// entire tail.
func (s *ReadStateService) countUnread(ctx context.Context,
	convID conversation.ConversationID, lastSeen conversation.MessageID,
) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	// Subquery + COUNT(*) gives us the LIMIT semantics we want: we cap
	// the scan at MaxUnreadCount + 1 rows so the answer is either an
	// exact count <= cap, or cap+1 which we translate to "cap+ overflow"
	// (caller returns cap).
	const stmt = `SELECT COUNT(*) FROM (
		SELECT 1 FROM messages
		WHERE conversation_id = ? AND id > ?
		LIMIT ?
	)`
	var n int
	if err := exec.QueryRowContext(ctx, stmt,
		string(convID), string(lastSeen), MaxUnreadCount+1,
	).Scan(&n); err != nil {
		return 0, err
	}
	if n > MaxUnreadCount {
		return MaxUnreadCount, nil
	}
	return n, nil
}

// MentionSummary extends the unread summary with the mention count used by
// the v2.8 #268 badge model.
type MentionSummary struct {
	LastSeenMessageID conversation.MessageID
	UnreadCount       int
	MentionCount      int
}

// UnreadWithMentions computes both unread_count and mention_count for
// (user, conv) in one pass over the read-state row.
//
// mention_count is v2.8 #268 path A (best-effort, no new schema): a bounded
// scan of the UNREAD tail (id > last_seen, capped at MaxUnreadCount+1) for
// @mentions of displayName, using the SAME matcher as the wake projector
// (internal/mention) — so a mention badge counts exactly the messages that
// would wake the user. mention_count <= unread_count by construction (the
// mention set is a subset of the unread set scanned under the same cap).
// An empty displayName yields mention_count 0 (nothing to match). The
// precise-mention model (a write-time projection) is deferred to v2.9.
func (s *ReadStateService) UnreadWithMentions(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID, displayName string,
) (MentionSummary, error) {
	if err := userID.Validate(); err != nil {
		return MentionSummary{}, fmt.Errorf("unread mentions: user_id: %w", err)
	}
	var lastSeen conversation.MessageID
	existing, err := s.rsRepo.FindByUserAndConv(ctx, userID, convID)
	if err != nil && !errors.Is(err, conversation.ErrReadStateNotFound) {
		return MentionSummary{}, err
	}
	if existing != nil {
		lastSeen = existing.LastSeenMessageID
	}
	unread, err := s.countUnread(ctx, convID, lastSeen)
	if err != nil {
		return MentionSummary{}, err
	}
	mentions, err := s.countMentions(ctx, convID, lastSeen, displayName)
	if err != nil {
		return MentionSummary{}, err
	}
	return MentionSummary{
		LastSeenMessageID: lastSeen,
		UnreadCount:       unread,
		MentionCount:      mentions,
	}, nil
}

// countMentions scans the unread tail (id > lastSeen, capped) and counts
// messages whose content @mentions displayName. Bounded by MaxUnreadCount+1
// rows; result capped at MaxUnreadCount so it never exceeds the unread cap.
//
// @all broadcast (per @oopslink): a message that @all-mentions counts as a
// mention for EVERY viewer — but ONLY when its sender is a human (a `user:`
// sender). This mirrors the wake projector's human-only @all gate, so the badge
// counts exactly the broadcasts that would actually wake/notify (no phantom
// mention from an agent's @all). The @all check is independent of displayName,
// so it still counts even for a viewer with no resolvable name.
func (s *ReadStateService) countMentions(ctx context.Context,
	convID conversation.ConversationID, lastSeen conversation.MessageID, displayName string,
) (int, error) {
	name := strings.TrimSpace(displayName)
	exec, _ := persistence.ExecutorFromCtx(ctx, s.db)
	const stmt = `SELECT content, sender_identity_id FROM messages
		WHERE conversation_id = ? AND id > ?
		LIMIT ?`
	rows, err := exec.QueryContext(ctx, stmt,
		string(convID), string(lastSeen), MaxUnreadCount+1)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var content, sender string
		if err := rows.Scan(&content, &sender); err != nil {
			return 0, err
		}
		matched := name != "" && mention.Present(content, name)
		if !matched && strings.HasPrefix(sender, "user:") && mention.MentionsAll(content) {
			matched = true
		}
		if matched {
			n++
			if n >= MaxUnreadCount {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return n, nil
}
