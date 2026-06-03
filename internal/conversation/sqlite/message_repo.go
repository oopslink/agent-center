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

// MessageRepo implements conversation.MessageRepository (v2 — vendor_msg_ref
// dropped per ADR-0031).
type MessageRepo struct {
	db *sql.DB
}

// NewMessageRepo constructs the repo.
func NewMessageRepo(db *sql.DB) *MessageRepo {
	return &MessageRepo{db: db}
}

// Append inserts a message row (append-only).
func (r *MessageRepo) Append(ctx context.Context, m *conversation.Message) error {
	if m == nil {
		return errors.New("message repo: nil message")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	ctxRefsJSON, err := conversation.MarshalContextRefsJSON(m.ContextRefs())
	if err != nil {
		return fmt.Errorf("message repo: marshal context_refs: %w", err)
	}
	attsJSON, err := conversation.MarshalAttachmentsJSON(m.Attachments())
	if err != nil {
		return fmt.Errorf("message repo: marshal attachments: %w", err)
	}
	const stmt = `INSERT INTO messages (
		id, conversation_id, sender_identity_id, content_kind, content,
		direction, input_request_ref, context_refs, attachments, posted_at, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(m.ID()),
		string(m.ConversationID()),
		string(m.SenderIdentityID()),
		string(m.ContentKind()),
		m.Content(),
		string(m.Direction()),
		nullString(m.InputRequestRef()),
		ctxRefsJSON,
		attsJSON,
		m.PostedAt().Format(time.RFC3339Nano),
		m.CreatedAt().Format(time.RFC3339Nano),
	)
	return err
}

// DeleteByConversationID hard-removes all messages of a conversation (v2.7 #198,
// DM delete). Idempotent — no rows = nil.
func (r *MessageRepo) DeleteByConversationID(ctx context.Context, conversationID conversation.ConversationID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ?`, string(conversationID))
	return err
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

// FindByIDs batches lookups via a single `WHERE id IN (?,...)` query.
// Missing ids are silently skipped per the interface contract.
func (r *MessageRepo) FindByIDs(ctx context.Context, ids []conversation.MessageID) ([]*conversation.Message, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	// Build `(?,?,...)` placeholders + args.
	placeholders := make([]byte, 0, len(ids)*2)
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, string(id))
	}
	q := messageSelect + ` WHERE id IN (` + string(placeholders) + `)`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*conversation.Message, 0, len(ids))
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
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
	out := make([]*conversation.Message, len(msgs))
	for i, m := range msgs {
		out[len(msgs)-1-i] = m
	}
	return out, nil
}

const messageSelect = `SELECT id, conversation_id, sender_identity_id, content_kind, content,
	direction, input_request_ref, context_refs, attachments, posted_at, created_at
	FROM messages`

func scanMessage(scan func(...any) error) (*conversation.Message, error) {
	var (
		id, conversationID, senderIdentityID, contentKind, content, direction string
		inputRequestRef                                                       sql.NullString
		contextRefsJSON, attachmentsJSON                                      sql.NullString
		postedAt, createdAt                                                   string
	)
	if err := scan(&id, &conversationID, &senderIdentityID, &contentKind, &content,
		&direction, &inputRequestRef, &contextRefsJSON, &attachmentsJSON, &postedAt, &createdAt); err != nil {
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
	ctxRefs, err := conversation.UnmarshalContextRefsJSON(contextRefsJSON.String)
	if err != nil {
		return nil, fmt.Errorf("parse context_refs: %w", err)
	}
	atts, err := conversation.UnmarshalAttachmentsJSON(attachmentsJSON.String)
	if err != nil {
		return nil, fmt.Errorf("parse attachments: %w", err)
	}
	return conversation.RehydrateMessage(conversation.RehydrateMessageInput{
		ID:               conversation.MessageID(id),
		ConversationID:   conversation.ConversationID(conversationID),
		SenderIdentityID: conversation.IdentityRef(senderIdentityID),
		ContentKind:      conversation.MessageContentKind(contentKind),
		Content:          content,
		Direction:        conversation.MessageDirection(direction),
		InputRequestRef:  inputRequestRef.String,
		ContextRefs:      ctxRefs,
		Attachments:      atts,
		PostedAt:         pt,
		CreatedAt:        ct,
	})
}
