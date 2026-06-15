package service

// This file hosts the files reference-CRUD AppService methods and the
// reachability authorization PRIMITIVE (v2.7 D3-b, ADR-0048 §3/§4). References
// are the placement layer: a {scope, scope_id} -> file_uri link carrying the
// per-placement display metadata. They are append-only (soft-delete only).
//
// Reachable is mechanism only: it decides whether a blob is reachable from a
// supplied SET of caller scopes. It does NOT resolve which scopes a caller
// actually has — that policy (human = org/project member scopes, agent =
// own-domain scopes) is the upper layer's job (D3-d). Reachability is granted
// only by LIVE references (FindByURI filters deleted_at IS NULL), so a
// soft-deleted reference never grants access.

import (
	"context"

	"github.com/oopslink/agent-center/internal/files"
)

// ScopeRef is a placement coordinate: a {scope, scope_id} pair. Reachable
// takes a caller's accessible scopes as a slice of these.
type ScopeRef struct {
	Scope   files.FileScope
	ScopeID string
}

// AddReferenceCmd is the input to AddReference.
type AddReferenceCmd struct {
	FileURI     files.FileURI
	Scope       files.FileScope
	ScopeID     string
	Filename    string
	MimeType    string
	SizeBytes   int64
	DisplayName string
	CreatedBy   string
}

// AddReference creates a new placement reference to a blob and persists it.
//
// References are append-only: multiple references to the same blob from
// different (or even the same) scopes are expected — sharing a file means
// adding a reference, never copying the blob (ADR-0048 §3). This method does
// NOT dedup.
func (s *Service) AddReference(ctx context.Context, cmd AddReferenceCmd) (files.FileReference, error) {
	ref := files.FileReference{
		ID:          s.idgen.NewULID(),
		FileURI:     cmd.FileURI,
		Scope:       cmd.Scope,
		ScopeID:     cmd.ScopeID,
		Filename:    cmd.Filename,
		MimeType:    cmd.MimeType,
		SizeBytes:   cmd.SizeBytes,
		DisplayName: cmd.DisplayName,
		CreatedBy:   cmd.CreatedBy,
		CreatedAt:   s.clock.Now(),
		DeletedAt:   nil, // live
	}
	if err := s.refs.Save(ctx, ref); err != nil {
		return files.FileReference{}, err
	}
	return ref, nil
}

// SoftDeleteReference soft-deletes a reference by id (idempotent; only live
// rows are affected). The backing blob is reaped later by the phase-D GC once
// its live-reference count hits zero (D3-c).
func (s *Service) SoftDeleteReference(ctx context.Context, refID string) error {
	return s.refs.SoftDelete(ctx, refID, s.clock.Now())
}

// Reachable is the reachability authorization PRIMITIVE: it reports whether the
// blob at fileURI is reachable from any of the supplied callerScopes.
//
// It returns true iff at least one LIVE reference to the blob has a
// {Scope, ScopeID} present in callerScopes. Because it walks FindByURI (which
// filters deleted_at IS NULL), a soft-deleted reference NEVER grants
// reachability. An empty callerScopes — or no live reference matching any
// supplied scope — yields false.
//
// This is mechanism only. Resolving the caller's accessible scopes (human vs.
// agent) is the upper layer's responsibility (D3-d); D3-b does not do it.
func (s *Service) Reachable(ctx context.Context, fileURI files.FileURI, callerScopes []ScopeRef) (bool, error) {
	if len(callerScopes) == 0 {
		return false, nil
	}
	refs, err := s.refs.FindByURI(ctx, fileURI)
	if err != nil {
		return false, err
	}
	allowed := make(map[ScopeRef]struct{}, len(callerScopes))
	for _, sr := range callerScopes {
		allowed[sr] = struct{}{}
	}
	for _, ref := range refs {
		if _, ok := allowed[ScopeRef{Scope: ref.Scope, ScopeID: ref.ScopeID}]; ok {
			return true, nil
		}
	}
	return false, nil
}

// ListReferences returns all LIVE references pointing at fileURI (a FindByURI
// passthrough), for inspection.
func (s *Service) ListReferences(ctx context.Context, fileURI files.FileURI) ([]files.FileReference, error) {
	return s.refs.FindByURI(ctx, fileURI)
}

// ListReferencesByScope returns all LIVE references placed in a {scope, scopeID}
// (a FindByScope passthrough). Used to enumerate the files attached to a task /
// issue / project. Authorization (who may see a scope's files) is the upper
// layer's job (D3-d) — this is mechanism only.
func (s *Service) ListReferencesByScope(ctx context.Context, scope files.FileScope, scopeID string) ([]files.FileReference, error) {
	return s.refs.FindByScope(ctx, scope, scopeID)
}
