package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// CreateProjectCommand creates a Project. The creator becomes an owner member
// in the same transaction (membership is the write-gate, OQ6). No Conversation
// is created — channels are Org-level and explicit (plan §10 OQ10).
type CreateProjectCommand struct {
	OrganizationID string
	Name           string
	Description    string
	CreatedBy      pm.IdentityRef
}

// CreateProject writes the Project + creator owner-member + outbox event.
func (s *Service) CreateProject(ctx context.Context, cmd CreateProjectCommand) (pm.ProjectID, error) {
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	p, err := pm.NewProject(pm.NewProjectInput{
		ID: pm.ProjectID(s.idgen.NewULID()), OrganizationID: cmd.OrganizationID,
		Name: cmd.Name, Description: cmd.Description, CreatedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	member, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(s.idgen.NewULID()), ProjectID: p.ID(), IdentityID: cmd.CreatedBy,
		Role: pm.RoleOwner, AddedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.projects.Save(txCtx, p); err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		return s.emit(txCtx, EvtProjectCreated,
			refsJSON(map[string]string{"project_id": string(p.ID())}),
			map[string]string{"project_id": string(p.ID()), "name": p.Name(), "created_by": string(cmd.CreatedBy)})
	})
	if err != nil {
		return "", err
	}
	return p.ID(), nil
}

// AddProjectMemberCommand adds a member to a project (actor must be a member).
type AddProjectMemberCommand struct {
	ProjectID  pm.ProjectID
	IdentityID pm.IdentityRef
	Role       pm.ProjectMemberRole
	Actor      pm.IdentityRef
}

// AddProjectMember writes the member + outbox event.
func (s *Service) AddProjectMember(ctx context.Context, cmd AddProjectMemberCommand) (pm.MemberID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	m, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(s.idgen.NewULID()), ProjectID: cmd.ProjectID, IdentityID: cmd.IdentityID,
		Role: cmd.Role, AddedBy: cmd.Actor, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.Actor); err != nil {
			return err
		}
		if err := s.members.Save(txCtx, m); err != nil {
			return err
		}
		return s.emit(txCtx, EvtMemberAdded,
			refsJSON(map[string]string{"project_id": string(cmd.ProjectID)}),
			map[string]string{"project_id": string(cmd.ProjectID), "identity_id": string(cmd.IdentityID), "role": string(m.Role())})
	})
	if err != nil {
		return "", err
	}
	return m.ID(), nil
}

// CreateIssueCommand creates an Issue.
type CreateIssueCommand struct {
	ProjectID   pm.ProjectID
	Title       string
	Description string
	CreatedBy   pm.IdentityRef
}

// CreateIssue writes the Issue + outbox pm.issue.created (the projector creates
// the issue Conversation owner_ref pm://issues/{id} + syncs participants).
func (s *Service) CreateIssue(ctx context.Context, cmd CreateIssueCommand) (pm.IssueID, error) {
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	i, err := pm.NewIssue(pm.NewIssueInput{
		ID: pm.IssueID(s.idgen.NewULID()), ProjectID: cmd.ProjectID, Title: cmd.Title,
		Description: cmd.Description, CreatedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.CreatedBy); err != nil {
			return err
		}
		proj, perr := s.projects.FindByID(txCtx, cmd.ProjectID)
		if perr != nil {
			return perr
		}
		if err := s.issues.Save(txCtx, i); err != nil {
			return err
		}
		return s.emit(txCtx, EvtIssueCreated,
			refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(cmd.ProjectID)}),
			issueEventPayload{
				IssueID: string(i.ID()), ProjectID: string(cmd.ProjectID),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://issues/" + string(i.ID()), Status: string(i.Status()),
				EffectiveSubscribers: EffectiveIssueSubscribers(i, nil),
			})
	})
	if err != nil {
		return "", err
	}
	return i.ID(), nil
}

// CreateTaskCommand creates a Task (independent or derived from an Issue).
type CreateTaskCommand struct {
	ProjectID        pm.ProjectID
	Title            string
	Description      string
	DerivedFromIssue pm.IssueID
	CreatedBy        pm.IdentityRef
}

