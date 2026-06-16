package cli

import (
	"context"
	"errors"
	"strings"
	"time"

	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	remindersqlite "github.com/oopslink/agent-center/internal/cognition/reminder/sqlite"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
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

// conversationDeliverer is the concrete cognition ReminderDeliverer (design §3.4,
// option A): on a fired reminder it opens a system↔remindee DM and posts the
// content as a `system` sender. The EXISTING WakeProjector then wakes the remindee
// (a DM message whose sender is not the agent wakes the agent participant). Anti-
// loop: the woken agent is the remindee only; cognition.reminder.fired is never on
// the supervisor self-wake allowlist.
type conversationDeliverer struct {
	writer *convservice.MessageWriter
	idGen  cogservice.IDGenerator
	clk    clockNow
}

// clockNow is the slice of clock.Clock the deliverer needs.
type clockNow interface{ Now() time.Time }

func (d *conversationDeliverer) Deliver(ctx context.Context, orgID, remindeeAgentID, content, _ string) error {
	remindeeRef := conversation.IdentityRef("agent:" + remindeeAgentID)
	now := d.clk.Now().UTC().Format(time.RFC3339Nano)
	res, err := d.writer.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		Name:           "Reminder",
		OrganizationID: orgID,
		Participants: []conversation.ParticipantElement{{
			IdentityID: remindeeRef, Role: "member", JoinedAt: now, JoinedBy: conversation.IdentityRef("system"),
		}},
		CreatedBy: conversation.IdentityRef("system"),
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		return err
	}
	_, err = d.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   res.ConversationID,
		SenderIdentityID: conversation.IdentityRef("system"),
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionOutbound,
		Content:          content,
		Actor:            observability.Actor("system"),
	})
	return err
}

// buildReminderDeliveryProjector builds the fired→deliver projector (for the
// outbox Relay). Returns nil if the conversation writer is missing.
func buildReminderDeliveryProjector(a *App) *cogservice.ReminderDeliveryProjector {
	if a == nil || a.MessageWriter == nil {
		return nil
	}
	return cogservice.NewReminderDeliveryProjector(&conversationDeliverer{writer: a.MessageWriter, idGen: a.IDGen, clk: a.Clock})
}

// buildReminderTickHook builds the per-tick scan→fire hook for pump.WithTickHook.
// Returns nil if prerequisites are missing. logf reports scan errors (the next
// tick retries — FindDue is idempotent).
func buildReminderTickHook(a *App, logf func(string)) func(context.Context) {
	if a == nil || a.DB == nil || a.Sink == nil || a.IDGen == nil {
		return nil
	}
	repo := remindersqlite.NewReminderRepo(a.DB)
	scheduler := cogservice.NewReminderScheduler(a.DB, repo, a.Sink, a.IDGen)
	tp := cogservice.NewReminderTickProjector(scheduler, a.Clock, func(err error) {
		if logf != nil {
			logf("reminder tick: " + err.Error())
		}
	})
	return tp.OnTick
}
