// Package provider is the workspace CodeRepo remote-viewing layer (v2.18.4 BE-2,
// issue-f980c8de): read-only metadata (recent commits / branches) fetched from a
// repo's remote WITHOUT cloning. It is deliberately decoupled from the coderepo
// aggregate — callers pass a Target (url/provider/default-branch + an already
// DECRYPTED credential), so this package imports nothing from the coderepo BC and
// never touches encryption.
//
// Two adapters back the Provider port:
//   - GitHub (github provider): go-github REST — rich data, no clone.
//   - Git (everything else: gitlab / generic / self-hosted / unknown): a
//     `git ls-remote` (branches) + shallow-fetch `git log` (commits) fallback.
//
// A Factory dispatches on Target.Provider so the service holds a single Provider.
package provider

import (
	"context"
	"time"
)

// Commit is a provider-agnostic commit summary (viewing only — no diff / files).
type Commit struct {
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	Author      string    `json:"author"`
	AuthorEmail string    `json:"author_email,omitempty"`
	CommittedAt time.Time `json:"committed_at"`
	URL         string    `json:"url,omitempty"`
}

// Branch is a provider-agnostic branch summary.
type Branch struct {
	Name      string `json:"name"`
	CommitSHA string `json:"commit_sha"`
	IsDefault bool   `json:"is_default"`
}

// Target is the minimal repo descriptor a provider needs — decoupled from the
// coderepo aggregate. Credential is the PLAINTEXT token/PAT the service decrypted
// ("" = none → anonymous/public access). It NEVER leaves this layer (the API/MCP
// surfaces return only the fetched Commit/Branch data, never the credential).
type Target struct {
	URL           string
	Provider      string
	DefaultBranch string
	Credential    string
}

// DefaultCommitLimit / MaxCommitLimit bound the commit listing (a viewing surface,
// not a history export).
const (
	DefaultCommitLimit = 20
	MaxCommitLimit     = 100
)

// ClampLimit normalizes a requested commit limit into [1, MaxCommitLimit],
// defaulting a non-positive request to DefaultCommitLimit.
func ClampLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultCommitLimit
	case limit > MaxCommitLimit:
		return MaxCommitLimit
	}
	return limit
}

// Provider reads remote repository metadata WITHOUT cloning.
type Provider interface {
	// ListCommits returns up to `limit` most-recent commits on `branch` (empty →
	// the target's default branch).
	ListCommits(ctx context.Context, t Target, branch string, limit int) ([]Commit, error)
	// ListBranches returns the repo's branches (IsDefault set from the target's
	// default branch).
	ListBranches(ctx context.Context, t Target) ([]Branch, error)
}

// Factory dispatches to the github adapter for the "github" provider and to the
// git ls-remote fallback for everything else (gitlab / git / self-hosted /
// unknown). It implements Provider so the service holds one collaborator.
type Factory struct {
	github Provider
	git    Provider
}

// NewFactory wires the github adapter + the git fallback. Either may be nil for
// tests; For() then returns whichever is set (and the service guards nil).
func NewFactory(github, git Provider) *Factory {
	return &Factory{github: github, git: git}
}

// For selects the adapter for a provider string: github → the go-github adapter;
// anything else → the git ls-remote fallback (the cross-provider catch-all).
func (f *Factory) For(providerName string) Provider {
	if providerName == "github" && f.github != nil {
		return f.github
	}
	return f.git
}

// ListCommits dispatches by Target.Provider.
func (f *Factory) ListCommits(ctx context.Context, t Target, branch string, limit int) ([]Commit, error) {
	return f.For(t.Provider).ListCommits(ctx, t, branch, limit)
}

// ListBranches dispatches by Target.Provider.
func (f *Factory) ListBranches(ctx context.Context, t Target) ([]Branch, error) {
	return f.For(t.Provider).ListBranches(ctx, t)
}
