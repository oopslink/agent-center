// Package sqlite implements the Conversation BC repositories (v2 per
// ADR-0032 / 0034 / 0035).
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

// Save inserts a new conversation row. Re-saving an existing id or a
// duplicate channel name returns ErrConversationAlreadyExists.
func (r *ConversationRepo) Save(ctx context.Context, c *conversation.Conversation) error {
	if c == nil {
		return errors.New("conversation repo: nil conversation")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	partsJSON, err := conversation.MarshalParticipantsJSON(c.Participants())
	if err != nil {
		return fmt.Errorf("conversation repo: marshal participants: %w", err)
	}
	const stmt = `INSERT INTO conversations (
		id, kind, name, description, parent_conversation_id,
		participants, created_by,
		status, opened_at, closed_at, closed_reason, closed_message,
		archived_at, archived_by,
		created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(c.ID()),
		string(c.Kind()),
		nullString(c.Name()),
		nullString(c.Description()),
		nullString(string(c.ParentConversationID())),
		partsJSON,
		string(c.CreatedBy()),
		string(c.Status()),
		c.OpenedAt().Format(time.RFC3339Nano),
		nullTimePtr(c.ClosedAt()),
		nullString(c.ClosedReason()),
		nullString(c.ClosedMessage()),
		nullTimePtr(c.ArchivedAt()),
		nullString(string(c.ArchivedBy())),
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
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, convSelect+` WHERE id = ?`, string(id))
	c, err := scanConversation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrConversationNotFound
	}
	return c, err
}

// Find returns conversations matching filter, ordered by id.
func (r *ConversationRepo) Find(ctx context.Context, filter conversation.ConversationFilter) ([]*conversation.Conversation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
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

// FindByName looks up a channel by its unique business name (ADR-0032 § 3).
func (r *ConversationRepo) FindByName(ctx context.Context, name string) (*conversation.Conversation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, convSelect+` WHERE name = ? AND kind = 'channel' LIMIT 1`, name)
	c, err := scanConversation(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrConversationNotFound
	}
	return c, err
}

// FindByParent returns the children of a Conversation (CV3 carry-over /
// CV4 派生入口 navigates the parent chain). Capped at
// DefaultReferenceLimit to prevent unbounded scans.
func (r *ConversationRepo) FindByParent(ctx context.Context, parentID conversation.ConversationID) ([]*conversation.Conversation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		convSelect+` WHERE parent_conversation_id = ? ORDER BY created_at ASC LIMIT ?`,
		string(parentID), conversation.DefaultReferenceLimit)
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

// UpdateStatus performs the CAS active→closed transition. archived
// transitions go through UpdateArchive.
func (r *ConversationRepo) UpdateStatus(ctx context.Context, id conversation.ConversationID, from, to conversation.ConversationStatus, version int, closedReason, closedMessage string, at time.Time) error {
	if !from.IsValid() || !to.IsValid() {
		return conversation.ErrConversationInvalidStatus
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE conversations
		SET status = ?, closed_at = ?, closed_reason = ?, closed_message = ?,
		    updated_at = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(to), nullTimePtrFromTime(at, to == conversation.ConversationClosed),
		nullString(closedReason), nullString(closedMessage),
		now, string(id), string(from), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.casConflict(ctx, exec, id)
	}
	return nil
}

// UpdateArchive transitions to archived (terminal) with audit who/when.
func (r *ConversationRepo) UpdateArchive(ctx context.Context, id conversation.ConversationID, version int, archivedBy conversation.IdentityRef, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE conversations
		SET status = 'archived', archived_at = ?, archived_by = ?,
		    updated_at = ?, version = version + 1
		WHERE id = ? AND status != 'archived' AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, now, string(archivedBy), now, string(id), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.casConflict(ctx, exec, id)
	}
	return nil
}

// UpdateParticipants does a CAS r-m-w on the JSON participants column.
func (r *ConversationRepo) UpdateParticipants(ctx context.Context, id conversation.ConversationID, participants []conversation.ParticipantElement, version int, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	partsJSON, err := conversation.MarshalParticipantsJSON(participants)
	if err != nil {
		return fmt.Errorf("conversation repo: marshal participants: %w", err)
	}
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE conversations
		SET participants = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, partsJSON, now, string(id), version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.casConflict(ctx, exec, id)
	}
	return nil
}

func (r *ConversationRepo) casConflict(ctx context.Context, exec persistence.SQLExecutor, id conversation.ConversationID) error {
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

const convSelect = `SELECT id, kind, name, description, parent_conversation_id,
	participants, created_by,
	status, opened_at, closed_at, closed_reason, closed_message,
	archived_at, archived_by,
	created_at, updated_at, version
	FROM conversations`

func scanConversation(scan func(...any) error) (*conversation.Conversation, error) {
	var (
		id, kind                                      string
		name, description, parent                     sql.NullString
		participantsJSON                              string
		createdBy                                     string
		status                                        string
		openedAt                                      string
		closedAt                                      sql.NullString
		closedReason, closedMessage                   sql.NullString
		archivedAt                                    sql.NullString
		archivedBy                                    sql.NullString
		createdAt, updatedAt                          string
		version                                       int
	)
	if err := scan(&id, &kind, &name, &description, &parent,
		&participantsJSON, &createdBy,
		&status, &openedAt, &closedAt, &closedReason, &closedMessage,
		&archivedAt, &archivedBy,
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
	ar, err := parseNullTime(archivedAt)
	if err != nil {
		return nil, err
	}
	parts, err := conversation.UnmarshalParticipantsJSON(participantsJSON)
	if err != nil {
		return nil, err
	}
	return conversation.RehydrateConversation(conversation.RehydrateConversationInput{
		ID:                   conversation.ConversationID(id),
		Kind:                 conversation.ConversationKind(kind),
		Name:                 name.String,
		Description:          description.String,
		ParentConversationID: conversation.ConversationID(parent.String),
		Participants:         parts,
		CreatedBy:            conversation.IdentityRef(createdBy),
		Status:               conversation.ConversationStatus(status),
		OpenedAt:             op,
		ClosedAt:             cl,
		ClosedReason:         closedReason.String,
		ClosedMessage:        closedMessage.String,
		ArchivedAt:           ar,
		ArchivedBy:           conversation.IdentityRef(archivedBy.String),
		CreatedAt:            cr,
		UpdatedAt:            up,
		Version:              version,
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
