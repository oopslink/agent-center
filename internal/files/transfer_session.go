package files

import (
	"errors"
	"time"
)

// Transfer URI scheme constants live alongside the file URI ones in
// file_uri.go (uriScheme/hostTrans/transPrefix). A transfer URI is
// `ac://transfers/{sessionID}` where sessionID is the FileTransferSession ULID.

// TransferDirection is the direction of a transfer session.
type TransferDirection string

const (
	// DirectionUpload is a client→server upload session: the FileURI is minted
	// fresh at create time and the blob bytes are written via the session.
	DirectionUpload TransferDirection = "upload"
	// DirectionDownload is a server→client download session over an EXISTING
	// FileURI.
	DirectionDownload TransferDirection = "download"
)

// IsValid reports whether d is a known direction.
func (d TransferDirection) IsValid() bool {
	return d == DirectionUpload || d == DirectionDownload
}

// TransferStatus is the lifecycle state of a transfer session.
type TransferStatus string

const (
	// StatusOpen is the initial state: the session may be written/read and
	// completed/canceled/expired.
	StatusOpen TransferStatus = "open"
	// StatusCompleted is the terminal success state (upload finalized / blob
	// available).
	StatusCompleted TransferStatus = "completed"
	// StatusCanceled is the terminal canceled state.
	StatusCanceled TransferStatus = "canceled"
	// StatusExpired is the terminal TTL-expired state.
	StatusExpired TransferStatus = "expired"
)

// DefaultTransferTTL is the lifetime of a freshly-created transfer session.
// After expiresAt the session may be Expire()d (D3-c GC reaps stale sessions
// + their partially-written blobs).
const DefaultTransferTTL = time.Hour

// Sentinel errors for the transfer-session AR + repo.
var (
	// ErrTransferSessionNotFound is returned by the repo when no session
	// matches the lookup.
	ErrTransferSessionNotFound = errors.New("files: transfer session not found")
	// ErrIllegalTransferState is returned by a state-machine method when the
	// transition is not allowed from the current status.
	ErrIllegalTransferState = errors.New("files: illegal transfer session state transition")
	// ErrInvalidTransferDirection is returned when a session carries an unknown
	// direction.
	ErrInvalidTransferDirection = errors.New("files: invalid transfer direction")
	// ErrEmptyTransferURI is returned when a transfer URI is empty.
	ErrEmptyTransferURI = errors.New("files: transfer uri is empty")
)

// FileTransferSession is the AR coordinating one upload or download of a blob
// (ADR-0048 §2/§6). Its id IS the transfer-session id and is carried in the
// transfer URI `ac://transfers/{id}`. For uploads the FileURI is minted at
// create time so the caller always holds the final reference up front; for
// downloads it points at an existing blob. The sha256 + final size are filled
// on Complete (upload integrity metadata).
type FileTransferSession struct {
	id          string // session ULID == transfer-session id
	fileURI     FileURI
	transferURI string // ac://transfers/{id}
	direction   TransferDirection
	status      TransferStatus
	contentType string
	size        int64
	sha256      string // set on Complete
	scope       FileScope
	scopeID     string
	createdBy   string // IdentityRef
	createdAt   time.Time
	expiresAt   time.Time
}

// NewUploadInput bundles the inputs for an upload session.
type NewUploadInput struct {
	// FileULID is the freshly-minted blob ULID (generate via idgen). The
	// FileURI becomes ac://files/{FileULID}.
	FileULID string
	// SessionULID is the freshly-minted transfer-session ULID. The transfer
	// URI becomes ac://transfers/{SessionULID}.
	SessionULID string
	ContentType string
	Size        int64
	Scope       FileScope // optional
	ScopeID     string    // optional
	CreatedBy   string
	CreatedAt   time.Time
	TTL         time.Duration // optional; <=0 ⇒ DefaultTransferTTL
}

// NewUploadSession mints a fresh upload session: a new FileURI
// (ac://files/{FileULID}) and transfer URI (ac://transfers/{SessionULID}),
// status open, expiring at CreatedAt+TTL. The caller always receives the final
// FileURI here (ADR-0048 §2).
func NewUploadSession(in NewUploadInput) (*FileTransferSession, error) {
	fileURI, err := NewFileURI(in.FileULID)
	if err != nil {
		return nil, err
	}
	if in.SessionULID == "" {
		return nil, ErrBadULID
	}
	transferURI := transPrefix + in.SessionULID
	ttl := in.TTL
	if ttl <= 0 {
		ttl = DefaultTransferTTL
	}
	if in.Scope != "" && !in.Scope.IsValid() {
		return nil, ErrInvalidScope
	}
	return &FileTransferSession{
		id:          in.SessionULID,
		fileURI:     fileURI,
		transferURI: transferURI,
		direction:   DirectionUpload,
		status:      StatusOpen,
		contentType: in.ContentType,
		size:        in.Size,
		scope:       in.Scope,
		scopeID:     in.ScopeID,
		createdBy:   in.CreatedBy,
		createdAt:   in.CreatedAt,
		expiresAt:   in.CreatedAt.Add(ttl),
	}, nil
}

