package files

import (
	"context"
	"time"
)

// FileTransferSessionRepository is the persistence seam for the transfer-session
// AR. Sessions are created once (Save) and then mutated in place (Update) as
// they move through the state machine. Lookups are by id or transfer URI; the
// D3-c GC scans expired-but-open sessions via ListExpired.
type FileTransferSessionRepository interface {
	// Save inserts a new session row.
	Save(ctx context.Context, s *FileTransferSession) error
	// Update overwrites the mutable fields of an existing session (status,
	// sha256, size). ErrTransferSessionNotFound if the row is absent.
	Update(ctx context.Context, s *FileTransferSession) error
	// FindByID returns one session or ErrTransferSessionNotFound.
	FindByID(ctx context.Context, id string) (*FileTransferSession, error)
	// FindByTransferURI returns one session by its transfer URI or
	// ErrTransferSessionNotFound.
	FindByTransferURI(ctx context.Context, transferURI string) (*FileTransferSession, error)
	// ListExpired returns open sessions whose expiresAt is strictly before the
	// given instant (the D3-c GC reaps these + their partial blobs).
	ListExpired(ctx context.Context, before time.Time) ([]*FileTransferSession, error)
}
