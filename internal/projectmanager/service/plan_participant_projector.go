package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanParticipantProjector is the v2.9 #284 projector that wires the Plan ↔
// Conversation 1:1 and keeps the Plan conversation's participants in sync with
// the Plan's selected-task assignees — ADDITIVELY (§9.5). It is a SIBLING of the
// task/issue ParticipantProjector (same one-way PM→Conversation contract, same
// side-effect + MarkApplied in ONE tx), registered on the same Relay.
//
// It consumes:
//   - pm.plan.created → create the Plan's 1:1 Conversation (owner_ref
//     pm://plans/{id}, kind=plan, org-stamped) with the creator as the first
//     participant, then persist the new conversation id back onto the Plan
//     (Plan.SetConversationID → plans.Update).
//   - pm.plan.participants_changed → ensure each ref in the event's Participants
//     is a participant of the Plan conversation (add if missing). NEVER removes.
//
// Unlike the task/issue projector (which OVERWRITES participants with the full
// effective set), this one UNIONS the delta into the existing set: §9.5 dispatch
// works by @mentioning a node's assignee in the Plan conversation, and a mention
// only reaches members — so the assignee (human OR agent) MUST be a participant
// to be woken, and yanking participants mid-plan would break history access.
type PlanParticipantProjector struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	plans    pm.PlanRepository
	applied  outbox.AppliedStore
	idgen    idgen.Generator
	clock    clock.Clock
	// msgRepo/readState are OPTIONAL (v2.9 P3, nil-safe). When wired, the
	// EvtPlanDeleted branch cascade-removes the plan conversation's messages +
	// read-state in the SAME tx as the conversation-row delete (mirroring the DM
	// hard-delete handler, #198). nil ⇒ only the conversation row is deleted (the
	// messages/read-state are left, harmless once the parent row is gone).
	msgRepo   conversation.MessageRepository
	readState conversation.UserConversationReadStateRepository
}

// NewPlanParticipantProjector constructs the projector. plans may be nil only in
// fixtures that never emit pm.plan.created (the create branch needs it to persist
// the conversation id back onto the Plan); pass it in real wiring.
func NewPlanParticipantProjector(db *sql.DB, convRepo conversation.ConversationRepository, plans pm.PlanRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *PlanParticipantProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &PlanParticipantProjector{db: db, convRepo: convRepo, plans: plans, applied: applied, idgen: gen, clock: clk}
}

// WithConversationCascade wires the OPTIONAL message + read-state repos so the
// EvtPlanDeleted branch fully hard-deletes the plan conversation (row + messages
// + read-state) in one tx (v2.9 P3, mirrors the DM-delete handler #198). Without
// it, EvtPlanDeleted still deletes the conversation row (messages/read-state are
// then orphan-harmless). Returns the receiver for fluent wiring.
func (p *PlanParticipantProjector) WithConversationCascade(msgRepo conversation.MessageRepository, readState conversation.UserConversationReadStateRepository) *PlanParticipantProjector {
	p.msgRepo = msgRepo
	p.readState = readState
	return p
}

// Name is the AppliedStore key (distinct from the task/issue projector so both
// can independently track applied events on the shared Relay).
func (p *PlanParticipantProjector) Name() string { return "pm-plan-participant-sync" }

