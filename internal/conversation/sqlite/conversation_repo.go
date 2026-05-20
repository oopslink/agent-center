// Package sqlite implements the Conversation BC repositories.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ConversationRepo implements conversation.ConversationRepository.
type ConversationRepo struct {
	db *sql.DB
}

// NewConversationRepo constructs the repo.
func NewConversationRepo(db *sql.DB) *ConversationRepo {
	return &ConversationRepo{db: db}
}

// Save inserts a new conversation row. Re-saving an existing id returns
// ErrConversationAlreadyExists.
func (r *ConversationRepo) Save(ctx context.Context, c *conversation.Conversation) error {
	if c == nil {
		return errors.New("conversation repo: nil conversation")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO conversations (
		id, kind, title, primary_channel_hint, primary_channel_thread_key,
		status, opened_at, closed_at, closed_reason, closed_message,
		created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(c.ID()),
		string(c.Kind()),
		nullString(c.Title()),
		nullString(c.PrimaryChannelHint()),
		nullString(c.PrimaryChannelThreadKey()),
		string(c.Status()),
		c.OpenedAt().Format(time.RFC3339Nano),
		nullTimePtr(c.ClosedAt()),
		nullString(c.ClosedReason()),
		nullString(c.ClosedMessage()),
		c.CreatedAt().Format(time.RFC3339Nano),
		c.UpdatedAt().Format(time.RFC3339Nano),
		c.Version(),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return conversation.ErrConversationAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns the conversation, or ErrConversationNotFound.
func (r *ConversationRepo) FindByID(ctx context.Context, id conversation.ConversationID) (*conversation.Conversation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, convSelect+` WHERE id = ?`, string(id))
	c, err := scanConversation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrConversationNotFound
	}
	return c, err
}

// Find returns conversations matching filter, ordered by id.
func (r *ConversationRepo) Find(ctx context.Context, filter conversation.ConversationFilter) ([]*conversation.Conversation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	sb := strings.Builder{}
	sb.WriteString(convSelect)
	sb.WriteString(` WHERE 1=1`)
	var args []any
	if filter.Kind != nil {
		sb.WriteString(` AND kind = ?`)
		args = append(args, string(*filter.Kind))
	}
	if filter.Status != nil {
		sb.WriteString(` AND status = ?`)
		args = append(args, string(*filter.Status))
	}
	if filter.Cursor != nil {
		sb.WriteString(` AND id > ?`)
		args = append(args, string(*filter.Cursor))
	}
	sb.WriteString(` ORDER BY id ASC`)
	limit := filter.Limit
	if limit <= 0 {
		limit = conversation.DefaultConversationLimit
	}
	sb.WriteString(` LIMIT ?`)
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*conversation.Conversation
	for rows.Next() {
		c, err := scanConversation(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FindByChannelAndThreadKey looks up by (channel, threadKey) (Bridge
// inbound reverse lookup).
func (r *ConversationRepo) FindByChannelAndThreadKey(ctx context.Context, channel, threadKey string) (*conversation.Conversation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx,
		convSelect+` WHERE primary_channel_hint = ? AND primary_channel_thread_key = ? LIMIT 1`,
		channel, threadKey)
	c, err := scanConversation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrConversationNotFound
	}
	return c, err
}

// UpdateStatus performs the CAS open→closed (or any from→to) transition.
func (r *ConversationRepo) UpdateStatus(ctx context.Context, id conversation.ConversationID, from, to conversation.ConversationStatus, version int, closedReason, closedMessage string, closedAt time.Time) error {
	if !from.IsValid() || !to.IsValid() {
		return conversation.ErrConversationInvalidStatus
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := closedAt.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE conversations
		SET status = ?, closed_at = ?, closed_reason = ?, closed_message = ?,
		    updated_at = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(to), nullTimePtrFromTime(closedAt, to == conversation.ConversationClosed),
		nullString(closedReason), nullString(closedMessage),
		now, string(id), string(from), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var c int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations WHERE id = ?`, string(id))
		if err := row.Scan(&c); err != nil {
			return err
		}
		if c == 0 {
			return conversation.ErrConversationNotFound
		}
		return conversation.ErrConversationVersionConflict
	}
	return nil
}

// UpdatePrimaryChannel sets the channel hint + thread key (Bridge async
// writeback).
func (r *ConversationRepo) UpdatePrimaryChannel(ctx context.Context, id conversation.ConversationID, channel, threadKey string, version int, at time.Time) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE conversations
		SET primary_channel_hint = ?, primary_channel_thread_key = ?,
		    updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, channel, threadKey, now, string(id), version)
	if err != nil {
		if isUniqueConstraint(err) {
			return conversation.ErrConversationAlreadyExists
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var c int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations WHERE id = ?`, string(id))
		if err := row.Scan(&c); err != nil {
			return err
		}
		if c == 0 {
			return conversation.ErrConversationNotFound
		}
		return conversation.ErrConversationVersionConflict
	}
	return nil
}

const convSelect = `SELECT id, kind, title, primary_channel_hint, primary_channel_thread_key,
	status, opened_at, closed_at, closed_reason, closed_message,
	created_at, updated_at, version
	FROM conversations`

func scanConversation(scan func(...any) error) (*conversation.Conversation, error) {
	var (
		id, kind                                                 string
		title, channelHint, threadKey                            sql.NullString
		status                                                   string
		openedAt                                                 string
		closedAt                                                 sql.NullString
		closedReason, closedMessage                              sql.NullString
		createdAt, updatedAt                                     string
		version                                                  int
	)
	if err := scan(&id, &kind, &title, &channelHint, &threadKey,
		&status, &openedAt, &closedAt, &closedReason, &closedMessage,
		&createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	op, err := time.Parse(time.RFC3339Nano, openedAt)
	if err != nil {
		return nil, fmt.Errorf("parse opened_at: %w", err)
	}
	cr, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	up, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, err
	}
	cl, err := parseNullTime(closedAt)
	if err != nil {
		return nil, err
	}
	return conversation.RehydrateConversation(conversation.RehydrateConversationInput{
		ID:                      conversation.ConversationID(id),
		Kind:                    conversation.ConversationKind(kind),
		Title:                   title.String,
		PrimaryChannelHint:      channelHint.String,
		PrimaryChannelThreadKey: threadKey.String,
		Status:                  conversation.ConversationStatus(status),
		OpenedAt:                op,
		ClosedAt:                cl,
		ClosedReason:            closedReason.String,
		ClosedMessage:           closedMessage.String,
		CreatedAt:               cr,
		UpdatedAt:               up,
		Version:                 version,
	})
}

// nullTimePtrFromTime returns NULL when use=false; ISO8601 string otherwise.
func nullTimePtrFromTime(t time.Time, use bool) any {
	if !use {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil, fmt.Errorf("parse time %q: %w", s.String, err)
	}
	return &t, nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE")
}
