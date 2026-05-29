package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/persistence"
)

// BlobMetadataRepo implements files.BlobStore's metadata seam over SQLite.
// A0 records integrity metadata only; the byte content read/write + transfer
// sessions land in phase D (ADR-0048 §6).
type BlobMetadataRepo struct {
	db *sql.DB
}

// NewBlobMetadataRepo constructs the repo.
func NewBlobMetadataRepo(db *sql.DB) *BlobMetadataRepo {
	return &BlobMetadataRepo{db: db}
}

// PutMetadata records a write-once blob metadata row. Re-putting an existing
// ULID is rejected (write-once) — blobs never overwrite.
func (r *BlobMetadataRepo) PutMetadata(ctx context.Context, m files.BlobMetadata) error {
	if m.ULID == "" {
		return errors.New("files: blob metadata ulid is required")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO blob_metadata (ulid, size_bytes, content_sha256, created_at)
		VALUES (?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		m.ULID,
		m.SizeBytes,
		nullString(m.ContentSHA256),
		m.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

// GetMetadata returns the metadata row for a blob ULID, or ErrBlobNotFound.
func (r *BlobMetadataRepo) GetMetadata(ctx context.Context, blobULID string) (files.BlobMetadata, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT ulid, size_bytes, content_sha256, created_at FROM blob_metadata WHERE ulid = ?`,
		blobULID)
	var (
		m         files.BlobMetadata
		sha       sql.NullString
		createdAt string
	)
	if err := row.Scan(&m.ULID, &m.SizeBytes, &sha, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return files.BlobMetadata{}, files.ErrBlobNotFound
		}
		return files.BlobMetadata{}, err
	}
	m.ContentSHA256 = sha.String
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		m.CreatedAt = t
	}
	return m, nil
}

var _ files.BlobStore = (*BlobMetadataRepo)(nil)
