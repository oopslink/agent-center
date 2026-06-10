package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TransitionIssue moves an Issue to a new status, enforcing the B1 Issue state
// machine (open→in_progress→resolved→closed / open·in_progress→withdrawn /
// resolved·closed→reopened→open). Pure PM-state + outbox (OQ1): it emits
// pm.issue.state_changed so downstream projectors keep the issue Conversation
// consistent (participant set is unchanged by a state move; a system-message
// projection can hang off this event later).
func (s *Service) TransitionIssue(ctx context.Context, issueID pm.IssueID, to pm.IssueStatus, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		if err := i.Transition(to, now); err != nil {
			return err
		}
		if err := s.issues.Update(txCtx, i); err != nil {
			return err
		}
		return s.emit(txCtx, EvtIssueStateChanged,
			refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
			issueEventPayload{
				IssueID: string(i.ID()), ProjectID: string(i.ProjectID()),
				OwnerRef: "pm://issues/" + string(i.ID()), Status: string(i.Status()),
			})
	})
}

// SetIssueStatus sets the Issue to any VALID status with NO adjacency enforcement
// (v2.8.1 @oopslink: state = self-reported progress; the center does not gate
// transitions — the Change-status menu offers the full enum). Project-member
// gated; emits pm.issue.state_changed (generic).
func (s *Service) SetIssueStatus(ctx context.Context, issueID pm.IssueID, target pm.IssueStatus, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		if err := i.SetStatus(target, now); err != nil {
			return err
		}
		if err := s.issues.Update(txCtx, i); err != nil {
			return err
		}
		return s.emit(txCtx, EvtIssueStateChanged,
			refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
			issueEventPayload{
				IssueID: string(i.ID()), ProjectID: string(i.ProjectID()),
				OwnerRef: "pm://issues/" + string(i.ID()), Status: string(i.Status()),
			})
	})
}

// BatchIssuePatch is the dirty-only patch for BatchUpdateIssue. Issues have NO
// assignee (they are never assignable), so unlike BatchTaskPatch there is no
// Assignee field — editable issue fields are title/description/status/tags.
type BatchIssuePatch struct {
	Status      *string
	Tags        *[]string
	Title       *string
	Description *string
}

// BatchUpdateIssue applies any subset of {title, description, status, tags} to an
// Issue in a SINGLE tx — all-or-none (if any field's mutation errors, the tx rolls
// back and nothing is applied). Project-member gated. The Issue analogue of
// BatchUpdateTask (#232); it lets the Edit-Issue modal save every field atomically
// so the issue-detail sidebar can drop its per-field inline editors (v2.8.1
// @oopslink edit-consolidation). Emits EvtIssueStateChanged (a tags/title-only edit
// still bumps version + re-emits; harmless/idempotent for the effective set).
func (s *Service) BatchUpdateIssue(ctx context.Context, issueID pm.IssueID, patch BatchIssuePatch, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		if patch.Title != nil {
			if err := i.Rename(*patch.Title, now); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			i.SetDescription(*patch.Description, now)
		}
		if patch.Status != nil {
			if err := i.SetStatus(pm.IssueStatus(*patch.Status), now); err != nil {
				return err
			}
		}
		if patch.Tags != nil {
			if err := i.SetTags(*patch.Tags, now); err != nil {
				return err
			}
		}
		if err := s.issues.Update(txCtx, i); err != nil {
			return err
		}
		return s.emit(txCtx, EvtIssueStateChanged,
			refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
			issueEventPayload{
				IssueID: string(i.ID()), ProjectID: string(i.ProjectID()),
				OwnerRef: "pm://issues/" + string(i.ID()), Status: string(i.Status()),
			})
	})
}
