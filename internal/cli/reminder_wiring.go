package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	remindersqlite "github.com/oopslink/agent-center/internal/cognition/reminder/sqlite"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
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

// scanMember folds one member's identity ref into the (remindeeIn, creatorIn)
// flags. remindee and creator are matched with INDEPENDENT ifs, never a switch:
// for a self-reminder remindeeRef == creatorRef, and a switch would run only its
// first matching case — leaving creatorIn false, so the project would not count
// as creator+remindee shared and the aggregate would wrongly reject with
// ErrCrossProjectReminder (T229; design 03-reminder.md Invariant #4 self-reminder).
func scanMember(ref, remindeeRef, creatorRef string, remindeeIn, creatorIn bool) (bool, bool) {
	if ref == remindeeRef {
		remindeeIn = true
	}
	if ref == creatorRef {
		creatorIn = true
	}
	return remindeeIn, creatorIn
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
			remindeeIn, creatorIn = scanMember(string(m.IdentityID()), remindeeRef, creatorRef, remindeeIn, creatorIn)
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
		return cogservice.ReminderContext{}, reminder.ErrRemindeeNotInProject
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
// option A): on a fired reminder it opens a DM to the remindee and posts the
// content. The sender identity is the system by default, OR the reminder's CREATOR
// when deliver_as_creator is set (F-B). The EXISTING WakeProjector then wakes the
// remindee (a DM message whose sender is not the remindee agent wakes the agent
// participant). Anti-loop: the woken agent is the remindee only;
// cognition.reminder.fired is never on the supervisor self-wake allowlist.
type conversationDeliverer struct {
	writer *convservice.MessageWriter
	idGen  cogservice.IDGenerator
	clk    clockNow
}

// clockNow is the slice of clock.Clock the deliverer needs.
type clockNow interface{ Now() time.Time }

func (d *conversationDeliverer) Deliver(ctx context.Context, req cogservice.DeliveryRequest) error {
	remindeeRef := conversation.IdentityRef("agent:" + req.RemindeeAgentID)
	sender := deliverySender(req, remindeeRef)
	now := d.clk.Now().UTC().Format(time.RFC3339Nano)
	// T344: the DM must include BOTH parties — the SENDER (system for a self-reminder,
	// else the creator) AND the remindee. The old code listed only the remindee, so
	// the DM had a single-party dm_key (e.g. "agent:<id>") that the dedup index could
	// not collide with the real pair DM — every self-reminding agent accreted a stray
	// single-participant "Reminder" DM (@oopslink: duplicate @agent DMs). With the
	// sender as a participant the key is canonical: a creator-delivered reminder reuses
	// the existing creator↔agent DM; a self-reminder uses one [system, agent] DM.
	res, err := d.writer.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		Name:           "Reminder",
		OrganizationID: req.OrganizationID,
		Participants: []conversation.ParticipantElement{
			{IdentityID: sender, Role: "owner", JoinedAt: now, JoinedBy: conversation.IdentityRef("system")},
			{IdentityID: remindeeRef, Role: "member", JoinedAt: now, JoinedBy: conversation.IdentityRef("system")},
		},
		CreatedBy: sender,
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		return err
	}
	_, err = d.writer.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   res.ConversationID,
		SenderIdentityID: sender,
		ContentKind:      conversation.MessageContentText,
		Direction:        conversation.DirectionOutbound,
		Content:          req.Content,
		Actor:            observability.Actor("system"),
	})
	return err
}

// deliverySender picks the message sender identity (F-B). Default: system. When
// deliver_as_creator is set and a creator ref is known, deliver as the creator —
// EXCEPT a self-reminder (creator == the remindee agent): the WakeProjector excludes
// a message's own sender from the wake, so delivering a self-reminder as the
// remindee would silently NOT wake it. For that case we fall back to system so the
// remindee is still woken.
func deliverySender(req cogservice.DeliveryRequest, remindeeRef conversation.IdentityRef) conversation.IdentityRef {
	const system = conversation.IdentityRef("system")
	if !req.DeliverAsCreator || strings.TrimSpace(req.CreatorRef) == "" {
		return system
	}
	if req.CreatorRef == string(remindeeRef) {
		return system // self-reminder: keep system sender so the remindee still wakes.
	}
	return conversation.IdentityRef(req.CreatorRef)
}

