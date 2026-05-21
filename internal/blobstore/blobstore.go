// Package blobstore implements the BlobStore abstraction per 01-blob-store.md
// + ADR-0006 + conventions § 8.
//
// v1 ships a LocalDir implementation; S3-compatible is a future extension
// point. Both implementations share the same contract test (testing/contract.go).
package blobstore

import (
	"context"
	"errors"
	"io"
)

// BlobStore is the BC-agnostic large-blob persistence port.
//
// Per 01-blob-store.md "接口（伪 Go）" + conventions § 8: DB stores only
// relative paths; the actual bytes live behind this port.
type BlobStore interface {
	// Put writes the blob at relPath. size == -1 means unknown. If a blob
	// already exists at relPath it is overwritten.
	Put(ctx context.Context, relPath string, content io.Reader, size int64) error

	// Get returns a ReadCloser for the blob at relPath. Returns
	// ErrBlobNotFound if absent.
	Get(ctx context.Context, relPath string) (io.ReadCloser, error)

	// Delete removes the blob at relPath. ErrBlobNotFound if absent.
	Delete(ctx context.Context, relPath string) error

	// Exists reports whether a blob is present at relPath.
	Exists(ctx context.Context, relPath string) (bool, error)

	// List returns relative paths under prefix.
	List(ctx context.Context, prefix string) ([]string, error)

	// URL returns a displayable URL for the blob (file:// for local; pre-
	// signed https:// for S3 in the future).
	URL(relPath string) string
}

// Sentinel errors. Use errors.Is to test.
var (
	ErrBlobNotFound        = errors.New("blobstore: not found")
	ErrBlobAlreadyExists   = errors.New("blobstore: already exists")
	ErrBlobStoreUnavail    = errors.New("blobstore: store unavailable")
	ErrPayloadTooLarge     = errors.New("blobstore: payload too large")
)

// MaxBlobBytes is the default v1 guard against runaway uploads. Configurable
// at construction; defaults to 100 MiB per plan-4 § 6.7.
const MaxBlobBytes int64 = 100 * 1024 * 1024
