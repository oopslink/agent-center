package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

// MessageRepo implements conversation.MessageRepository.
type MessageRepo struct {
	db *sql.DB
}

// NewMessageRepo constructs the repo.
func NewMessageRepo(db *sql.DB) *MessageRepo {
	return &MessageRepo{db: db}
}

// Append inserts a message row. (channel,vendor_msg_ref) collision →
// ErrMessageDuplicate.
func (r *MessageRepo) Append(ctx context.Context, m *conversation.Message) error {
	if m == nil {
		return errors.New("message repo: nil message")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO messages (
		id, conversation_id, sender_identity_id, content_kind, content,
		direction, vendor_msg_ref, input_request_ref, posted_at, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		string(m.ID()),
		string(m.ConversationID()),
		string(m.SenderIdentityID()),
		string(m.ContentKind()),
		m.Content(),
		string(m.Direction()),
		nullString(m.VendorMsgRef()),
		nullString(m.InputRequestRef()),
		m.PostedAt().Format(time.RFC3339Nano),
		m.CreatedAt().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return conversation.ErrMessageDuplicate
		}
		return err
	}
	return nil
}

// FindByID returns a message; ErrMessageNotFound if absent.
func (r *MessageRepo) FindByID(ctx context.Context, id conversation.MessageID) (*conversation.Message, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, messageSelect+` WHERE id = ?`, string(id))
	m, err := scanMessage(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrMessageNotFound
	}
	return m, err
}

// FindByConversationID returns messages in a conversation; filter supports
// Since cutoff + Limit + Tail.
func (r *MessageRepo) FindByConversationID(ctx context.Context, conversationID conversation.ConversationID, filter conversation.MessageFilter) ([]*conversation.Message, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := messageSelect + ` WHERE conversation_id = ?`
	args := []any{string(conversationID)}
	if filter.Since != nil {
		q += ` AND posted_at >= ?`
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if filter.Tail > 0 {
		q += ` ORDER BY posted_at DESC LIMIT ?`
		args = append(args, filter.Tail)
	} else {
		q += ` ORDER BY posted_at ASC`
		if filter.Limit > 0 {
			q += ` LIMIT ?`
			args = append(args, filter.Limit)
		}
	}
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*conversation.Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FindRecent returns the N most-recent messages, ordered oldest→newest.
func (r *MessageRepo) FindRecent(ctx context.Context, conversationID conversation.ConversationID, n int) ([]*conversation.Message, error) {
	if n <= 0 {
		n = 50
	}
	msgs, err := r.FindByConversationID(ctx, conversationID, conversation.MessageFilter{Tail: n})
	if err != nil {
		return nil, err
	}
	// FindByConversationID with Tail returns DESC; flip to ASC for callers
	// who want chronological order.
	out := make([]*conversation.Message, len(msgs))
	for i, m := range msgs {
		out[len(msgs)-1-i] = m
	}
	return out, nil
}

// FindByVendorMsgRef returns the message with the given vendor msg ref
// (Bridge dedupe path).
func (r *MessageRepo) FindByVendorMsgRef(ctx context.Context, vendorMsgRef string) (*conversation.Message, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, messageSelect+` WHERE vendor_msg_ref = ?`, vendorMsgRef)
	m, err := scanMessage(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrMessageNotFound
	}
	return m, err
}

// UpdateVendorMsgRef performs the one-shot backfill. Returns
// ErrMessageImmutable if the message already has a vendor_msg_ref.
func (r *MessageRepo) UpdateVendorMsgRef(ctx context.Context, id conversation.MessageID, vendorMsgRef string) error {
	if vendorMsgRef == "" {
		return errors.New("message repo: vendor_msg_ref required")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	// Atomic conditional UPDATE: only set when currently NULL.
	res, err := exec.ExecContext(ctx,
		`UPDATE messages SET vendor_msg_ref = ? WHERE id = ? AND vendor_msg_ref IS NULL`,
		vendorMsgRef, string(id))
	if err != nil {
		if isUniqueConstraint(err) {
			return conversation.ErrMessageDuplicate
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Disambiguate: not found vs already set.
		var c int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE id = ?`, string(id))
		if err := row.Scan(&c); err != nil {
			return err
		}
		if c == 0 {
			return conversation.ErrMessageNotFound
		}
		return conversation.ErrMessageImmutable
	}
	return nil
}

const messageSelect = `SELECT id, conversation_id, sender_identity_id, content_kind, content,
	direction, vendor_msg_ref, input_request_ref, posted_at, created_at
	FROM messages`

func scanMessage(scan func(...any) error) (*conversation.Message, error) {
	var (
		id, conversationID, senderIdentityID, contentKind, content, direction string
		vendorMsgRef, inputRequestRef                                          sql.NullString
		postedAt, createdAt                                                    string
	)
	if err := scan(&id, &conversationID, &senderIdentityID, &contentKind, &content,
		&direction, &vendorMsgRef, &inputRequestRef, &postedAt, &createdAt); err != nil {
		return nil, err
	}
	pt, err := time.Parse(time.RFC3339Nano, postedAt)
	if err != nil {
		return nil, fmt.Errorf("parse posted_at: %w", err)
	}
	ct, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	return conversation.RehydrateMessage(conversation.RehydrateMessageInput{
		ID:               conversation.MessageID(id),
		ConversationID:   conversation.ConversationID(conversationID),
		SenderIdentityID: conversation.IdentityRef(senderIdentityID),
		ContentKind:      conversation.MessageContentKind(contentKind),
		Content:          content,
		Direction:        conversation.MessageDirection(direction),
		VendorMsgRef:     vendorMsgRef.String,
		InputRequestRef:  inputRequestRef.String,
		PostedAt:         pt,
		CreatedAt:        ct,
	})
}
