package service

import (
	"context"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Task/Issue metadata-edit AppServices (B3-b prerequisite). Editing a Task's or
// Issue's title/description is basic usability, NOT a state transition and has
// NO cross-BC effect (it does not change subscribers, assignment, or lifecycle),
// so these are pure PM-state writes — no outbox event (OQ1: outbox is only for
// cross-BC effects), mirroring UpdateProject. nil pointer = field unchanged.
//
// NOTE (deliberate minimal scope): the Task/Issue Conversation name was set from
// the title at creation by the ParticipantProjector; a later rename here does
// NOT propagate to that Conversation's name. Conversation-name sync on rename is
// a follow-up enhancement (would need a metadata-changed event + projector),
// out of scope for the minimal "let users fix the title" requirement.

// UpdateTaskCommand patches a Task's title/description/derived_from_issue
// (nil = unchanged). For DerivedFromIssue a non-nil pointer applies the value:
// "" CLEARS the link, a non-empty id (RE)LINKS it (T192 — editable after creation).
type UpdateTaskCommand struct {
	TaskID           pm.TaskID
	Title            *string
	Description      *string
	DerivedFromIssue *pm.IssueID // nil = unchanged; "" = clear; id = link (validated)
	Actor            pm.IdentityRef
}

// UpdateTask applies the metadata patch under the membership gate.
func (s *Service) UpdateTask(ctx context.Context, cmd UpdateTaskCommand) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, cmd.TaskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		// #297: reject task metadata edit on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		if cmd.Title != nil {
			if err := t.Rename(*cmd.Title, now); err != nil {
				return err
			}
		}
		if cmd.Description != nil {
			if err := t.SetDescription(*cmd.Description, now); err != nil {
				return err
			}
		}
		if cmd.DerivedFromIssue != nil {
			if err := s.applyDerivedFromIssue(txCtx, t, *cmd.DerivedFromIssue, now); err != nil {
				return err
			}
		}
		return s.tasks.Update(txCtx, t)
	})
}

// applyDerivedFromIssue sets (or clears) a task's derived_from_issue under the T192
// editable invariant: clearing ("") is always allowed; (re)linking a non-empty issue
// requires that issue to EXIST (ErrIssueNotFound otherwise) and belong to the SAME
// project as the task (ErrDerivedIssueProjectMismatch otherwise). It mutates t in
// place; the caller persists within its tx. Shared by UpdateTask + BatchUpdateTask.
func (s *Service) applyDerivedFromIssue(ctx context.Context, t *pm.Task, issueID pm.IssueID, now time.Time) error {
	if strings.TrimSpace(string(issueID)) != "" {
		iss, err := s.issues.FindByID(ctx, issueID)
		if err != nil {
			return err // pm.ErrIssueNotFound when missing
		}
		if iss.ProjectID() != t.ProjectID() {
			return pm.ErrDerivedIssueProjectMismatch
		}
	}
	return t.SetDerivedFromIssue(issueID, now)
}

// UpdateIssueCommand patches an Issue's title/description (nil = unchanged).
type UpdateIssueCommand struct {
	IssueID     pm.IssueID
	Title       *string
	Description *string
	Actor       pm.IdentityRef
}

// UpdateIssue applies the metadata patch under the membership gate.
func (s *Service) UpdateIssue(ctx context.Context, cmd UpdateIssueCommand) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, cmd.IssueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		// #297: reject issue metadata edit on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, i.ProjectID()); err != nil {
			return err
		}
		if cmd.Title != nil {
			if err := i.Rename(*cmd.Title, now); err != nil {
				return err
			}
		}
		if cmd.Description != nil {
			i.SetDescription(*cmd.Description, now)
		}
		return s.issues.Update(txCtx, i)
	})
}
