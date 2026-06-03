package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Project mutation AppServices (B3-c — needed when the flat /api/projects PATCH
// + DELETE routes repoint to the pm Service). Project rename/describe/archive
// have NO cross-BC effect (a Project owns no Conversation), so these are pure
// PM-state writes — no outbox event needed (OQ1 outbox is only for cross-BC
// effects). Membership-gated like every project write.

// UpdateProjectCommand patches a Project's name/description (nil = unchanged).
type UpdateProjectCommand struct {
	ProjectID   pm.ProjectID
	Name        *string
	Description *string
	Actor       pm.IdentityRef
}

// UpdateProject applies the patch under the membership gate.
func (s *Service) UpdateProject(ctx context.Context, cmd UpdateProjectCommand) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.projects.FindByID(txCtx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ID(), cmd.Actor); err != nil {
			return err
		}
		if cmd.Name != nil {
			if err := p.Rename(*cmd.Name, now); err != nil {
				return err
			}
		}
		if cmd.Description != nil {
			p.SetDescription(*cmd.Description, now)
		}
		return s.projects.Update(txCtx, p)
	})
}

// ArchiveProject marks a Project archived (the DELETE /api/projects/{id} verb;
// v2.7 archives rather than hard-deletes).
func (s *Service) ArchiveProject(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.projects.FindByID(txCtx, projectID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ID(), actor); err != nil {
			return err
		}
		p.Archive(now)
		return s.projects.Update(txCtx, p)
	})
}
