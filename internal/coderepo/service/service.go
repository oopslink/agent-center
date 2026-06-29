// Package service is the workspace CodeRepo application service (v2.18.4 BE-1):
// CRUD over the Repo aggregate plus credential encryption (AES-GCM via the
// secretmgmt master key). It NEVER returns plaintext credentials — callers read a
// masked view (HasCredential) only.
package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/coderepo"
	"github.com/oopslink/agent-center/internal/coderepo/provider"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// ErrViewingNotConfigured is returned by the remote-viewing reads when no provider
// is wired (the viewing layer is optional — a center without it serves static
// repo info only).
var ErrViewingNotConfigured = errors.New("coderepo: remote viewing not configured")

// RefUnlinker breaks every project reference to a workspace Repo (used when the
// Repo is deleted): it clears repo_id + is_primary on all pm_code_repo_refs pointing
// at repoID, and counts the distinct projects affected (for the delete-confirm
// prompt). Implemented by a projectmanager-side adapter so this BC does not import
// projectmanager. Both methods run inside the caller's tx when one is present.
type RefUnlinker interface {
	CountReferencingProjects(ctx context.Context, repoID string) (int, error)
	UnlinkRepoEverywhere(ctx context.Context, repoID string) (int, error)
}

// Deps wires the service.
type Deps struct {
	DB        *sql.DB
	Repos     coderepo.RepoRepository
	IDGen     idgen.Generator
	Clock     clock.Clock
	MasterKey *secretmgmt.MasterKey // nil ⇒ credential writes fail with ErrMasterKeyNotLoaded
	// Unlinker is OPTIONAL (nil-safe): when nil, DeleteRepo deletes the Repo without
	// touching project references (they keep a now-dangling repo_id that resolves to
	// the fallback). When wired, DeleteRepo strong-deletes + unrefs atomically.
	Unlinker RefUnlinker
	// Providers is the OPTIONAL remote-viewing port (v2.18.4 BE-2): github + git
	// fallback behind a Factory. nil ⇒ ListCommits/ListBranches return
	// ErrViewingNotConfigured (the rest of the service still serves static info).
	Providers provider.Provider
}

// Service is the workspace CodeRepo application service.
type Service struct {
	db        *sql.DB
	repos     coderepo.RepoRepository
	idgen     idgen.Generator
	clock     clock.Clock
	masterKey *secretmgmt.MasterKey
	unlinker  RefUnlinker
	providers provider.Provider
}

// New constructs the service.
func New(d Deps) *Service {
	return &Service{db: d.DB, repos: d.Repos, idgen: d.IDGen, clock: d.Clock, masterKey: d.MasterKey, unlinker: d.Unlinker, providers: d.Providers}
}

func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	return persistence.RunInTx(ctx, s.db, fn)
}

// CreateRepoCommand is the create input. Credential is the OPTIONAL plaintext secret
// (token / deploy key); empty = no credential. It is encrypted before storage and
// never retained in plaintext.
type CreateRepoCommand struct {
	OrgID         string
	Label         string
	Description   string
	URL           string
	Provider      coderepo.Provider
	DefaultBranch string
	Credential    string
	CreatedBy     coderepo.IdentityRef
}

// CreateRepo creates a workspace Repo, encrypting the credential when provided.
func (s *Service) CreateRepo(ctx context.Context, cmd CreateRepoCommand) (string, error) {
	now := s.clock.Now()
	repo, err := coderepo.NewRepo(coderepo.NewRepoInput{
		ID: s.idgen.NewEntityID("repo"), OrgID: cmd.OrgID, Label: cmd.Label, Description: cmd.Description,
		URL: cmd.URL, Provider: cmd.Provider, DefaultBranch: cmd.DefaultBranch, CreatedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	if cmd.Credential != "" {
		ct, nonce, eerr := s.encrypt(cmd.Credential)
		if eerr != nil {
			return "", eerr
		}
		repo.SetCredential(ct, nonce, now) // bumps version to 2; harmless for a fresh row
	}
	if err := s.repos.Save(ctx, repo); err != nil {
		return "", err
	}
	return repo.ID(), nil
}

// UpdateRepoCommand edits a Repo. Credential is tri-state: nil = leave unchanged,
// non-nil empty string = CLEAR the credential, non-nil non-empty = replace it.
type UpdateRepoCommand struct {
	ID            string
	Label         string
	Description   string
	URL           string
	Provider      coderepo.Provider
	DefaultBranch string
	Credential    *string
}

// UpdateRepo edits a Repo's info and (optionally) its credential.
func (s *Service) UpdateRepo(ctx context.Context, cmd UpdateRepoCommand) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		repo, err := s.repos.FindByID(txCtx, cmd.ID)
		if err != nil {
			return err
		}
		if err := repo.UpdateInfo(cmd.Label, cmd.Description, cmd.URL, cmd.Provider, cmd.DefaultBranch, now); err != nil {
			return err
		}
		if cmd.Credential != nil {
			if *cmd.Credential == "" {
				repo.ClearCredential(now)
			} else {
				ct, nonce, eerr := s.encrypt(*cmd.Credential)
				if eerr != nil {
					return eerr
				}
				repo.SetCredential(ct, nonce, now)
			}
		}
		return s.repos.Update(txCtx, repo)
	})
}

