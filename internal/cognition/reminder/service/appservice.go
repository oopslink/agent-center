package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// Lifecycle event types (§3.7) emitted by the AppService on create/update.
const (
	EventReminderCreated  observability.EventType = "cognition.reminder.created"
	EventReminderPaused   observability.EventType = "cognition.reminder.paused"
	EventReminderResumed  observability.EventType = "cognition.reminder.resumed"
	EventReminderUpdated  observability.EventType = "cognition.reminder.updated"
	EventReminderCanceled observability.EventType = "cognition.reminder.canceled"
)

// ErrReminderForbidden is returned when a requester may not manage a reminder
// (not its creator and not an owner).
var ErrReminderForbidden = errors.New("cognition: not allowed to manage this reminder")

// ReminderContext is what the Directory resolves for a create: the org + project
// the reminder lives in (the remindee's project), whether the creator is an owner
// (bypasses the cross-project guard), and the creator's project (for the guard).
type ReminderContext struct {
	OrganizationID   string
	ProjectID        string
	CreatorIsOwner   bool
	CreatorProjectID string
}

// Directory is the port the AppService uses to resolve membership/ownership for
// the cross-project guard (§1: Cognition ← Workforce/Identity). The concrete impl
// (cli) reads agent→project mappings; keeping it a port keeps the AppService
// unit-testable.
type Directory interface {
	// ResolveReminderContext resolves the create context within orgID (the handler
	// passes the guardrail-resolved operating agent's org) for creatorRef targeting
	// remindeeAgentID. It errors if the remindee can't be located in the org.
	ResolveReminderContext(ctx context.Context, orgID, creatorRef, remindeeAgentID string) (ReminderContext, error)
	// IsOwner reports whether ref is an organization owner (may manage any reminder).
	IsOwner(ctx context.Context, ref string) bool
}

// IDGenerator produces reminder/firing ULIDs.
type IDGenerator interface{ NewULID() string }

// ReminderAppService is the application service behind the MCP tools + admin API:
// create / get / list / update (pause·resume·cancel·edit). It enforces the
// cross-project guard at create and creator/owner authz on manage, and emits the
// lifecycle events. Each write runs in a tx with its event (ADR-0014).
type ReminderAppService struct {
	db    *sql.DB
	repo  reminder.Repository
	dir   Directory
	sink  EventEmitter
	idGen IDGenerator
	clk   clock.Clock
}

// NewReminderAppService wires the service.
func NewReminderAppService(db *sql.DB, repo reminder.Repository, dir Directory, sink EventEmitter, idGen IDGenerator, clk clock.Clock) *ReminderAppService {
	return &ReminderAppService{db: db, repo: repo, dir: dir, sink: sink, idGen: idGen, clk: clk}
}

// CreateReminderCommand is the create input. CreatorRef is the PROCESS-FIXED
// identity (agent:<id> injected by the tool layer, or user:<owner>); never from
// model args.
type CreateReminderCommand struct {
	OrganizationID  string // the operating agent's org (handler-resolved)
	CreatorRef      string
	RemindeeAgentID string
	Schedule        reminder.Schedule
	Content         string
	SkipIfOverlap   bool
	// DeliverAsCreator (F-B): when true the delivered reminder is posted as the
	// creator's identity; when false as the system identity. Handler defaults ON.
	DeliverAsCreator bool
	EndCondition     reminder.EndCondition
}

