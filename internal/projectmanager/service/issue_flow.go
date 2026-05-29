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