// GetRepo returns one Repo (masked credential — the AR carries only ciphertext).
func (s *Service) GetRepo(ctx context.Context, id string) (*coderepo.Repo, error) {
	return s.repos.FindByID(ctx, id)
}

// ListRepos returns the workspace's Repos.
func (s *Service) ListRepos(ctx context.Context, orgID string) ([]*coderepo.Repo, error) {
	return s.repos.ListByOrg(ctx, orgID)
}

// RepoURL resolves a Repo's url by id — the projectmanager CodeRepoResolver port
// (merge-check primaryRepoURL). An unknown repo returns ("", nil) so a lookup miss
// falls back to the ref's own url rather than failing the merge check.
func (s *Service) RepoURL(ctx context.Context, repoID string) (string, error) {
	repo, err := s.repos.FindByID(ctx, repoID)
	if errors.Is(err, coderepo.ErrRepoNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return repo.URL(), nil
}

// RepoOrg returns the workspace Repo's owning org and whether it exists — the
// projectmanager CodeRepoResolver port backing AddCodeRepoReference's existence +
// same-org guard. found=false (not an error) for an unknown repo.
func (s *Service) RepoOrg(ctx context.Context, repoID string) (string, bool, error) {
	repo, err := s.repos.FindByID(ctx, repoID)
	if errors.Is(err, coderepo.ErrRepoNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return repo.OrgID(), true, nil
}

// CountReferencingProjects returns how many projects reference the Repo — the
// number the delete-confirm prompt shows ("解除 N 个项目引用"). 0 when no unlinker
// is wired.
func (s *Service) CountReferencingProjects(ctx context.Context, repoID string) (int, error) {
	if s.unlinker == nil {
		return 0, nil
	}
	return s.unlinker.CountReferencingProjects(ctx, repoID)
}

// DeleteRepo strong-deletes a Repo: it unrefs every project reference (clearing
// repo_id + is_primary) AND deletes the Repo row (clearing its credential) in ONE
// tx. Returns the number of projects whose reference was unlinked.
func (s *Service) DeleteRepo(ctx context.Context, repoID string) (int, error) {
	var unlinked int
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		if s.unlinker != nil {
			n, uerr := s.unlinker.UnlinkRepoEverywhere(txCtx, repoID)
			if uerr != nil {
				return uerr
			}
			unlinked = n
		}
		return s.repos.Delete(txCtx, repoID)
	})
	return unlinked, err
}

// encrypt wraps the master-key AES-GCM seal; a nil master key surfaces
// ErrMasterKeyNotLoaded so a credential write fails loudly rather than storing
// plaintext.
func (s *Service) encrypt(plaintext string) (ciphertext, nonce []byte, err error) {
	if s.masterKey == nil {
		return nil, nil, secretmgmt.ErrMasterKeyNotLoaded
	}
	return s.masterKey.Encrypt([]byte(plaintext))
}

// decrypt reverses encrypt for the remote-viewing reads. It is unexported and used
// only to build a provider.Target — the plaintext NEVER leaves the service (no API
// / MCP surface returns it).
func (s *Service) decrypt(ciphertext, nonce []byte) (string, error) {
	if s.masterKey == nil {
		return "", secretmgmt.ErrMasterKeyNotLoaded
	}
	pt, err := s.masterKey.Decrypt(ciphertext, nonce)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// viewTarget loads a Repo and builds the provider.Target, decrypting the credential
// in-memory for the fetch. The returned Target's credential is plaintext and must
// not be logged or returned to a caller.
func (s *Service) viewTarget(ctx context.Context, repoID string) (provider.Target, error) {
	repo, err := s.repos.FindByID(ctx, repoID)
	if err != nil {
		return provider.Target{}, err
	}
	cred := ""
	if repo.HasCredential() {
		pt, derr := s.decrypt(repo.CredentialCiphertext(), repo.CredentialNonce())
		if derr != nil {
			return provider.Target{}, derr
		}
		cred = pt
	}
	return provider.Target{
		URL:           repo.URL(),
		Provider:      string(repo.Provider()),
		DefaultBranch: repo.DefaultBranch(),
		Credential:    cred,
	}, nil
}

// ListCommits returns up to `limit` recent commits on `branch` (empty → the repo's
// default branch) from the repo's remote, via the provider abstraction. Never
// returns or logs the credential.
func (s *Service) ListCommits(ctx context.Context, repoID, branch string, limit int) ([]provider.Commit, error) {
	if s.providers == nil {
		return nil, ErrViewingNotConfigured
	}
	t, err := s.viewTarget(ctx, repoID)
	if err != nil {
		return nil, err
	}
	return s.providers.ListCommits(ctx, t, branch, limit)
}

// ListBranches returns the repo's remote branches via the provider abstraction.
func (s *Service) ListBranches(ctx context.Context, repoID string) ([]provider.Branch, error) {
	if s.providers == nil {
		return nil, ErrViewingNotConfigured
	}
	t, err := s.viewTarget(ctx, repoID)
	if err != nil {
		return nil, err
	}
	return s.providers.ListBranches(ctx, t)
}
