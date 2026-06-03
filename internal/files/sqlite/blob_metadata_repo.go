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

// DeleteMetadata removes the blob_metadata row for a blob ULID. It is
// idempotent: deleting an absent row is a no-op (no error). The D3-c GC calls
// this inside the per-candidate tx after the live-reference re-check passes.
func (r *BlobMetadataRepo) DeleteMetadata(ctx context.Context, blobULID string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM blob_metadata WHERE ulid = ?`, blobULID)
	return err
}

// ListCollectable enumerates blob ULIDs that are candidates for GC reaping:
// blobs with NO live reference whose "zero-since" instant is strictly before
// cutoff. zero-since is the later-bounded grace anchor:
//
//	COALESCE(MAX(deleted_at over the blob's references), blob_metadata.created_at)
//
// i.e. for an once-referenced-then-removed blob the grace runs from the last
// reference removal; for a never-referenced orphan it runs from blob creation.
// The file_uri join key is reconstructed as 'ac://files/'||ulid to match how
// FileReferenceRepo.Save stores file_uri (the full FileURI string).
//
// Times are stored as RFC3339Nano UTC strings (clock.Now is always UTC), so the
// lexicographic '<' comparison is a correct time comparison — the same
// convention FileTransferSessionRepo.ListExpired relies on.
func (r *BlobMetadataRepo) ListCollectable(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `SELECT b.ulid
		FROM blob_metadata b
		WHERE NOT EXISTS (
			SELECT 1 FROM file_references r
			WHERE r.file_uri = 'ac://files/' || b.ulid AND r.deleted_at IS NULL
		)
		AND COALESCE(
			(SELECT MAX(r2.deleted_at) FROM file_references r2
				WHERE r2.file_uri = 'ac://files/' || b.ulid),
			b.created_at
		) < ?
		ORDER BY b.created_at, b.ulid
		LIMIT ?`
	rows, err := exec.QueryContext(ctx, stmt, cutoff.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ulid string
		if err := rows.Scan(&ulid); err != nil {
			return nil, err
		}
		out = append(out, ulid)
	}
	return out, rows.Err()
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