// CreateReminder resolves the project context + guard, builds the aggregate, and
// persists it with a created event.
func (s *ReminderAppService) CreateReminder(ctx context.Context, cmd CreateReminderCommand) (*reminder.Reminder, error) {
	rc, err := s.dir.ResolveReminderContext(ctx, cmd.OrganizationID, cmd.CreatorRef, cmd.RemindeeAgentID)
	if err != nil {
		return nil, err
	}
	now := s.clk.Now()
	r, err := reminder.NewReminder(reminder.NewReminderInput{
		ID:               s.idGen.NewULID(),
		OrganizationID:   rc.OrganizationID,
		ProjectID:        rc.ProjectID,
		CreatorRef:       cmd.CreatorRef,
		CreatorIsOwner:   rc.CreatorIsOwner,
		CreatorProjectID: rc.CreatorProjectID,
		RemindeeAgentID:  cmd.RemindeeAgentID,
		Schedule:         cmd.Schedule,
		Content:          cmd.Content,
		SkipIfOverlap:    cmd.SkipIfOverlap,
		DeliverAsCreator: cmd.DeliverAsCreator,
		EndCondition:     cmd.EndCondition,
		Now:              now,
	})
	if err != nil {
		return nil, err
	}
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, r); err != nil {
			return err
		}
		return s.emit(txCtx, EventReminderCreated, r)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetReminder loads a reminder if the requester may see it (creator / remindee /
// owner).
func (s *ReminderAppService) GetReminder(ctx context.Context, id reminder.ReminderID, requesterRef string) (*reminder.Reminder, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.canManage(ctx, r, requesterRef) && r.RemindeeAgentID() != bareAgent(requesterRef) {
		return nil, ErrReminderForbidden
	}
	return r, nil
}

// ListRemindersQuery selects reminders by creator OR remindee (exactly one set),
// optionally narrowed by status.
type ListRemindersQuery struct {
	CreatorRef      string
	RemindeeAgentID string
	Statuses        []reminder.ReminderStatus
}

// ListReminders lists by creator or remindee with an optional status filter.
func (s *ReminderAppService) ListReminders(ctx context.Context, q ListRemindersQuery) ([]*reminder.Reminder, error) {
	f := reminder.ListFilter{Statuses: q.Statuses}
	if q.CreatorRef != "" {
		return s.repo.ListByCreator(ctx, q.CreatorRef, f)
	}
	return s.repo.ListByRemindee(ctx, q.RemindeeAgentID, f)
}

// ListOrgReminders backs the web console "全部" view (T207): an org OWNER sees
// EVERY reminder in orgID; a non-owner requester is fail-closed to its OWN created
// reminders (never another creator's). Org-scoped by construction — no cross-org leak.
func (s *ReminderAppService) ListOrgReminders(ctx context.Context, orgID, requesterRef string, statuses []reminder.ReminderStatus) ([]*reminder.Reminder, error) {
	f := reminder.ListFilter{Statuses: statuses}
	if s.dir.IsOwner(ctx, requesterRef) {
		return s.repo.ListByOrg(ctx, orgID, f)
	}
	return s.repo.ListByCreator(ctx, requesterRef, f)
}

// ListRemindersPage is the server-side paginated ListReminders: same scope
// (creator or remindee) + filter f (status/q/sort/limit/offset), returning the
// page + the TOTAL count for the web console pagination.
func (s *ReminderAppService) ListRemindersPage(ctx context.Context, q ListRemindersQuery, f reminder.ListFilter) ([]*reminder.Reminder, int, error) {
	f.Statuses = q.Statuses
	if q.CreatorRef != "" {
		return s.repo.ListByCreatorPage(ctx, q.CreatorRef, f)
	}
	return s.repo.ListByRemindeePage(ctx, q.RemindeeAgentID, f)
}

// ListOrgRemindersPage is the paginated ListOrgReminders (owner → org-wide;
// non-owner → own created), returning the page + TOTAL count.
func (s *ReminderAppService) ListOrgRemindersPage(ctx context.Context, orgID, requesterRef string, f reminder.ListFilter) ([]*reminder.Reminder, int, error) {
	if s.dir.IsOwner(ctx, requesterRef) {
		return s.repo.ListByOrgPage(ctx, orgID, f)
	}
	return s.repo.ListByCreatorPage(ctx, requesterRef, f)
}

// GetReminderFirings returns a reminder's trigger history (T207 历史触发) if the
// requester may see the reminder (creator / remindee / owner) — same visibility
// gate as GetReminder.
func (s *ReminderAppService) GetReminderFirings(ctx context.Context, id reminder.ReminderID, requesterRef string) ([]reminder.Firing, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.canManage(ctx, r, requesterRef) && r.RemindeeAgentID() != bareAgent(requesterRef) {
		return nil, ErrReminderForbidden
	}
	return s.repo.ListFirings(ctx, id.String())
}

// UpdateAction selects the lifecycle op for UpdateReminder.
type UpdateAction string

const (
	ActionPause  UpdateAction = "pause"
	ActionResume UpdateAction = "resume"
	ActionCancel UpdateAction = "cancel"
	ActionEdit   UpdateAction = "edit" // schedule and/or content
)

// UpdateReminderCommand drives pause/resume/cancel or an edit (schedule/content).
type UpdateReminderCommand struct {
	ID           reminder.ReminderID
	RequesterRef string
	Action       UpdateAction
	Schedule     *reminder.Schedule // edit only
	Content      string             // edit only ("" leaves unchanged)
}

// UpdateReminder applies the requested lifecycle op under creator/owner authz and
// emits the matching event.
func (s *ReminderAppService) UpdateReminder(ctx context.Context, cmd UpdateReminderCommand) (*reminder.Reminder, error) {
	r, err := s.repo.Get(ctx, cmd.ID)
	if err != nil {
		return nil, err
	}
	if !s.canManage(ctx, r, cmd.RequesterRef) {
		return nil, ErrReminderForbidden
	}
	now := s.clk.Now()
	var evt observability.EventType
	switch cmd.Action {
	case ActionPause:
		err, evt = r.Pause(now), EventReminderPaused
	case ActionResume:
		err, evt = r.Resume(now), EventReminderResumed
	case ActionCancel:
		err, evt = r.Cancel(now), EventReminderCanceled
	case ActionEdit:
		err, evt = r.Update(cmd.Schedule, cmd.Content, now), EventReminderUpdated
	default:
		return nil, errors.New("cognition: unknown update action " + string(cmd.Action))
	}
	if err != nil {
		return nil, err
	}
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Update(txCtx, r); err != nil {
			return err
		}
		return s.emit(txCtx, evt, r)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// canManage: the creator of the reminder or an org owner may manage it.
func (s *ReminderAppService) canManage(ctx context.Context, r *reminder.Reminder, requesterRef string) bool {
	return r.CreatorRef() == requesterRef || s.dir.IsOwner(ctx, requesterRef)
}

// emit publishes a reminder lifecycle event inside the caller's tx.
func (s *ReminderAppService) emit(ctx context.Context, et observability.EventType, r *reminder.Reminder) error {
	_, err := s.sink.Emit(ctx, observability.EmitCommand{
		EventType: et,
		Refs: observability.EventRefs{
			AgentID:        r.RemindeeAgentID(),
			ProjectID:      r.ProjectID(),
			OrganizationID: r.OrganizationID(),
		},
		Actor: observability.Actor(r.CreatorRef()),
		Payload: map[string]any{
			"reminder_id":       r.ID().String(),
			"remindee_agent_id": r.RemindeeAgentID(),
			"status":            string(r.Status()),
		},
	})
	return err
}

// bareAgent strips an "agent:" prefix so a requester ref can be compared to a
// bare remindee_agent_id.
func bareAgent(ref string) string {
	const p = "agent:"
	if len(ref) > len(p) && ref[:len(p)] == p {
		return ref[len(p):]
	}
	return ref
}
