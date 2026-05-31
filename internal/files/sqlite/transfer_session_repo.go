package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/persistence"
)

// FileTransferSessionRepo implements files.FileTransferSessionRepository over
// SQLite. ExecutorFromCtx-aware so AppServices can wrap Save/Update in a tx.
type FileTransferSessionRepo struct {
	db *sql.DB
}

// NewFileTransferSessionRepo constructs the repo.
func NewFileTransferSessionRepo(db *sql.DB) *FileTransferSessionRepo {
	return &FileTransferSessionRepo{db: db}
}

const transferSessionSelect = `SELECT id, file_uri, transfer_uri, direction, status,
	content_type, size, sha256, scope, scope_id, created_by, created_at, expires_at
	FROM file_transfer_sessions`

// Save inserts a new session row.
func (r *FileTransferSessionRepo) Save(ctx context.Context, s *files.FileTransferSession) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO file_transfer_sessions (
		id, file_uri, transfer_uri, direction, status, content_type,
		size, sha256, scope, scope_id, created_by, created_at, expires_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		s.ID(),
		s.FileURI().String(),
		s.TransferURI(),
		string(s.Direction()),
		string(s.Status()),
		nullString(s.ContentType()),
		s.Size(),
		nullString(s.SHA256()),
		nullString(string(s.Scope())),
		nullString(s.ScopeID()),
		s.CreatedBy(),
		s.CreatedAt().Format(time.RFC3339Nano),
		s.ExpiresAt().Format(time.RFC3339Nano),
	)
	return err
}

// Update overwrites the mutable fields of an existing session.
func (r *FileTransferSessionRepo) Update(ctx context.Context, s *files.FileTransferSession) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE file_transfer_sessions
		SET status = ?, sha256 = ?, size = ?
		WHERE id = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(s.Status()),
		nullString(s.SHA256()),
		s.Size(),
		s.ID(),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return files.ErrTransferSessionNotFound
	}
	return err
}

// FindByID returns one session or ErrTransferSessionNotFound.
func (r *FileTransferSessionRepo) FindByID(ctx context.Context, id string) (*files.FileTransferSession, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, transferSessionSelect+` WHERE id = ?`, id)
	s, err := scanTransferSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, files.ErrTransferSessionNotFound
	}
	return s, err
}

// FindByTransferURI returns one session by transfer URI or
// ErrTransferSessionNotFound.
func (r *FileTransferSessionRepo) FindByTransferURI(ctx context.Context, transferURI string) (*files.FileTransferSession, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, transferSessionSelect+` WHERE transfer_uri = ?`, transferURI)
	s, err := scanTransferSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, files.ErrTransferSessionNotFound
	}
	return s, err
}

// ListExpired returns open sessions with expires_at strictly before before.
func (r *FileTransferSessionRepo) ListExpired(ctx context.Context, before time.Time) ([]*files.FileTransferSession, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		transferSessionSelect+` WHERE status = ? AND expires_at < ? ORDER BY expires_at, id`,
		string(files.StatusOpen), before.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*files.FileTransferSession
	for rows.Next() {
		s, err := scanTransferSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListOpen returns LIVE in-flight sessions: status=open AND expires_at > now.
// NO LIMIT — the Environment-page transfer view (#139) must see ALL of an org's
// in-flight sessions after org-resolution (no global cap that truncates one org —
// #126). Expired-but-not-reaped open sessions are excluded (dead, not in-flight).
// Uses idx_fts_status_expires (status, expires_at).
func (r *FileTransferSessionRepo) ListOpen(ctx context.Context, now time.Time) ([]*files.FileTransferSession, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		transferSessionSelect+` WHERE status = ? AND expires_at > ? ORDER BY created_at, id`,
		string(files.StatusOpen), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*files.FileTransferSession
	for rows.Next() {
		s, err := scanTransferSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanTransferSession(scan func(dest ...any) error) (*files.FileTransferSession, error) {
	var (
		id, fileURI, transferURI, direction, status string
		contentType, sha, scope, scopeID            sql.NullString
		size                                        int64
		createdBy, createdAt, expiresAt             string
	)
	if err := scan(
		&id, &fileURI, &transferURI, &direction, &status,
		&contentType, &size, &sha, &scope, &scopeID,
		&createdBy, &createdAt, &expiresAt,
	); err != nil {
		return nil, err
	}
	var createdT, expiresT time.Time
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		createdT = t
	}
	if t, err := time.Parse(time.RFC3339Nano, expiresAt); err == nil {
		expiresT = t
	}
	return files.RehydrateTransferSession(
		id,
		files.FileURI(fileURI),
		transferURI,
		files.TransferDirection(direction),
		files.TransferStatus(status),
		contentType.String,
		size,
		sha.String,
		files.FileScope(scope.String),
		scopeID.String,
		createdBy,
		createdT,
		expiresT,
	), nil
}

var _ files.FileTransferSessionRepository = (*FileTransferSessionRepo)(nil)
