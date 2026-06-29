// Package coderepo is the workspace CodeRepo bounded context (v2.18.4 BE-1,
// issue-f980c8de): a code repository promoted to a WORKSPACE-level (org-scoped)
// entity, parallel to Projects/Issues/Tasks/Plans. It holds repository INFORMATION
// (label/url/provider/default branch) plus an encrypted-at-rest credential. It is
// NOT a VCS integration — provider viewing (commits/branches) and the agent MCP
// surface are BE-2; this BC is the data base + CRUD + credential storage.
//
// Credentials live ONLY here (the project side references a Repo, storing no
// url/credential). The plaintext credential NEVER lives on the aggregate: the
// service encrypts it (AES-GCM via the secretmgmt master key) and the AR carries
// only the ciphertext + nonce. The API masks it and never returns plaintext.
package coderepo

import (
	"errors"
	"strings"
	"time"
)

// IdentityRef mirrors the actor ref shape used across BCs ("user:<id>" / "agent:<id>"
// / "system"). Kept local so this BC does not import another for a string alias.
type IdentityRef string

// Provider is the closed set of repository hosting providers BE-1 understands. The
// value drives BE-2's viewing adapter selection (github → go-github; gitlab/git →
// git ls-remote fallback).
type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
	ProviderGit    Provider = "git" // generic / self-hosted (ls-remote)
)

// IsValid reports provider-enum membership.
func (p Provider) IsValid() bool {
	switch p {
	case ProviderGitHub, ProviderGitLab, ProviderGit:
		return true
	}
	return false
}

// Sentinel errors.
var (
	ErrRepoNotFound      = errors.New("coderepo: repo not found")
	ErrLabelRequired     = errors.New("coderepo: label required")
	ErrURLRequired       = errors.New("coderepo: url required")
	ErrInvalidProvider   = errors.New("coderepo: invalid provider (must be github|gitlab|git)")
	ErrOrgRequired       = errors.New("coderepo: organization_id required")
	ErrVersionConflict   = errors.New("coderepo: version conflict (optimistic lock)")
	ErrCredentialMissing = errors.New("coderepo: no credential configured")
)

// Repo is the workspace CodeRepo aggregate.
type Repo struct {
	id            string
	orgID         string
	label         string
	description   string
	url           string
	provider      Provider
	defaultBranch string
	// credentialCiphertext/Nonce are the AES-GCM encrypted credential (empty = none).
	// The plaintext is never held on the aggregate.
	credentialCiphertext []byte
	credentialNonce      []byte
	createdBy            IdentityRef
	createdAt            time.Time
	updatedAt            time.Time
	version              int
}

// NewRepoInput is the constructor input.
type NewRepoInput struct {
	ID            string
	OrgID         string
	Label         string
	Description   string
	URL           string
	Provider      Provider
	DefaultBranch string
	CreatedBy     IdentityRef
	CreatedAt     time.Time
}

// NewRepo constructs a fresh workspace Repo (no credential yet — set it via
// SetCredential after the service encrypts the plaintext).
func NewRepo(in NewRepoInput) (*Repo, error) {
	if strings.TrimSpace(in.OrgID) == "" {
		return nil, ErrOrgRequired
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("coderepo: id required")
	}
	if strings.TrimSpace(in.Label) == "" {
		return nil, ErrLabelRequired
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, ErrURLRequired
	}
	if !in.Provider.IsValid() {
		return nil, ErrInvalidProvider
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("coderepo: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Repo{
		id:            in.ID,
		orgID:         in.OrgID,
		label:         strings.TrimSpace(in.Label),
		description:   in.Description,
		url:           strings.TrimSpace(in.URL),
		provider:      in.Provider,
		defaultBranch: strings.TrimSpace(in.DefaultBranch),
		createdBy:     in.CreatedBy,
		createdAt:     at,
		updatedAt:     at,
		version:       1,
	}, nil
}

// RehydrateRepoInput is for repository round-trip.
type RehydrateRepoInput struct {
	ID                   string
	OrgID                string
	Label                string
	Description          string
	URL                  string
	Provider             Provider
	DefaultBranch        string
	CredentialCiphertext []byte
	CredentialNonce      []byte
	CreatedBy            IdentityRef
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// RehydrateRepo reconstructs a Repo from storage without invariant checks.
func RehydrateRepo(in RehydrateRepoInput) *Repo {
	return &Repo{
		id:                   in.ID,
		orgID:                in.OrgID,
		label:                in.Label,
		description:          in.Description,
		url:                  in.URL,
		provider:             in.Provider,
		defaultBranch:        in.DefaultBranch,
		credentialCiphertext: in.CredentialCiphertext,
		credentialNonce:      in.CredentialNonce,
		createdBy:            in.CreatedBy,
		createdAt:            in.CreatedAt.UTC(),
		updatedAt:            in.UpdatedAt.UTC(),
		version:              in.Version,
	}
}

// UpdateInfo edits the non-credential fields (validated like NewRepo) and bumps the
// version. Credential edits go through SetCredential / ClearCredential.
func (r *Repo) UpdateInfo(label, description, url string, provider Provider, defaultBranch string, at time.Time) error {
	if strings.TrimSpace(label) == "" {
		return ErrLabelRequired
	}
	if strings.TrimSpace(url) == "" {
		return ErrURLRequired
	}
	if !provider.IsValid() {
		return ErrInvalidProvider
	}
	r.label = strings.TrimSpace(label)
	r.description = description
	r.url = strings.TrimSpace(url)
	r.provider = provider
	r.defaultBranch = strings.TrimSpace(defaultBranch)
	r.touch(at)
	return nil
}

// SetCredential stores the pre-encrypted credential (ciphertext + nonce). The
// service is responsible for the encryption; the AR never sees plaintext.
func (r *Repo) SetCredential(ciphertext, nonce []byte, at time.Time) {
	r.credentialCiphertext = ciphertext
	r.credentialNonce = nonce
	r.touch(at)
}

// ClearCredential removes the stored credential.
func (r *Repo) ClearCredential(at time.Time) {
	r.credentialCiphertext = nil
	r.credentialNonce = nil
	r.touch(at)
}

func (r *Repo) touch(at time.Time) {
	r.updatedAt = at.UTC()
	r.version++
}

// Getters.
func (r *Repo) ID() string                   { return r.id }
func (r *Repo) OrgID() string                { return r.orgID }
func (r *Repo) Label() string                { return r.label }
func (r *Repo) Description() string          { return r.description }
func (r *Repo) URL() string                  { return r.url }
func (r *Repo) Provider() Provider           { return r.provider }
func (r *Repo) DefaultBranch() string        { return r.defaultBranch }
func (r *Repo) CredentialCiphertext() []byte { return r.credentialCiphertext }
func (r *Repo) CredentialNonce() []byte      { return r.credentialNonce }
func (r *Repo) HasCredential() bool          { return len(r.credentialCiphertext) > 0 }
func (r *Repo) CreatedBy() IdentityRef       { return r.createdBy }
func (r *Repo) CreatedAt() time.Time         { return r.createdAt }
func (r *Repo) UpdatedAt() time.Time         { return r.updatedAt }
func (r *Repo) Version() int                 { return r.version }
