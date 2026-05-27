// Package sqlite implements the Discussion BC repositories.
//
// Per conventions § 9.w the underlying schema declares no FOREIGN KEY;
// referential integrity is enforced at the application service layer.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/persistence"
)

// IssueRepo implements discussion.IssueRepository over SQLite.
type IssueRepo struct {
	db *sql.DB
}

// NewIssueRepo constructs the repo.
func NewIssueRepo(db *sql.DB) *IssueRepo { return &IssueRepo{db: db} }

const issueSelect = `SELECT
	id, project_id, title, description, description_blob_ref,
	opened_by_identity_id, origin, opened_at, status,
	concluded_at, conclusion_summary, concluded_by_identity_id,
	withdraw_reason, withdraw_message,
	conversation_id, related_conversation_ids,
	created_at, updated_at, version
FROM issues`

// Save inserts a new Issue row. version=1.
func (r *IssueRepo) Save(ctx context.Context, i *discussion.Issue) error {
	if i == nil {
		return errors.New("issue repo: nil issue")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	relatedJSON, err := i.MarshalRelatedConversationIDsJSON()
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO issues (
		id, project_id, title, description, description_blob_ref,
		opened_by_identity_id, origin, opened_at, status,
		concluded_at, conclusion_summary, concluded_by_identity_id,
		withdraw_reason, withdraw_message,
		conversation_id, related_conversation_ids,
		created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(i.ID()),
		i.ProjectID(),
		i.Title(),
		i.Description(),
		nullString(i.DescriptionBlobRef()),
		i.OpenedByIdentityID(),
		string(i.Origin()),
		i.OpenedAt().Format(time.RFC3339Nano),
		string(i.Status()),
		nullTimePtr(i.ConcludedAt()),
		nullString(i.ConclusionSummary()),
		nullString(i.ConcludedByIdentityID()),
		nullString(i.WithdrawReason()),
		nullString(i.WithdrawMessage()),
		nullString(string(i.ConversationID())),
		relatedJSON,
		i.CreatedAt().Format(time.RFC3339Nano),
		i.UpdatedAt().Format(time.RFC3339Nano),
		i.Version(),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return discussion.ErrIssueAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns an Issue or ErrIssueNotFound.
func (r *IssueRepo) FindByID(ctx context.Context, id discussion.IssueID) (*discussion.Issue, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, issueSelect+` WHERE id = ?`, string(id))
	i, err := scanIssue(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, discussion.ErrIssueNotFound
	}
	return i, err
}

// FindByProject filters by project_id with optional status filter.
func (r *IssueRepo) FindByProject(ctx context.Context, projectID string, filter discussion.IssueFilter) ([]*discussion.Issue, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := issueSelect + ` WHERE project_id = ?`
	args := []any{projectID}
	if filter.Status != nil {
		q += ` AND status = ?`
		args = append(args, string(*filter.Status))
	}
	if filter.Cursor != nil {
		q += ` AND id > ?`
		args = append(args, string(*filter.Cursor))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = discussion.DefaultIssueLimit
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIssues(rows)
}

// FindAll returns every issue with the optional status / cursor /
// limit from IssueFilter applied. Used by the Web Console "All
// projects" filter (v2.5.15 #68); FindByStatus already covered a
// cross-project read but required a concrete status, so it couldn't
// service the "All status × All projects" combination.
func (r *IssueRepo) FindAll(ctx context.Context, filter discussion.IssueFilter) ([]*discussion.Issue, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := issueSelect
	args := []any{}
	where := []string{}
	if filter.Status != nil {
		where = append(where, `status = ?`)
		args = append(args, string(*filter.Status))
	}
	if filter.Cursor != nil {
		where = append(where, `id > ?`)
		args = append(args, string(*filter.Cursor))
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = discussion.DefaultIssueLimit
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIssues(rows)
}

// FindByStatus returns issues across all projects matching status.
func (r *IssueRepo) FindByStatus(ctx context.Context, status discussion.Status, filter discussion.IssueFilter) ([]*discussion.Issue, error) {
	if !status.IsValid() {
		return nil, fmt.Errorf("issue repo: invalid status %q", status)
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = discussion.DefaultIssueLimit
	}
	q := issueSelect + ` WHERE status = ? ORDER BY created_at DESC LIMIT ?`
	rows, err := exec.QueryContext(ctx, q, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIssues(rows)
}

// FindByOpener returns issues opened by the given identity_id.
func (r *IssueRepo) FindByOpener(ctx context.Context, openerIdentityID string) ([]*discussion.Issue, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := issueSelect + ` WHERE opened_by_identity_id = ? ORDER BY created_at DESC`
	rows, err := exec.QueryContext(ctx, q, openerIdentityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIssues(rows)
}

// UpdateStatus performs the CAS from→to transition (no concurrent
// reason/message fields; use UpdateWithdraw / UpdateConclusion for those).
func (r *IssueRepo) UpdateStatus(ctx context.Context, id discussion.IssueID, from, to discussion.Status, version int, at time.Time) error {
	if !from.IsValid() || !to.IsValid() {
		return fmt.Errorf("issue repo: invalid status from=%q to=%q", from, to)
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE issues SET status = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, string(to), now, string(id), string(from), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

// UpdateConversationID sets conversation_id; null→non-null only at app
// layer (caller must enforce that constraint before calling).
func (r *IssueRepo) UpdateConversationID(ctx context.Context, id discussion.IssueID, conversationID conversation.ConversationID, version int, at time.Time) error {
	if strings.TrimSpace(string(conversationID)) == "" {
		return errors.New("issue repo: conversation_id required for UpdateConversationID")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339Nano)
	// Enforce null→non-null at SQL level: only update where conversation_id
	// IS NULL. Combined with CAS on version, this gives us atomic "rebind
	// rejected" semantics.
	const stmt = `UPDATE issues SET conversation_id = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND conversation_id IS NULL`
	res, err := exec.ExecContext(ctx, stmt, string(conversationID), now, string(id), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

// UpdateConclusion writes conclusion_* fields atomically with status.
// status is set to the resolution's terminal kind by caller's previous
// UpdateStatus call (kept separate to mirror SimpleTransition / RichTransition
// split in other BCs).
func (r *IssueRepo) UpdateConclusion(ctx context.Context, id discussion.IssueID, summary, concludedBy string, concludedAt time.Time, version int) error {
	if strings.TrimSpace(summary) == "" || strings.TrimSpace(concludedBy) == "" {
		return errors.New("issue repo: summary and concluded_by required for UpdateConclusion")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	at := concludedAt.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE issues SET
			conclusion_summary = ?, concluded_by_identity_id = ?, concluded_at = ?,
			updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, summary, concludedBy, at, at, string(id), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

// UpdateRelatedConversationIDs replaces the JSON column.
func (r *IssueRepo) UpdateRelatedConversationIDs(ctx context.Context, id discussion.IssueID, ids []conversation.ConversationID, version int, at time.Time) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	// Build JSON via helper that ensures "[]" for empty input.
	asStr := make([]string, len(ids))
	for k, v := range ids {
		asStr[k] = string(v)
	}
	j, err := marshalStringList(asStr)
	if err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE issues SET related_conversation_ids = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, j, now, string(id), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

// UpdateWithdraw atomically writes withdraw_{reason,message} +
// concluded_by_identity_id + concluded_at + status=withdrawn.
func (r *IssueRepo) UpdateWithdraw(ctx context.Context, id discussion.IssueID, reason, message, withdrawnBy string, withdrawnAt time.Time, version int) error {
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(message) == "" {
		return errors.New("issue repo: reason+message required for UpdateWithdraw (conventions § 16)")
	}
	if strings.TrimSpace(withdrawnBy) == "" {
		return errors.New("issue repo: withdrawn_by required for UpdateWithdraw")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	at := withdrawnAt.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE issues SET
			status = ?, withdraw_reason = ?, withdraw_message = ?,
			concluded_by_identity_id = ?, concluded_at = ?,
			updated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND status NOT IN ('withdrawn','closed_no_action','closed_with_tasks')`
	res, err := exec.ExecContext(ctx, stmt,
		string(discussion.StatusWithdrawn), reason, message,
		withdrawnBy, at, at, string(id), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

// UpdateMetadata writes title + description (v2.5.x #64). Caller (the
// AR via service) is responsible for the non-terminal guard; the SQL
// keeps the guard belt-and-suspenders via status filter.
func (r *IssueRepo) UpdateMetadata(ctx context.Context, id discussion.IssueID, title, description string, version int, at time.Time) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("issue repo: title required for UpdateMetadata")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE issues SET title = ?, description = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?
		  AND status NOT IN ('withdrawn','closed_no_action','closed_with_tasks')`
	res, err := exec.ExecContext(ctx, stmt, title, description, now, string(id), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

// UpdateReopen flips a terminal issue back to open + clears the
// conclusion / withdraw fields (v2.5.x #64, (c) semantics). Spawned
// tasks are not cascaded by this method (or anywhere).
func (r *IssueRepo) UpdateReopen(ctx context.Context, id discussion.IssueID, version int, at time.Time) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE issues SET
			status = ?, conclusion_summary = '', concluded_by_identity_id = '',
			concluded_at = NULL, withdraw_reason = '', withdraw_message = '',
			updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?
		  AND status IN ('withdrawn','closed_no_action','closed_with_tasks')`
	res, err := exec.ExecContext(ctx, stmt, string(discussion.StatusOpen), now, string(id), version)
	if err != nil {
		return err
	}
	return r.casResult(ctx, res, id)
}

func (r *IssueRepo) casResult(ctx context.Context, res sql.Result, id discussion.IssueID) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		exec, exErr := persistence.ExecutorFromCtx(ctx, r.db)
		if exErr != nil {
			return exErr
		}
		var c int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE id = ?`, string(id))
		if scanErr := row.Scan(&c); scanErr == nil {
			if c == 0 {
				return discussion.ErrIssueNotFound
			}
		}
		return discussion.ErrIssueVersionConflict
	}
	return nil
}