// NewDownloadInput bundles the inputs for a download session over an existing
// blob.
type NewDownloadInput struct {
	FileURI     FileURI
	SessionULID string
	CreatedBy   string
	CreatedAt   time.Time
	TTL         time.Duration // optional; <=0 ⇒ DefaultTransferTTL
}

// NewDownloadSession builds a download session for an EXISTING FileURI, status
// open, expiring at CreatedAt+TTL.
func NewDownloadSession(in NewDownloadInput) (*FileTransferSession, error) {
	if err := in.FileURI.Validate(); err != nil {
		return nil, err
	}
	if in.SessionULID == "" {
		return nil, ErrBadULID
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = DefaultTransferTTL
	}
	return &FileTransferSession{
		id:          in.SessionULID,
		fileURI:     in.FileURI,
		transferURI: transPrefix + in.SessionULID,
		direction:   DirectionDownload,
		status:      StatusOpen,
		createdBy:   in.CreatedBy,
		createdAt:   in.CreatedAt,
		expiresAt:   in.CreatedAt.Add(ttl),
	}, nil
}

// Complete transitions an open session to completed, recording the final
// integrity sha256 + size. Only valid from open.
func (s *FileTransferSession) Complete(sha256 string, size int64, at time.Time) error {
	if s.status != StatusOpen {
		return ErrIllegalTransferState
	}
	s.status = StatusCompleted
	s.sha256 = sha256
	s.size = size
	_ = at // recorded implicitly; completion time not separately persisted in D3-a
	return nil
}

// Cancel transitions an open session to canceled. Only valid from open.
func (s *FileTransferSession) Cancel(at time.Time) error {
	if s.status != StatusOpen {
		return ErrIllegalTransferState
	}
	s.status = StatusCanceled
	_ = at
	return nil
}

// Expire transitions an open session to expired. Only valid from open.
func (s *FileTransferSession) Expire(at time.Time) error {
	if s.status != StatusOpen {
		return ErrIllegalTransferState
	}
	s.status = StatusExpired
	_ = at
	return nil
}

// IsExpired reports whether now is at/after the session's expiresAt. It is a
// pure time check and does not consult status.
func (s *FileTransferSession) IsExpired(now time.Time) bool {
	return !now.Before(s.expiresAt)
}

// --- getters ---

func (s *FileTransferSession) ID() string                   { return s.id }
func (s *FileTransferSession) FileURI() FileURI             { return s.fileURI }
func (s *FileTransferSession) TransferURI() string          { return s.transferURI }
func (s *FileTransferSession) Direction() TransferDirection { return s.direction }
func (s *FileTransferSession) Status() TransferStatus       { return s.status }
func (s *FileTransferSession) ContentType() string          { return s.contentType }
func (s *FileTransferSession) Size() int64                  { return s.size }
func (s *FileTransferSession) SHA256() string               { return s.sha256 }
func (s *FileTransferSession) Scope() FileScope             { return s.scope }
func (s *FileTransferSession) ScopeID() string              { return s.scopeID }
func (s *FileTransferSession) CreatedBy() string            { return s.createdBy }
func (s *FileTransferSession) CreatedAt() time.Time         { return s.createdAt }
func (s *FileTransferSession) ExpiresAt() time.Time         { return s.expiresAt }

// IsOpen reports whether the session is still open.
func (s *FileTransferSession) IsOpen() bool { return s.status == StatusOpen }

// RehydrateTransferSession reconstructs a session from persisted row values
// (repo round-trip). It does not validate transitions — the row is assumed to
// have been produced by a valid AR.
func RehydrateTransferSession(
	id string,
	fileURI FileURI,
	transferURI string,
	direction TransferDirection,
	status TransferStatus,
	contentType string,
	size int64,
	sha256 string,
	scope FileScope,
	scopeID string,
	createdBy string,
	createdAt time.Time,
	expiresAt time.Time,
) *FileTransferSession {
	return &FileTransferSession{
		id:          id,
		fileURI:     fileURI,
		transferURI: transferURI,
		direction:   direction,
		status:      status,
		contentType: contentType,
		size:        size,
		sha256:      sha256,
		scope:       scope,
		scopeID:     scopeID,
		createdBy:   createdBy,
		createdAt:   createdAt,
		expiresAt:   expiresAt,
	}
}
