// Package service hosts the files transfer AppService (v2.7 D3-a, ADR-0048 §6):
// the create→write→complete upload flow and the download/open-blob flow over the
// FileTransferSession AR. It owns NO HTTP, NO reachability authorization, and NO
// GC — those land in later D3 slices (D3-b/c/d). The AppService is the only place
// the transfer-session AR, the FileTransferSessionRepository, the Resolver and the
// BlobStore are wired together.
//
// Path contract: the Resolver MUST yield a blobstore-relative ObjectPath (e.g.
// files.NewLocalResolver("") → objects/{h1}/{h2}/{ulid}); the BlobStore owns the
// physical root. Identity is the opaque ULID, so the same FileURI resolves
// identically regardless of backend.
package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/files"
	"github.com/oopslink/agent-center/internal/idgen"
)

// Service-level sentinels (the AR/store sentinels are reused as-is).
var (
	// ErrBlobAlreadyExists is returned by WriteBlob when the blob bytes were
	// already written (blobs are write-once at the transfer layer).
	ErrBlobAlreadyExists = errors.New("files: blob already exists")
	// ErrNotUploadSession is returned when an upload-only operation targets a
	// non-upload (download) session.
	ErrNotUploadSession = errors.New("files: session is not an upload session")
	// ErrSessionNotOpen is returned when an operation requires an open session
	// but the session is completed/canceled/expired.
	ErrSessionNotOpen = errors.New("files: transfer session is not open")
	// ErrBlobNotFound is returned by CreateDownloadSession/OpenBlob when the
	// underlying blob bytes are absent.
	ErrBlobNotFound = errors.New("files: blob not found")
)

// Deps bundles the Service dependencies.
type Deps struct {
	DB         *sql.DB
	Sessions   files.FileTransferSessionRepository
	References files.FileReferenceRepository
	Resolver   files.Resolver
	BlobStore  blobstore.BlobStore
	IDGen      idgen.Generator
	Clock      clock.Clock
}

// Service is the files transfer AppService facade.
type Service struct {
	db       *sql.DB
	sessions files.FileTransferSessionRepository
	refs     files.FileReferenceRepository
	resolver files.Resolver
	blobs    blobstore.BlobStore
	idgen    idgen.Generator
	clock    clock.Clock
	gcRepo   BlobGCRepo // optional; wired via SetGCRepo for the D3-c GC job
}

// New constructs the Service. Clock defaults to the system clock.
func New(d Deps) *Service {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Service{
		db:       d.DB,
		sessions: d.Sessions,
		refs:     d.References,
		resolver: d.Resolver,
		blobs:    d.BlobStore,
		idgen:    d.IDGen,
		clock:    clk,
	}
}

// CreateUploadCmd is the input to CreateUploadSession.
type CreateUploadCmd struct {
	ContentType string
	Size        int64
	Scope       files.FileScope // optional
	ScopeID     string          // optional
	CreatedBy   string
}

// CreateUploadSession mints a fresh upload session (new FileURI + transfer URI)
// and persists it. The returned session carries the final FileURI the caller
// will reference (ADR-0048 §2).
func (s *Service) CreateUploadSession(ctx context.Context, cmd CreateUploadCmd) (*files.FileTransferSession, error) {
	now := s.clock.Now()
	sess, err := files.NewUploadSession(files.NewUploadInput{
		FileULID:    s.idgen.NewULID(),
		SessionULID: s.idgen.NewULID(),
		ContentType: cmd.ContentType,
		Size:        cmd.Size,
		Scope:       cmd.Scope,
		ScopeID:     cmd.ScopeID,
		CreatedBy:   cmd.CreatedBy,
		CreatedAt:   now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Save(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// WriteBlob streams content into the blob backing an open upload session. Blobs
// are write-once: if the bytes already exist WriteBlob returns
// ErrBlobAlreadyExists and does not overwrite.
func (s *Service) WriteBlob(ctx context.Context, transferURI string, content io.Reader, size int64) error {
	sess, err := s.sessions.FindByTransferURI(ctx, transferURI)
	if err != nil {
		return err
	}
	if sess.Direction() != files.DirectionUpload {
		return ErrNotUploadSession
	}
	if !sess.IsOpen() {
		return ErrSessionNotOpen
	}
	rel, err := s.resolver.ObjectPath(sess.FileURI())
	if err != nil {
		return err
	}
	exists, err := s.blobs.Exists(ctx, rel)
	if err != nil {
		return err
	}
	if exists {
		return ErrBlobAlreadyExists
	}
	return s.blobs.Put(ctx, rel, content, size)
}

// CompleteUpload finalizes an open upload session, recording the integrity
// sha256 + final size.
func (s *Service) CompleteUpload(ctx context.Context, transferURI string, sha256 string, size int64) error {
	sess, err := s.sessions.FindByTransferURI(ctx, transferURI)
	if err != nil {
		return err
	}
	if err := sess.Complete(sha256, size, s.clock.Now()); err != nil {
		return err
	}
	return s.sessions.Update(ctx, sess)
}

// CreateDownloadSession creates an open download session for an existing blob.
// It does NOT perform reachability authorization (D3-d) — it only verifies the
// blob bytes exist.
func (s *Service) CreateDownloadSession(ctx context.Context, fileURI files.FileURI, by string) (*files.FileTransferSession, error) {
	rel, err := s.resolver.ObjectPath(fileURI)
	if err != nil {
		return nil, err
	}
	exists, err := s.blobs.Exists(ctx, rel)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBlobNotFound
	}
	now := s.clock.Now()
	sess, err := files.NewDownloadSession(files.NewDownloadInput{
		FileURI:     fileURI,
		SessionULID: s.idgen.NewULID(),
		CreatedBy:   by,
		CreatedAt:   now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Save(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// OpenBlob returns a reader over the blob bytes for fileURI. It does NOT
// perform reachability authorization (D3-d).
func (s *Service) OpenBlob(ctx context.Context, fileURI files.FileURI) (io.ReadCloser, error) {
	rel, err := s.resolver.ObjectPath(fileURI)
	if err != nil {
		return nil, err
	}
	rc, err := s.blobs.Get(ctx, rel)
	if errors.Is(err, blobstore.ErrBlobNotFound) {
		return nil, ErrBlobNotFound
	}
	return rc, err
}

// FindSessionByTransferURI returns the transfer session for transferURI (or
// ErrTransferSessionNotFound). It is a read-only passthrough used by the
// transport layer to verify session ownership before writing/finalizing a blob
// (the agent file tools check session.CreatedBy() == agent:<id>).
func (s *Service) FindSessionByTransferURI(ctx context.Context, transferURI string) (*files.FileTransferSession, error) {
	return s.sessions.FindByTransferURI(ctx, transferURI)
}

// CancelSession transitions an open session to canceled.
func (s *Service) CancelSession(ctx context.Context, transferURI string) error {
	return s.transition(ctx, transferURI, func(sess *files.FileTransferSession, at time.Time) error {
		return sess.Cancel(at)
	})
}

// ExpireSession transitions an open session to expired.
func (s *Service) ExpireSession(ctx context.Context, transferURI string) error {
	return s.transition(ctx, transferURI, func(sess *files.FileTransferSession, at time.Time) error {
		return sess.Expire(at)
	})
}

func (s *Service) transition(ctx context.Context, transferURI string, apply func(*files.FileTransferSession, time.Time) error) error {
	sess, err := s.sessions.FindByTransferURI(ctx, transferURI)
	if err != nil {
		return err
	}
	if err := apply(sess, s.clock.Now()); err != nil {
		return err
	}
	return s.sessions.Update(ctx, sess)
}
