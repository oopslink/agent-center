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

// FollowStateRepo implements
// conversation.UserConversationFollowStateRepository against the
// user_conversation_follow_state table (migration 0050). It mirrors the
// optimistic-locking idiom of ReadStateRepo (migration 0026).
type FollowStateRepo struct {
	db *sql.DB
}

// NewFollowStateRepo constructs the repo.
func NewFollowStateRepo(db *sql.DB) *FollowStateRepo {
	return &FollowStateRepo{db: db}
}

const followStateSelect = `SELECT user_id, conversation_id, followed,
	updated_at, version
	FROM user_conversation_follow_state`

// FindByUserAndConv returns the row or ErrFollowStateNotFound.
func (r *FollowStateRepo) FindByUserAndConv(ctx context.Context,
	userID conversation.IdentityRef, convID conversation.ConversationID,
) (*conversation.UserConversationFollowState, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		followStateSelect+` WHERE user_id = ? AND conversation_id = ?`,
		string(userID), string(convID))
	s, err := scanFollowState(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrFollowStateNotFound
	}
	return s, err
}

// FindByUserBatch returns every override row for a user, ordered by
// conversation id.
func (r *FollowStateRepo) FindByUserBatch(ctx context.Context,
	userID conversation.IdentityRef,
) ([]*conversation.UserConversationFollowState, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		followStateSelect+` WHERE user_id = ? ORDER BY conversation_id ASC`,
		string(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*conversation.UserConversationFollowState
	for rows.Next() {
		s, err := scanFollowState(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Upsert inserts (Version == 0) or CAS-updates (Version > 0), recording
// an explicit follow/unfollow override.
func (r *FollowStateRepo) Upsert(ctx context.Context,
	s *conversation.UserConversationFollowState,
) error {
	if s == nil {
		return errors.New("follow state repo: nil state")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	now := s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if s.Version == 0 {
		const stmt = `INSERT INTO user_conversation_follow_state
			(user_id, conversation_id, followed, updated_at, version)
			VALUES (?, ?, ?, ?, 1)`
		if _, err := exec.ExecContext(ctx, stmt,
			string(s.UserID), string(s.ConversationID),
			boolToInt(s.Followed), now); err != nil {
			if persistence.IsUniqueViolation(err) {
				return conversation.ErrFollowStateVersionConflict
			}
			return err
		}
		s.Version = 1
		return nil
	}
	const stmt = `UPDATE user_conversation_follow_state
		SET followed = ?, updated_at = ?, version = version + 1
		WHERE user_id = ? AND conversation_id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		boolToInt(s.Followed), now,
		string(s.UserID), string(s.ConversationID), s.Version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return conversation.ErrFollowStateVersionConflict
	}
	s.Version++
	return nil
}

// InsertIfAbsent records followed=true only when no row exists yet. A
// present row (whether followed=true or an explicit unfollow) is left
// untouched, so auto-follow never resurrects an explicit unfollow.
func (r *FollowStateRepo) InsertIfAbsent(ctx context.Context,
	s *conversation.UserConversationFollowState,
) (bool, error) {
	if s == nil {
		return false, errors.New("follow state repo: nil state")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	now := s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	const stmt = `INSERT INTO user_conversation_follow_state
		(user_id, conversation_id, followed, updated_at, version)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT (user_id, conversation_id) DO NOTHING`
	res, err := exec.ExecContext(ctx, stmt,
		string(s.UserID), string(s.ConversationID),
		boolToInt(s.Followed), now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.Version = 1
	}
	return n > 0, nil
}

// DeleteByConversationID hard-removes all follow-state rows for a
// conversation. Idempotent — no rows = nil.
func (r *FollowStateRepo) DeleteByConversationID(ctx context.Context,
	convID conversation.ConversationID,
) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM user_conversation_follow_state WHERE conversation_id = ?`,
		string(convID))
	return err
}

func scanFollowState(scan func(...any) error) (*conversation.UserConversationFollowState, error) {
	var (
		userID, convID, updatedAt string
		followed, version         int
	)
	if err := scan(&userID, &convID, &followed, &updatedAt, &version); err != nil {
		return nil, err
	}
	ut, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &conversation.UserConversationFollowState{
		UserID:         conversation.IdentityRef(userID),
		ConversationID: conversation.ConversationID(convID),
		Followed:       followed != 0,
		UpdatedAt:      ut,
		Version:        version,
	}, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
