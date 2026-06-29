package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// CodeRepo reference CRUD (v2.18.4 BE-1, issue-f980c8de). A project references a
// workspace coderepo.Repo (repo_id) — or carries a legacy url-only ref — and may
// flag ONE of them primary (the merge-check resolver reads the primary). All are
// project-member-gated; the workspace Repo + its credentials are org-admin-gated
// (enforced at the API layer). These methods own only the REFERENCE side.

// AddCodeRepoReferenceCommand adds a reference to a project. Provide RepoID (a
// workspace Repo reference) and/or URL (a legacy url-only ref) — at least one.
type AddCodeRepoReferenceCommand struct {
	ProjectID pm.ProjectID
	RepoID    string
	URL       string
	Label     string
	IsPrimary bool
	Actor     pm.IdentityRef
}

// AddCodeRepoReference creates a project↔repo reference. When IsPrimary, it becomes
// the sole primary (others demoted in the same tx).
func (s *Service) AddCodeRepoReference(ctx context.Context, cmd AddCodeRepoReferenceCommand) (string, error) {
	now := s.clock.Now()
	id := s.idgen.NewEntityID("reporef")
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.Actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, cmd.ProjectID); err != nil {
			return err
		}
		ref, err := pm.NewCodeRepoRef(pm.NewCodeRepoRefInput{
			ID: id, ProjectID: cmd.ProjectID, URL: cmd.URL, Label: cmd.Label,
			AddedBy: cmd.Actor, CreatedAt: now, RepoID: cmd.RepoID, IsPrimary: cmd.IsPrimary,
		})
		if err != nil {
			return err
		}
		if err := s.codeRepoRefs.Save(txCtx, ref); err != nil {
			return err
		}
		if cmd.IsPrimary {
			return s.codeRepoRefs.ClearPrimaryForProject(txCtx, cmd.ProjectID, id)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// RemoveCodeRepoReference deletes a project↔repo reference (project-member-gated).
func (s *Service) RemoveCodeRepoReference(ctx context.Context, projectID pm.ProjectID, refID string, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, projectID, actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, projectID); err != nil {
			return err
		}
		ref, err := s.codeRepoRefs.FindByID(txCtx, refID)
		if err != nil {
			return err
		}
		if ref.ProjectID() != projectID {
			return pm.ErrCodeRepoRefNotFound // do not leak refs across projects
		}
		return s.codeRepoRefs.Delete(txCtx, refID)
	})
}

// SetPrimaryCodeRepo marks refID as the project's primary repo and demotes the rest
// (at-most-one-primary). project-member-gated.
func (s *Service) SetPrimaryCodeRepo(ctx context.Context, projectID pm.ProjectID, refID string, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, projectID, actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, projectID); err != nil {
			return err
		}
		ref, err := s.codeRepoRefs.FindByID(txCtx, refID)
		if err != nil {
			return err
		}
		if ref.ProjectID() != projectID {
			return pm.ErrCodeRepoRefNotFound
		}
		ref.SetPrimary(true)
		if err := s.codeRepoRefs.Update(txCtx, ref); err != nil {
			return err
		}
		return s.codeRepoRefs.ClearPrimaryForProject(txCtx, projectID, refID)
	})
}
