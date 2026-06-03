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

// ReadStateRepo implements conversation.UserConversationReadStateRepository
// against the user_conversation_read_state table (migration 0026).
type ReadStateRepo struct {
	db *sql.DB
}

// NewReadStateRepo constructs the repo.
func NewReadStateRepo(db *sql.DB) *ReadStateRepo {
	return &ReadStateRepo{db: db}
}

const readStateSelect = `SELECT user_id, conversation_id, last_seen_message_id,
	updated_at, version
	FROM user_conversation_read_state`

// FindByUserAndConv returns the row or ErrReadStateNotFound.
// DeleteByConversationID hard-removes all read-state rows for a conversation
// (v2.7 #198, DM delete). Idempotent — no rows = nil.
func (r *ReadStateRepo) DeleteByConversationID(ctx context.Context, convID conversation.ConversationID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM user_conversation_read_state WHERE conversation_id = ?`, string(convID))
	return err
}

func (r *ReadStateRepo) FindByUserAndConv(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) (*conversation.UserConversationReadState, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		readStateSelect+` WHERE user_id = ? AND conversation_id = ?`,
		string(userID), string(convID))
	s, err := scanReadState(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrReadStateNotFound
	}
	return s, err
}

// FindByUserBatch returns every row for a user, ordered by conversation id.
func (r *ReadStateRepo) FindByUserBatch(ctx context.Context,
	userID conversation.IdentityRef,
) ([]*conversation.UserConversationReadState, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		readStateSelect+` WHERE user_id = ? ORDER BY conversation_id ASC`,
		string(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*conversation.UserConversationReadState
	for rows.Next() {
		s, err := scanReadState(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Upsert inserts (Version == 0) or CAS-updates (Version > 0).
//
// Insert path: a unique-constraint failure (race with another writer
// trying to insert at the same PK) maps to ErrReadStateVersionConflict
// so the caller can refetch + retry.
//
// CAS path: an UPDATE with `WHERE version = ?` is the only safe way to
// detect a concurrent writer; RowsAffected() == 0 → conflict.
func (r *ReadStateRepo) Upsert(ctx context.Context,
	s *conversation.UserConversationReadState,
) error {
	if s == nil {
		return errors.New("read state repo: nil state")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	now := s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if s.Version == 0 {
		const stmt = `INSERT INTO user_conversation_read_state
			(user_id, conversation_id, last_seen_message_id, updated_at, version)
			VALUES (?, ?, ?, ?, 1)`
		if _, err := exec.ExecContext(ctx, stmt,
			string(s.UserID), string(s.ConversationID),
			string(s.LastSeenMessageID), now); err != nil {
			if isUniqueConstraint(err) {
				return conversation.ErrReadStateVersionConflict
			}
			return err
		}
		s.Version = 1
		return nil
	}
	const stmt = `UPDATE user_conversation_read_state
		SET last_seen_message_id = ?, updated_at = ?, version = version + 1
		WHERE user_id = ? AND conversation_id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(s.LastSeenMessageID), now,
		string(s.UserID), string(s.ConversationID), s.Version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return conversation.ErrReadStateVersionConflict
	}
	s.Version++
	return nil
}

func scanReadState(scan func(...any) error) (*conversation.UserConversationReadState, error) {
	var (
		userID, convID, lastSeen, updatedAt string
		version                             int
	)
	if err := scan(&userID, &convID, &lastSeen, &updatedAt, &version); err != nil {
		return nil, err
	}
	ut, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &conversation.UserConversationReadState{
		UserID:            conversation.IdentityRef(userID),
		ConversationID:    conversation.ConversationID(convID),
		LastSeenMessageID: conversation.MessageID(lastSeen),
		UpdatedAt:         ut,
		Version:           version,
	}, nil
}
