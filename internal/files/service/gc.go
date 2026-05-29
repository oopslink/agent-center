package service

// This file hosts the files refcount garbage-collection job (v2.7 D3-c,
// ADR-0048 §5). A blob's bytes are physically reaped once it has ZERO live
// references past a grace period; abandoned (expired) upload sessions are first
// transitioned to expired so their written-but-never-referenced blobs surface
// as never-referenced orphans and become collectable too.
//
// Crash-safety / delete ordering (PM-mandated):
//
//   - Candidate selection (ListCollectable) and the physical delete are NOT
//     atomic — a reference could be added in between. So inside the per-candidate
//     transaction we RE-CHECK CountLiveByURI == 0 before deleting anything; if a
//     live reference appeared we SKIP the blob entirely (guards the select→delete
//     race).
//   - The blob FILE delete is a filesystem op and cannot join the SQL tx. We
//     order it so a crash can never leave blob_metadata pointing at an
//     already-deleted file:
//     1. open the tx, re-check CountLiveByURI == 0 (still zero → proceed),
//     2. delete the blob FILE (blobstore.Delete; missing → treated as ok so a
//        partial prior run reconciles cleanly),
//     3. delete the blob_metadata ROW (in the tx),
//     4. commit.
//     If step 3/commit fails the tx rolls back: metadata survives, so the blob
//     is simply re-selected next pass (the file is already gone, step 2 is a
//     no-op via the missing→ok rule). If the process crashes between 2 and 4,
//     same outcome: metadata still present, blob re-collected later, idempotent
//     file delete. The forbidden window — metadata deleted while the file still
//     exists (a leaked orphan with no record) — never occurs because metadata is
//     deleted strictly AFTER the file.

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/persistence"
)

// DefaultGCGrace is the default reference-zero grace period before a blob's
// bytes are reaped (ADR-0048 §5). A long grace makes GC frequency non-critical.
const DefaultGCGrace = 7 * 24 * time.Hour

// defaultGCBatch bounds how many candidate blobs one RunGCOnce pass processes.
const defaultGCBatch = 500

// BlobGCRepo is the metadata-side seam the GC needs beyond the byte BlobStore:
// candidate enumeration + the metadata-row delete. The SQLite BlobMetadataRepo
// satisfies it.
type BlobGCRepo interface {
	// ListCollectable returns blob ULIDs with no live reference whose
	// zero-since instant is before cutoff (caller passes now-grace).
	ListCollectable(ctx context.Context, cutoff time.Time, limit int) ([]string, error)
	// DeleteMetadata removes a blob's metadata row (idempotent).
	DeleteMetadata(ctx context.Context, blobULID string) error
}

// SetGCRepo wires the GC metadata repo onto the Service. Kept separate from the
// New() Deps so D3-c stays additive — existing constructions need not change.
func (s *Service) SetGCRepo(repo BlobGCRepo) *Service {
	s.gcRepo = repo
	return s
}

// RunGCOnce performs one garbage-collection pass and returns the number of
// blobs whose bytes were reaped.
//
// Step 1: expire stale sessions. Every open session past its expiry is moved to
// expired so an abandoned upload's blob (if bytes were written but no reference
// was ever added) becomes a collectable never-referenced orphan.
//
// Step 2: enumerate collectable blobs (zero live refs, zero-since past grace)
// and, per candidate, run the re-check + delete transaction described in this
// file's header.
func (s *Service) RunGCOnce(ctx context.Context, grace time.Duration) (collected int, err error) {
	if s.gcRepo == nil {
		return 0, errors.New("files: GC repo not wired (call SetGCRepo)")
	}
	if grace <= 0 {
		grace = DefaultGCGrace
	}
	now := s.clock.Now()

	// Step 1: expire abandoned upload/download sessions.
	expired, err := s.sessions.ListExpired(ctx, now)
	if err != nil {
		return 0, err
	}
	for _, sess := range expired {
		if !sess.IsOpen() {
			continue
		}
		if err := sess.Expire(now); err != nil {
			// Concurrent terminal transition; skip.
			continue
		}
		if err := s.sessions.Update(ctx, sess); err != nil {
			return collected, err
		}
	}

	// Step 2: enumerate + reap collectable blobs.
	cutoff := now.Add(-grace)
	cands, err := s.gcRepo.ListCollectable(ctx, cutoff, defaultGCBatch)
	if err != nil {
		return collected, err
	}
	for _, blobULID := range cands {
		ok, derr := s.collectOne(ctx, blobULID)
		if derr != nil {
			return collected, derr
		}
		if ok {
			collected++
		}
	}
	return collected, nil
}

// collectOne runs the per-candidate re-check + delete transaction for one blob
// ULID. It returns (true, nil) when the blob was reaped, (false, nil) when it
// was safely skipped (a live reference reappeared), or a non-nil error.
func (s *Service) collectOne(ctx context.Context, blobULID string) (bool, error) {
	fileURI, err := files.NewFileURI(blobULID)
	if err != nil {
		// Not a valid ULID identity; skip rather than abort the whole pass.
		return false, nil
	}
	rel, err := s.resolver.ObjectPath(fileURI)
	if err != nil {
		return false, err
	}

	collected := false
	txErr := persistence.RunInTx(ctx, s.db, func(ctx context.Context) error {
		// SAFETY re-check inside the tx: a reference may have been added between
		// candidate selection and now. If so, SKIP — never delete a live blob.
		n, cerr := s.refs.CountLiveByURI(ctx, fileURI)
		if cerr != nil {
			return cerr
		}
		if n != 0 {
			return nil // skip; leave blob + metadata intact
		}
		// Still zero. Delete the FILE first (idempotent: missing → ok), then the
		// metadata row in this tx — see the file header for crash-safety order.
		if derr := s.blobs.Delete(ctx, rel); derr != nil && !errors.Is(derr, blobstore.ErrBlobNotFound) {
			return derr
		}
		if derr := s.gcRepo.DeleteMetadata(ctx, blobULID); derr != nil {
			return derr
		}
		collected = true
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	return collected, nil
}