// Project applies one Plan event. Irrelevant event types are a no-op.
func (p *PlanParticipantProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case EvtPlanCreated, EvtPlanParticipantsChanged, EvtPlanDeleted, EvtPlanArchived:
		// handled below
	default:
		return nil
	}
	var pl planEventPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if strings.TrimSpace(pl.OwnerRef) == "" {
		return errors.New("plan participant projector: event missing owner_ref")
	}
	ownerRef := conversation.OwnerRef(pl.OwnerRef)
	now := p.clock.Now()

	// v2.9 P3 cleanup branches: delete (hard-remove the plan conversation, "删会话")
	// and archive (UpdateArchive) the plan's 1:1 conversation. Both are idempotent
	// (an already-gone/already-archived conversation is a no-op) + applied-gated.
	if e.EventType == EvtPlanDeleted || e.EventType == EvtPlanArchived {
		return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
			if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
				return err
			} else if done {
				return nil
			}
			conv, err := p.convRepo.FindByOwnerRef(txCtx, ownerRef)
			switch {
			case errors.Is(err, conversation.ErrConversationNotFound):
				// No conversation bound (or already deleted) — nothing to clean up.
			case err != nil:
				return err
			case e.EventType == EvtPlanDeleted:
				if cerr := p.deletePlanConversation(txCtx, conv.ID()); cerr != nil {
					return cerr
				}
			default: // EvtPlanArchived
				if conv.Status() != conversation.ConversationArchived {
					if cerr := p.convRepo.UpdateArchive(txCtx, conv.ID(), conv.Version(), conversation.IdentityRef("system"), now); cerr != nil {
						return cerr
					}
				}
			}
			return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
		})
	}

	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		// Same-tx idempotency: if already applied, this event is done.
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}

		conv, err := p.convRepo.FindByOwnerRef(txCtx, ownerRef)
		switch {
		case errors.Is(err, conversation.ErrConversationNotFound):
			// Create the Plan's 1:1 Conversation with the event's participants
			// (creator on pm.plan.created — the FIRST event for the owner_ref).
			if strings.TrimSpace(pl.OrganizationID) == "" {
				// Defensive (§-1 no-silent): an org-less Plan conversation would 404 for
				// the orchestrator's @mention path. Created always carries org.
				return errors.New("plan participant projector: cannot create conversation with empty organization_id for " + string(ownerRef))
			}
			nc, nerr := conversation.NewConversation(conversation.NewConversationInput{
				ID:             conversation.ConversationID(p.idgen.NewULID()),
				Kind:           conversation.ConversationKindPlan,
				OwnerRef:       ownerRef,
				OrganizationID: pl.OrganizationID,
				CreatedBy:      conversation.IdentityRef("system"),
				OpenedAt:       now,
				Participants:   participantsFrom(pl.Participants, now),
			})
			if nerr != nil {
				return nerr
			}
			if serr := p.convRepo.Save(txCtx, nc); serr != nil {
				return serr
			}
			// Persist the new conversation id back onto the Plan (1:1 binding).
			if perr := p.bindConversationToPlan(txCtx, pl.PlanID, string(nc.ID()), now); perr != nil {
				return perr
			}
		case err != nil:
			return err
		default:
			// Existing Conversation: UNION the delta into the current participants
			// (ADDITIVE §9.5 — add if missing, never remove). Re-emit is idempotent
			// because an already-present ref contributes nothing to the union.
			merged, changed := unionParticipants(conv.Participants(), pl.Participants, now)
			if changed {
				if uerr := p.convRepo.UpdateParticipants(txCtx, conv.ID(), merged, conv.Version(), now); uerr != nil {
					return uerr
				}
			}
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// deletePlanConversation hard-removes the plan's 1:1 conversation: its messages
// and read-state (when the optional repos are wired), then the conversation row
// itself — all in the caller's tx (mirroring the DM hard-delete handler, #198).
// Each step is idempotent (deleting absent rows is a no-op).
func (p *PlanParticipantProjector) deletePlanConversation(ctx context.Context, id conversation.ConversationID) error {
	if p.msgRepo != nil {
		if err := p.msgRepo.DeleteByConversationID(ctx, id); err != nil {
			return err
		}
	}
	if p.readState != nil {
		if err := p.readState.DeleteByConversationID(ctx, id); err != nil {
			return err
		}
	}
	return p.convRepo.Delete(ctx, id)
}

// bindConversationToPlan loads the Plan, sets its conversation id, and persists
// it (the 1:1 binding). A nil plans repo or an unloadable plan is a hard error
// here — the binding is the whole point of pm.plan.created (§-1 no-silent).
func (p *PlanParticipantProjector) bindConversationToPlan(ctx context.Context, planID, convID string, now time.Time) error {
	if p.plans == nil {
		return errors.New("plan participant projector: plan repository unavailable — cannot bind conversation to plan")
	}
	pl, err := p.plans.FindByID(ctx, pm.PlanID(planID))
	if err != nil {
		return err
	}
	pl.SetConversationID(convID, now)
	return p.plans.Update(ctx, pl)
}

// unionParticipants returns existing ∪ {adds} (set semantics on identity id) and
// whether any ref was actually added. Existing participants are preserved
// verbatim (role/joined_at untouched); newly-added refs join as system members.
func unionParticipants(existing []conversation.ParticipantElement, adds []string, now time.Time) ([]conversation.ParticipantElement, bool) {
	have := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		have[string(e.IdentityID)] = struct{}{}
	}
	out := make([]conversation.ParticipantElement, len(existing))
	copy(out, existing)
	joinedAt := now.UTC().Format(time.RFC3339Nano)
	changed := false
	for _, a := range adds {
		if strings.TrimSpace(a) == "" {
			continue
		}
		if _, ok := have[a]; ok {
			continue // already a participant — additive no-op
		}
		have[a] = struct{}{}
		out = append(out, conversation.ParticipantElement{
			IdentityID: conversation.IdentityRef(a),
			Role:       "member",
			JoinedAt:   joinedAt,
			JoinedBy:   conversation.IdentityRef("system"),
		})
		changed = true
	}
	return out, changed
}

var _ outbox.Projector = (*PlanParticipantProjector)(nil)
