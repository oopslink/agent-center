package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// decision_timeout_wiring.go (I103 §2) — the production DecisionReminderPort. When a
// human_decision node's deadline elapses, the pm-side HumanDecisionTimeoutSink calls
// this adapter to arm a durable, owner-visible "please rule" reminder over the cognition
// reminder AppService (create_reminder) — reusing the reminder infra rather than
// building a new notification path. Idempotency + escalation cadence are the SINK's job
// (it only calls at probe boundaries); this adapter just resolves the owner and creates
// the reminder.
//
// It lives in cli (the composition root) because it bridges two BCs — pm (decision
// owner) and cognition/reminder (delivery) — the same seam PlanDispatchAdapter uses. The
// pm BC stays clean of the reminder import (it sees only DecisionReminderPort).

// reminderCreator is the narrow slice of the reminder AppService this adapter needs
// (create a one-shot reminder). *cogservice.ReminderAppService satisfies it; a fake
// satisfies it in tests.
type reminderCreator interface {
	CreateReminder(ctx context.Context, cmd cogservice.CreateReminderCommand) (*reminder.Reminder, error)
}

// Narrow read-ports — only the FindByID each repo needs, so the concrete pmsql repos and
// test fakes both satisfy them without the full Repository surface.
type taskFinder interface {
	FindByID(ctx context.Context, id pm.TaskID) (*pm.Task, error)
}
type planFinder interface {
	FindByID(ctx context.Context, id pm.PlanID) (*pm.Plan, error)
}
type projectFinder interface {
	FindByID(ctx context.Context, id pm.ProjectID) (*pm.Project, error)
}

// decisionReminderAdapter implements pmservice.DecisionReminderPort.
type decisionReminderAdapter struct {
	reminders reminderCreator
	tasks     taskFinder
	plans     planFinder
	projects  projectFinder
	now       func() time.Time
	log       func(string, ...any)
}

// buildDecisionTimeoutSink assembles the production TimeoutSink (I103 §2). Returns nil
// when the reminder AppService cannot be built (e.g. a partial App) — the deadline
// engine then records probes but arms no reminders (nil-safe at the Service level).
func buildDecisionTimeoutSink(a *App) pmservice.TimeoutSink {
	reminders := buildReminderService(a)
	if reminders == nil {
		return nil
	}
	adapter := &decisionReminderAdapter{
		reminders: reminders,
		tasks:     pmsql.NewTaskRepo(a.DB),
		plans:     pmsql.NewPlanRepo(a.DB),
		projects:  pmsql.NewProjectRepo(a.DB),
		now:       a.Clock.Now,
		log: func(f string, args ...any) {
			slog.Warn("i103 decision-timeout: "+fmt.Sprintf(f, args...))
		},
	}
	return pmservice.NewHumanDecisionTimeoutSink(adapter, pmservice.DefaultDecisionEscalateAfter)
}

// ArmDecisionReminder resolves the timed-out decision's owner(s) and arms a one-shot
// reminder to each agent owner. Best-effort: it returns the first error for the sink to
// log (the sink swallows it — a failed reminder never rolls back the recorded probe),
// and a non-agent / unassigned owner is log-skipped (reminders are agent-remindee only;
// a user-owned decision is surfaced by the plan-conversation escalation path, not here).
func (ad *decisionReminderAdapter) ArmDecisionReminder(ctx context.Context, req pmservice.DecisionReminderRequest) error {
	plan, err := ad.plans.FindByID(ctx, req.PlanID)
	if err != nil {
		return err
	}
	if plan == nil {
		return nil // plan vanished between materialize and route — nothing to do.
	}
	orgID := ""
	if proj, perr := ad.projects.FindByID(ctx, plan.ProjectID()); perr == nil && proj != nil {
		orgID = proj.OrganizationID()
	}
	planCreator := string(plan.CreatorRef())

	// DecisionKeys are the pending decision task id(s); fall back to the node itself.
	keys := req.DecisionKeys
	if len(keys) == 0 {
		keys = []string{string(req.TaskID)}
	}
	var firstErr error
	for _, k := range keys {
		dec, derr := ad.tasks.FindByID(ctx, pm.TaskID(k))
		if derr != nil {
			if firstErr == nil {
				firstErr = derr
			}
			continue
		}
		owner, title := "", k
		if dec != nil {
			owner = string(dec.Assignee())
			if t := strings.TrimSpace(dec.Title()); t != "" {
				title = t
			}
		}
		remindee, ok := resolveDecisionRemindee(owner, planCreator)
		if !ok {
			ad.log("decision %s owner %q is not an agent — reminder skipped (owner nudged via plan escalation)", k, owner)
			continue
		}
		cmd := cogservice.CreateReminderCommand{
			OrganizationID:   orgID,
			CreatorRef:       decisionReminderCreator(planCreator),
			RemindeeAgentID:  remindee,
			Schedule:         reminder.OnceScheduleAt(ad.now().Add(time.Minute)),
			Content:          decisionReminderContent(title, req.Escalate, req.Overdue),
			SkipIfOverlap:    true,
			DeliverAsCreator: false, // deliver as system — the creator ref may be synthetic
			EndCondition:     reminder.NeverEnd(),
		}
		if _, cerr := ad.reminders.CreateReminder(ctx, cmd); cerr != nil {
			if firstErr == nil {
				firstErr = cerr
			}
		}
	}
	return firstErr
}

// resolveDecisionRemindee picks the agent that must rule a decision: its assignee, or —
// when unassigned — the plan creator. Reminders are agent-remindee only, so a user /
// system / empty owner returns ok=false (skip; that owner is nudged by the plan
// escalation path). Returns the BARE agent id (no "agent:" scheme) on success.
func resolveDecisionRemindee(ownerRef, planCreatorRef string) (string, bool) {
	owner := strings.TrimSpace(ownerRef)
	if owner == "" {
		owner = strings.TrimSpace(planCreatorRef)
	}
	if !strings.HasPrefix(owner, "agent:") {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(owner, "agent:"))
	if id == "" {
		return "", false
	}
	return id, true
}

// decisionReminderCreator returns a CreatorRef that clears the reminder cross-project
// guard (reminder_wiring.go IsOwner: a "user:" ref is an owner and may create across
// projects). The plan creator is used when it is a user; otherwise a fixed system-user
// ref. Delivery is DeliverAsCreator=false (as system), so this ref only needs to be a
// valid owner identity, not a real deliverable inbox.
func decisionReminderCreator(planCreatorRef string) string {
	if strings.HasPrefix(strings.TrimSpace(planCreatorRef), "user:") {
		return strings.TrimSpace(planCreatorRef)
	}
	return "user:system"
}

// decisionReminderContent renders the reminder body — a plain "please rule" nudge, or a
// stronger escalation once the decision has blown several deadlines.
func decisionReminderContent(title string, escalate bool, overdue time.Duration) string {
	if escalate {
		return fmt.Sprintf("决策节点 %q 待裁已多次超时（累计逾期约 %s，已升级）——该分支不会自动推进，请尽快裁决。", title, roundOverdue(overdue))
	}
	return fmt.Sprintf("决策节点 %q 待裁已超时（逾期约 %s），请裁决。", title, roundOverdue(overdue))
}

// roundOverdue renders an overdue duration at minute granularity for the reminder text.
func roundOverdue(d time.Duration) time.Duration {
	if d < time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(time.Minute)
}