// CreateTask writes the Task + outbox pm.task.created. The projector (B2-b)
// creates the task Conversation (owner_ref pm://tasks/{id}) + syncs
// participants to the effective subscriber set {creator}.
func (s *Service) CreateTask(ctx context.Context, cmd CreateTaskCommand) (pm.TaskID, error) {
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	t, err := pm.NewTask(pm.NewTaskInput{
		ID: pm.TaskID(s.idgen.NewULID()), ProjectID: cmd.ProjectID, Title: cmd.Title,
		Description: cmd.Description, DerivedFromIssue: cmd.DerivedFromIssue, CreatedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.CreatedBy); err != nil {
			return err
		}
		// The task Conversation must be stamped with the project's org so org-scoped
		// endpoints (incl. a human replying to a waiting_input agent → wake) resolve it.
		proj, perr := s.projects.FindByID(txCtx, cmd.ProjectID)
		if perr != nil {
			return perr
		}
		if err := s.tasks.Save(txCtx, t); err != nil {
			return err
		}
		return s.emit(txCtx, EvtTaskCreated,
			refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(cmd.ProjectID)}),
			taskEventPayload{
				TaskID: string(t.ID()), ProjectID: string(cmd.ProjectID),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://tasks/" + string(t.ID()), Status: string(t.Status()),
				EffectiveSubscribers: EffectiveTaskSubscribers(t, nil),
			})
	})
	if err != nil {
		return "", err
	}
	return t.ID(), nil
}

// SubscribeTask adds a MANUAL subscriber to a Task and re-emits the effective
// set. creator/assignee are derived (not rows) — subscribing them is a no-op
// on rows but they remain effective regardless.
func (s *Service) SubscribeTask(ctx context.Context, taskID pm.TaskID, identity, actor pm.IdentityRef) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	sub, err := pm.NewTaskSubscriber(taskID, identity, actor, now)
	if err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.taskSubs.Add(txCtx, sub); err != nil {
			return err
		}
		return s.emitTaskSubsChanged(txCtx, t)
	})
}

// UnsubscribeTask removes a MANUAL subscriber row. If identity is the creator
// or current assignee they remain effective (role-derived) — this only deletes
// a manual row, so "can't unsubscribe while role holds" is automatic.
func (s *Service) UnsubscribeTask(ctx context.Context, taskID pm.TaskID, identity, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.taskSubs.Remove(txCtx, taskID, identity); err != nil {
			return err
		}
		return s.emitTaskSubsChanged(txCtx, t)
	})
}

// emitTaskSubsChanged loads the manual rows, computes the effective set, and
// emits pm.task.subscribers_changed for the participant projector.
func (s *Service) emitTaskSubsChanged(ctx context.Context, t *pm.Task) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtTaskSubsChanged,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()), Status: string(t.Status()),
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}

// SubscribeIssue / UnsubscribeIssue mirror the Task variants for Issues.
func (s *Service) SubscribeIssue(ctx context.Context, issueID pm.IssueID, identity, actor pm.IdentityRef) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	sub, err := pm.NewIssueSubscriber(issueID, identity, actor, now)
	if err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.issueSubs.Add(txCtx, sub); err != nil {
			return err
		}
		return s.emitIssueSubsChanged(txCtx, i)
	})
}

func (s *Service) UnsubscribeIssue(ctx context.Context, issueID pm.IssueID, identity, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.issueSubs.Remove(txCtx, issueID, identity); err != nil {
			return err
		}
		return s.emitIssueSubsChanged(txCtx, i)
	})
}

func (s *Service) emitIssueSubsChanged(ctx context.Context, i *pm.Issue) error {
	manual, err := s.issueSubs.ListByIssue(ctx, i.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtIssueSubsChanged,
		refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
		issueEventPayload{
			IssueID: string(i.ID()), ProjectID: string(i.ProjectID()),
			OwnerRef: "pm://issues/" + string(i.ID()), Status: string(i.Status()),
			EffectiveSubscribers: EffectiveIssueSubscribers(i, manual),
		})
}
