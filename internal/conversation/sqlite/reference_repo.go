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

// ReferenceRepo implements conversation.ConversationMessageReferenceRepository
// on SQLite (table `conversation_message_reference`, migration 0022).
type ReferenceRepo struct {
	db *sql.DB
}

// NewReferenceRepo constructs the repo.
func NewReferenceRepo(db *sql.DB) *ReferenceRepo {
	return &ReferenceRepo{db: db}
}

// Save batch-inserts carry-over references in the caller's tx. Duplicate
// (child_conversation_id, source_message_id) → ErrConversationAlreadyExists
// (the unique index protects append-only semantics).
func (r *ReferenceRepo) Save(ctx context.Context, refs []*conversation.ConversationMessageReference) error {
	if len(refs) == 0 {
		return nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO conversation_message_reference (
		id, child_conversation_id, source_conversation_id, source_message_id,
		created_by, created_at
	) VALUES (?,?,?,?,?,?)`
	for _, ref := range refs {
		if ref == nil {
			return errors.New("reference repo: nil reference in batch")
		}
		_, err := exec.ExecContext(ctx, stmt,
			ref.ID,
			string(ref.ChildConversationID),
			string(ref.SourceConversationID),
			string(ref.SourceMessageID),
			string(ref.CreatedBy),
			ref.CreatedAt.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			if isUniqueConstraint(err) {
				return conversation.ErrConversationAlreadyExists
			}
			return err
		}
	}
	return nil
}

// FindByChildConvID returns all references attached to a child conv,
// ordered by created_at ASC.
func (r *ReferenceRepo) FindByChildConvID(ctx context.Context, childConvID conversation.ConversationID) ([]*conversation.ConversationMessageReference, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		refSelect+` WHERE child_conversation_id = ? ORDER BY created_at ASC`,
		string(childConvID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRefs(rows)
}

// FindBySourceMsgID returns all references that point at a given source
// message (reverse lookup; useful for "which child convs carried this
// message over").
func (r *ReferenceRepo) FindBySourceMsgID(ctx context.Context, sourceMsgID conversation.MessageID) ([]*conversation.ConversationMessageReference, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		refSelect+` WHERE source_message_id = ? ORDER BY created_at ASC`,
		string(sourceMsgID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRefs(rows)
}

// DeleteByChildConvID removes all references for a child conv (used when
// the child conv itself is deleted; uncommon path).
func (r *ReferenceRepo) DeleteByChildConvID(ctx context.Context, childConvID conversation.ConversationID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM conversation_message_reference WHERE child_conversation_id = ?`,
		string(childConvID))
	return err
}

const refSelect = `SELECT id, child_conversation_id, source_conversation_id, source_message_id,
	created_by, created_at
	FROM conversation_message_reference`

func scanRefs(rows *sql.Rows) ([]*conversation.ConversationMessageReference, error) {
	var out []*conversation.ConversationMessageReference
	for rows.Next() {
		var (
			id, child, source, msgID, createdBy, createdAt string
		)
		if err := rows.Scan(&id, &child, &source, &msgID, &createdBy, &createdAt); err != nil {
			return nil, err
		}
		ct, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, &conversation.ConversationMessageReference{
			ID:                   id,
			ChildConversationID:  conversation.ConversationID(child),
			SourceConversationID: conversation.ConversationID(source),
			SourceMessageID:      conversation.MessageID(msgID),
			CreatedBy:            conversation.IdentityRef(createdBy),
			CreatedAt:            ct,
		})
	}
	return out, rows.Err()
}
