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
)

// IsValid reports whether s is one of the six known scopes.
func (s FileScope) IsValid() bool {
	switch s {
	case ScopeTask, ScopeIssue, ScopeProject, ScopeConversation, ScopeAgent, ScopeTmp:
		return true
	default:
		return false
	}
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
