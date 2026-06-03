package files

import (
	"context"
	"errors"
	"time"
)

// FileScope is where a blob is used. Identity (the FileURI) is separate from
// placement (this scope): one blob may be referenced from any number of
// scopes, and sharing a file means adding a reference, never copying the blob
// (ADR-0048 §3).
type FileScope string

const (
	ScopeTask         FileScope = "task"
	ScopeIssue        FileScope = "issue"
	ScopeProject      FileScope = "project"
	ScopeConversation FileScope = "conversation"
	ScopeAgent        FileScope = "agent"
	ScopeTmp          FileScope = "tmp"
	// ScopeUploader (v2.7 #142) is a SERVER-INTERNAL reachability scope: a
	// reference with Scope=uploader, ScopeID=<uploader identity ref> grants the
	// uploader (and ONLY that identity) reachability to the blob they uploaded.
	// The server creates it at upload-complete (gated on the session initiator);
	// it is deliberately EXCLUDED from IsValid so a client can never set it on an
	// upload request — uploader reachability is a server-derived fact, not a
	// client claim. The attach flow uses it to let a user reference a blob they
	// uploaded (fileReachableForHuman) before a conversation reference is made.
	ScopeUploader FileScope = "uploader"
)

// IsValid reports whether s is a known, persistable reference scope (incl. the
// server-internal ScopeUploader). NOTE: ScopeUploader is server-internal — it is
// a valid scope for a server-created reference, but the webconsole upload handler
// rejects a CLIENT-supplied scope=uploader (see createUploadHandler) so uploader
// reachability can never be claimed by a client; it is always server-derived.
func (s FileScope) IsValid() bool {
	switch s {
	case ScopeTask, ScopeIssue, ScopeProject, ScopeConversation, ScopeAgent, ScopeTmp, ScopeUploader:
		return true
	default:
		return false
	}
}

// IsClientSettable reports whether a client may set this scope on an upload
// request. ScopeUploader is excluded (server-internal — see IsValid).
func (s FileScope) IsClientSettable() bool {
	return s.IsValid() && s != ScopeUploader
}

// Sentinel errors.
var (
	ErrReferenceNotFound = errors.New("files: file reference not found")
	ErrInvalidScope      = errors.New("files: invalid file reference scope")
)

// FileReference links a placement {scope, scope_id} to a blob's FileURI and
// carries the per-placement display metadata (filename/mime/size/display
// name) — the SAME blob can appear under different names in different scopes,
// so this metadata lives here and not on the blob identity (ADR-0048 §3).
//
// Deletion is soft (DeletedAt set): removing a business object soft-deletes
// its references; the blob is physically reaped later by the phase-D GC once
// its live-reference count hits zero past a grace period (ADR-0048 §5).
type FileReference struct {
	ID          string // reference record ULID
	FileURI     FileURI
	Scope       FileScope
	ScopeID     string // e.g. the task/issue/conversation/agent id
	Filename    string
	MimeType    string
	SizeBytes   int64
	DisplayName string
	CreatedBy   string // IdentityRef
	CreatedAt   time.Time
	DeletedAt   *time.Time // nil = live
}

// IsLive reports whether the reference has not been soft-deleted.
func (r FileReference) IsLive() bool { return r.DeletedAt == nil }

// Validate enforces the minimal invariants for a reference record.
func (r FileReference) Validate() error {
	if err := r.FileURI.Validate(); err != nil {
		return err
	}
	if !r.Scope.IsValid() {
		return ErrInvalidScope
	}
	if r.ScopeID == "" {
		return errors.New("files: file reference scope_id is required")
	}
	return nil
}

// FileReferenceRepository is the placement-layer persistence seam. A0 ships
// the interface plus a SQLite implementation good enough for B/C/D to attach
// files; the phase-D GC consumes CountLiveByURI to decide reaping.
type FileReferenceRepository interface {
	// Save inserts a reference record (references are append-only; soft-delete
	// via SoftDelete, never hard update).
	Save(ctx context.Context, ref FileReference) error
	// FindByID returns one reference or ErrReferenceNotFound.
	FindByID(ctx context.Context, id string) (FileReference, error)
	// FindByScope lists live references attached to a {scope, scope_id}.
	FindByScope(ctx context.Context, scope FileScope, scopeID string) ([]FileReference, error)
	// FindByURI lists all live references pointing at a blob (reachability
	// authorization + GC both walk this).
	FindByURI(ctx context.Context, uri FileURI) ([]FileReference, error)
	// SoftDelete marks a reference deleted at t (idempotent).
	SoftDelete(ctx context.Context, id string, t time.Time) error
	// CountLiveByURI returns the number of live references to a blob; the
	// phase-D GC reaps when this is zero past the grace period.
	CountLiveByURI(ctx context.Context, uri FileURI) (int, error)
}
