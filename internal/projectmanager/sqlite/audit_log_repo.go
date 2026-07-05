package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// AuditLogRepo is the SQLite-backed pm.AuditLogRepository (design §4.3): the
// append-only object-level change ledger in pm_audit_log (migration 0099). IDs are
// minted here (via idgen) so the domain aggregate stays free of an infra
// id-generation dependency — mirroring TaskActionLogRepo.
type AuditLogRepo struct {
	db  *sql.DB
	gen idgen.Generator
}

// NewAuditLogRepo constructs the repo. gen mints a ULID for any appended entry
// that arrives without an ID.
func NewAuditLogRepo(db *sql.DB, gen idgen.Generator) *AuditLogRepo {
	return &AuditLogRepo{db: db, gen: gen}
}

// DefaultAuditPageSize is the read API's page size when the caller passes limit<=0.
const DefaultAuditPageSize = 50

// MaxAuditPageSize caps a caller-requested page size.
const MaxAuditPageSize = 200

// Append inserts entry under the caller's ambient tx (so the audit row commits
// atomically with the change it records). A blank ID is assigned a fresh ULID; a
// blank Detail is normalized to '{}' to honor the column's JSON invariant.
func (r *AuditLogRepo) Append(ctx context.Context, entry pm.AuditEntry) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(entry.ID)
	if id == "" {
		id = r.gen.NewULID()
	}
	detail := strings.TrimSpace(entry.Detail)
	if detail == "" {
		detail = "{}"
	}
	_, err = exec.ExecContext(ctx,
		`INSERT INTO pm_audit_log
		   (id, project_id, object_type, object_id, change_type, field, from_value, to_value, actor_ref, detail, occurred_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, string(entry.ProjectID), string(entry.ObjectType), entry.ObjectID,
		string(entry.ChangeType), entry.Field, entry.FromValue, entry.ToValue,
		string(entry.ActorRef), detail, ts(entry.OccurredAt))
	return err
}

// ListByObject returns (objType, objID)'s ledger newest-first ((occurred_at, id)
// DESC), one page at a time. cursor is the id of the previous page's last row ("" =
// first page); limit caps the page (clamped to [1, MaxAuditPageSize], default
// DefaultAuditPageSize). nextCursor is "" on the final page. Pagination is stable
// under equal occurred_at values because it breaks ties on the unique id.
func (r *AuditLogRepo) ListByObject(ctx context.Context, objType pm.AuditObjectType, objID, cursor string, limit int) ([]pm.AuditEntry, string, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = DefaultAuditPageSize
	}
	if limit > MaxAuditPageSize {
		limit = MaxAuditPageSize
	}

	// Resolve the cursor row's occurred_at so the keyset predicate can page on the
	// (occurred_at, id) tuple without a dialect-specific row-value subquery. An
	// unknown cursor id yields the zero anchor (empty page follows) — treated as "no
	// more rows" rather than an error, so a stale cursor degrades gracefully.
	var curOccurred string
	if c := strings.TrimSpace(cursor); c != "" {
		if err := exec.QueryRowContext(ctx,
			`SELECT occurred_at FROM pm_audit_log WHERE id = ?`, c).Scan(&curOccurred); err != nil {
			if err == sql.ErrNoRows {
				return nil, "", nil
			}
			return nil, "", err
		}
	}

	// Fetch limit+1 to detect whether a further page exists.
	var rows *sql.Rows
	base := `SELECT id, project_id, object_type, object_id, change_type, field, from_value, to_value, actor_ref, detail, occurred_at
	         FROM pm_audit_log WHERE object_type = ? AND object_id = ?`
	if curOccurred == "" && strings.TrimSpace(cursor) == "" {
		rows, err = exec.QueryContext(ctx,
			base+` ORDER BY occurred_at DESC, id DESC LIMIT ?`,
			string(objType), objID, limit+1)
	} else {
		rows, err = exec.QueryContext(ctx,
			base+` AND (occurred_at < ? OR (occurred_at = ? AND id < ?))
			       ORDER BY occurred_at DESC, id DESC LIMIT ?`,
			string(objType), objID, curOccurred, curOccurred, strings.TrimSpace(cursor), limit+1)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	out := make([]pm.AuditEntry, 0, limit)
	for rows.Next() {
		var e pm.AuditEntry
		var projectID, objType2, objID2, changeType, actorRef, occurredAt string
		if err := rows.Scan(&e.ID, &projectID, &objType2, &objID2, &changeType,
			&e.Field, &e.FromValue, &e.ToValue, &actorRef, &e.Detail, &occurredAt); err != nil {
			return nil, "", err
		}
		e.ProjectID = pm.ProjectID(projectID)
		e.ObjectType = pm.AuditObjectType(objType2)
		e.ObjectID = objID2
		e.ChangeType = pm.AuditChangeType(changeType)
		e.ActorRef = pm.IdentityRef(actorRef)
		e.OccurredAt = parseTime(occurredAt)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(out) > limit {
		next = out[limit-1].ID // last row of THIS page is the next cursor
		out = out[:limit]
	}
	return out, next, nil
}

var _ pm.AuditLogRepository = (*AuditLogRepo)(nil)