// buildReminderDeliveryProjector builds the fired→deliver projector (for the
// outbox Relay). Returns nil if the conversation writer is missing.
func buildReminderDeliveryProjector(a *App) *cogservice.ReminderDeliveryProjector {
	if a == nil || a.MessageWriter == nil || a.DB == nil {
		return nil
	}
	// The repo doubles as the FiringMarker: after delivery the projector resolves
	// the firing pending→delivered so the recorded outcome reflects reality.
	repo := remindersqlite.NewReminderRepo(a.DB)
	return cogservice.NewReminderDeliveryProjector(
		&conversationDeliverer{writer: a.MessageWriter, idGen: a.IDGen, clk: a.Clock}, repo)
}

// buildReminderTickHook builds the per-tick scan→fire hook for pump.WithTickHook.
// Returns nil if prerequisites are missing. logf reports scan errors (the next
// tick retries — FindDue is idempotent).
func buildReminderTickHook(a *App, logf func(string)) func(context.Context) {
	if a == nil || a.DB == nil || a.Sink == nil || a.OutboxRepo == nil || a.IDGen == nil {
		return nil
	}
	repo := remindersqlite.NewReminderRepo(a.DB)
	// OutboxRepo is REQUIRED: the fired event must land in the outbox so the
	// delivery projector wakes the remindee (F1). Same DB-backed outbox the relay
	// drains, so the Append and the projector see one table.
	scheduler := cogservice.NewReminderScheduler(a.DB, repo, a.Sink, a.OutboxRepo, a.IDGen)
	// reminder-event: record a reminder_fired change on the triggering entity's ledger
	// when an event-driven reminder fires (best-effort; nil sink = no-op).
	scheduler.SetAudit(buildReminderAuditSink(a))
	tp := cogservice.NewReminderTickProjector(scheduler, a.Clock, func(err error) {
		if logf != nil {
			logf("reminder tick: " + err.Error())
		}
	})
	return tp.OnTick
}

// reminderAuditAdapter maps the Cognition reminder audit port
// (cogservice.ReminderAuditSink) onto the ProjectManager change-log ledger
// (pm.AuditLogRepository). It lets the reminder event-projector + scheduler record
// reminder_armed / reminder_fired entries on the TRIGGERING pm entity's ledger
// without the Cognition BC importing pm's audit types directly (the adapter — living
// in the cli composition root, which already depends on both BCs — bridges them).
type reminderAuditAdapter struct{ repo pm.AuditLogRepository }

func (a *reminderAuditAdapter) AppendReminderAudit(ctx context.Context, e cogservice.ReminderAuditEntry) error {
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	return a.repo.Append(ctx, pm.AuditEntry{
		ProjectID:  pm.ProjectID(e.ProjectID),
		ObjectType: pm.AuditObjectType(e.ObjectType), // "plan"|"task"|"issue" (matches pm.AuditObject*)
		ObjectID:   e.ObjectID,
		ChangeType: pm.AuditChangeType(e.ChangeType), // "reminder_armed"|"reminder_fired"
		ActorRef:   pm.SystemActor("reminder-event"),
		Detail:     string(detail),
		OccurredAt: e.OccurredAt,
	})
}

// buildReminderAuditSink builds the change-log audit adapter for reminder-event
// firing/arming. Returns nil (⇒ audit is a no-op) when the DB/idgen are missing.
func buildReminderAuditSink(a *App) cogservice.ReminderAuditSink {
	if a == nil || a.DB == nil || a.IDGen == nil {
		return nil
	}
	return &reminderAuditAdapter{repo: pmsql.NewAuditLogRepo(a.DB, a.IDGen)}
}

// buildReminderEventProjector builds the outbox projector that ARMS event-driven
// reminders on matching pm entity state-change events (reminder-event feature). It
// must be registered on the relay (like the other cross-BC projectors) or on_event
// reminders would never arm. Returns nil when prerequisites are missing.
func buildReminderEventProjector(a *App, applied outbox.AppliedStore) *cogservice.ReminderEventProjector {
	if a == nil || a.DB == nil || applied == nil {
		return nil
	}
	repo := remindersqlite.NewReminderRepo(a.DB)
	return cogservice.NewReminderEventProjector(a.DB, repo, applied, a.Clock, buildReminderAuditSink(a))
}
