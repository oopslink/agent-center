// Package sqlite implements the files BC persistence seams (v2.7 A0,
// ADR-0048): FileReference placement records and BlobMetadata integrity
// rows. Blob byte transfer (upload/download/GC) lands in phase D.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/persistence"
)

// FileReferenceRepo implements files.FileReferenceRepository.
type FileReferenceRepo struct {
	db *sql.DB
}

// NewFileReferenceRepo constructs the repo.
func NewFileReferenceRepo(db *sql.DB) *FileReferenceRepo {
	return &FileReferenceRepo{db: db}
}

const fileRefSelect = `SELECT id, file_uri, scope, scope_id, filename, mime_type,
	size_bytes, display_name, created_by, created_at, deleted_at
	FROM file_references`

// Save inserts a reference record.
func (r *FileReferenceRepo) Save(ctx context.Context, ref files.FileReference) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO file_references (
		id, file_uri, scope, scope_id, filename, mime_type,
		size_bytes, display_name, created_by, created_at, deleted_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		ref.ID,
		ref.FileURI.String(),
		string(ref.Scope),
		ref.ScopeID,
		nullString(ref.Filename),
		nullString(ref.MimeType),
		ref.SizeBytes,
		nullString(ref.DisplayName),
		ref.CreatedBy,
		ref.CreatedAt.Format(time.RFC3339Nano),
		nullTimePtr(ref.DeletedAt),
	)
	return err
}

// FindByID returns one reference or ErrReferenceNotFound.
func (r *FileReferenceRepo) FindByID(ctx context.Context, id string) (files.FileReference, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, fileRefSelect+` WHERE id = ?`, id)
	ref, err := scanFileRef(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return files.FileReference{}, files.ErrReferenceNotFound
	}
	return ref, err
}

// FindByScope lists live references attached to a {scope, scope_id}.
func (r *FileReferenceRepo) FindByScope(ctx context.Context, scope files.FileScope, scopeID string) ([]files.FileReference, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		fileRefSelect+` WHERE scope = ? AND scope_id = ? AND deleted_at IS NULL ORDER BY created_at, id`,
		string(scope), scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFileRefs(rows)
}

// FindByURI lists all live references pointing at a blob.
func (r *FileReferenceRepo) FindByURI(ctx context.Context, uri files.FileURI) ([]files.FileReference, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		fileRefSelect+` WHERE file_uri = ? AND deleted_at IS NULL ORDER BY created_at, id`,
		uri.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFileRefs(rows)
}

// SoftDelete marks a reference deleted at t (idempotent: only live rows).
func (r *FileReferenceRepo) SoftDelete(ctx context.Context, id string, t time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE file_references SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
		t.Format(time.RFC3339Nano), id)
	return err
}

// CountLiveByURI returns the number of live references to a blob.
func (r *FileReferenceRepo) CountLiveByURI(ctx context.Context, uri files.FileURI) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_references WHERE file_uri = ? AND deleted_at IS NULL`,
		uri.String())
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func scanFileRefs(rows *sql.Rows) ([]files.FileReference, error) {
	var out []files.FileReference
	for rows.Next() {
		ref, err := scanFileRef(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func scanFileRef(scan func(dest ...any) error) (files.FileReference, error) {
	var (
		ref                                      files.FileReference
		uri, scope                               string
		filename, mimeType, displayName, deleted sql.NullString
		createdAt                                string
	)
	if err := scan(
		&ref.ID, &uri, &scope, &ref.ScopeID,
		&filename, &mimeType, &ref.SizeBytes, &displayName,
		&ref.CreatedBy, &createdAt, &deleted,
	); err != nil {
		return files.FileReference{}, err
	}
	ref.FileURI = files.FileURI(uri)
	ref.Scope = files.FileScope(scope)
	ref.Filename = filename.String
	ref.MimeType = mimeType.String
	ref.DisplayName = displayName.String
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		ref.CreatedAt = t
	}
	if deleted.Valid && deleted.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, deleted.String); err == nil {
			ref.DeletedAt = &t
		}
	}
	return ref, nil
}

// --- small null helpers (local to this package, mirrors conversation/sqlite) ---

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}
