package cli

import (
	"context"
	"errors"
	"strings"

	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	remindersqlite "github.com/oopslink/agent-center/internal/cognition/reminder/sqlite"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// reminderDirectory is the concrete cognition Directory: it resolves the
// cross-project guard context (design 03-reminder.md Invariant #2) from pm
// project membership. A `user:` creator is treated as an owner (may cross
// projects + manage any reminder); an `agent:` creator is gated to a project it
// shares with the remindee.
type reminderDirectory struct{ pmSvc *pmservice.Service }

func (d *reminderDirectory) IsOwner(_ context.Context, ref string) bool {
	return strings.HasPrefix(ref, "user:")
}

// ResolveReminderContext finds the project the reminder lives in (one the remindee
// is a member of) within orgID, and — for an agent creator — confirms the creator
// shares that project so the guard passes. Owner (user) creators bypass the guard.
func (d *reminderDirectory) ResolveReminderContext(ctx context.Context, orgID, creatorRef, remindeeAgentID string) (cogservice.ReminderContext, error) {
	if d.pmSvc == nil {
		return cogservice.ReminderContext{}, errors.New("cognition: pm not wired")
	}
	remindeeRef := "agent:" + remindeeAgentID
	owner := d.IsOwner(ctx, creatorRef)
	projects, err := d.pmSvc.ListProjects(ctx, orgID)
	if err != nil {
		return cogservice.ReminderContext{}, err
	}
	var remindeeProject string // a project the remindee is in (fallback when no shared one)
	for _, p := range projects {
		members, merr := d.pmSvc.ListMembers(ctx, p.ID())
		if merr != nil {
			return cogservice.ReminderContext{}, merr
		}
		remindeeIn, creatorIn := false, false
		for _, m := range members {
			switch string(m.IdentityID()) {
			case remindeeRef:
				remindeeIn = true
			case creatorRef:
				creatorIn = true
			}
		}
		if !remindeeIn {
			continue
		}
		remindeeProject = string(p.ID())
		// A project shared by creator + remindee → guard passes cleanly.
		if creatorIn {
			return cogservice.ReminderContext{
				OrganizationID: orgID, ProjectID: remindeeProject,
				CreatorProjectID: remindeeProject,
			}, nil
		}
		if owner {
			return cogservice.ReminderContext{
				OrganizationID: orgID, ProjectID: remindeeProject, CreatorIsOwner: true,
			}, nil
		}
	}
	if remindeeProject == "" {
		return cogservice.ReminderContext{}, errors.New("cognition: remindee is not a member of any project in this org")
	}
	// remindee found but the agent creator shares no project → let the aggregate
	// reject with ErrCrossProjectReminder (CreatorProjectID left empty ≠ ProjectID).
	return cogservice.ReminderContext{
		OrganizationID: orgID, ProjectID: remindeeProject, CreatorIsOwner: owner,
	}, nil
}

// buildReminderService constructs the Reminder AppService from the App's wired
// deps. Returns nil if the prerequisites are missing (handlers then degrade to
// reminder_not_wired). The sqlite repo + EventSink + idgen + clock back it.
func buildReminderService(a *App) *cogservice.ReminderAppService {
	if a == nil || a.DB == nil || a.PMService == nil || a.Sink == nil || a.IDGen == nil {
		return nil
	}
	repo := remindersqlite.NewReminderRepo(a.DB)
	dir := &reminderDirectory{pmSvc: a.PMService}
	return cogservice.NewReminderAppService(a.DB, repo, dir, a.Sink, a.IDGen, a.Clock)
}
