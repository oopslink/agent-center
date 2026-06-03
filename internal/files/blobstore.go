package files

import (
	"context"
	"errors"
	"time"
)

// ErrBlobNotFound is returned when a blob's metadata is absent.
var ErrBlobNotFound = errors.New("files: blob not found")

// BlobMetadata is the integrity/record-keeping row for one write-once blob.
// Identity is the opaque ULID (NOT the checksum): the sha256 is stored only
// for integrity verification and future opt-in dedup and never participates
// in addressing (ADR-0048 §1, plan §10 OQ8). Filename/mime/display-name do
// NOT live here — those are placement metadata on FileReference, because the
// same blob may appear under different names in different scopes.
type BlobMetadata struct {
	// ULID is the blob identity; the FileURI is ac://files/{ULID}.
	ULID string
	// SizeBytes is the stored content length.
	SizeBytes int64
	// ContentSHA256 is a hex digest for integrity only (optional in A0; the
	// transfer layer in D fills it on upload completion).
	ContentSHA256 string
	CreatedAt     time.Time
}

// URI returns the stable file URI for this blob.
func (m BlobMetadata) URI() (FileURI, error) { return NewFileURI(m.ULID) }

// BlobStore is the storage-layer seam: a generic content store that knows
// nothing about Task/Issue/Agent/Conversation (ADR-0048 §1). A0 defines the
// metadata-record interface only; the byte read/write/transfer mechanics
// (upload sessions, chunking, the local-fs backend) land in phase D. Blobs
// are write-once — there is no Update/overwrite by design (same-name uploads
// always mint a fresh ULID).
type BlobStore interface {
	// PutMetadata records a blob's metadata row (write-once; re-putting the
	// same ULID is a no-op or conflict per implementation). The byte content
	// itself is written by the phase-D transfer layer.
	PutMetadata(ctx context.Context, m BlobMetadata) error
	// GetMetadata returns the metadata row for a blob ULID, or ErrBlobNotFound.
	GetMetadata(ctx context.Context, blobULID string) (BlobMetadata, error)
}
