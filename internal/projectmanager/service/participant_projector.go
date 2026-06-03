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
)

// ParticipantProjector is the B2-b outbox projector that syncs the effective
// subscriber set (ProjectManager truth) into Conversation participants
// (ADR-0052 §2). It is ONE-WAY (PM→Conversation) and applies ONLY to task/issue
// conversations — channel/dm members are explicit and self-managed by the
// Conversation BC (plan §10 OQ10), never projected.
//
// On pm.task.created / pm.issue.created it creates the bound Conversation
// (owner_ref pm://tasks|issues/{id}) idempotently; on *.subscribers_changed it
// overwrites participants with the new effective set (set semantics). It
// performs the side effect AND records AppliedStore.MarkApplied in the SAME
// transaction (review finding 2), so redelivery is a true no-op even though
// the body is not otherwise idempotent.
type ParticipantProjector struct {
	db       *sql.DB
	convRepo conversation.ConversationRepository
	applied  outbox.AppliedStore
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewParticipantProjector constructs the projector.
func NewParticipantProjector(db *sql.DB, convRepo conversation.ConversationRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *ParticipantProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &ParticipantProjector{db: db, convRepo: convRepo, applied: applied, idgen: gen, clock: clk}
}

// Name is the AppliedStore key.
func (p *ParticipantProjector) Name() string { return "pm-participant-sync" }

// projectorPayload is the subset of the task/issue event payloads this
// projector reads.
type projectorPayload struct {
	OwnerRef             string   `json:"owner_ref"`
	OrganizationID       string   `json:"organization_id"`
	EffectiveSubscribers []string `json:"effective_subscribers"`
}

// Project applies one event. Irrelevant event types are a no-op (the Relay
// marks them processed). Relevant ones run side-effect + MarkApplied in one tx.
func (p *ParticipantProjector) Project(ctx context.Context, e outbox.Event) error {
	var kind conversation.ConversationKind
	switch e.EventType {
	case EvtTaskCreated, EvtTaskSubsChanged, EvtTaskAssigned, EvtTaskReassigned, EvtTaskStateChanged:
		// EvtTaskStateChanged carries the recomputed effective set: unassign/
		// reopen drop the assignee → the prior assignee must leave the task
		// Conversation. Other state changes leave the set unchanged (idempotent
		// rewrite), so handling them all keeps participants convergent.
		kind = conversation.ConversationKindTask
	case EvtIssueCreated, EvtIssueSubsChanged:
		kind = conversation.ConversationKindIssue
	default:
		return nil // not a participant-affecting event; nothing to project
	}
	var pl projectorPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if strings.TrimSpace(pl.OwnerRef) == "" {
		return errors.New("participant projector: event missing owner_ref")
	}
	ownerRef := conversation.OwnerRef(pl.OwnerRef)
	now := p.clock.Now()

	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		// Same-tx idempotency: if already applied, this event is done.
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}

		parts := participantsFrom(pl.EffectiveSubscribers, now)
		conv, err := p.convRepo.FindByOwnerRef(txCtx, ownerRef)
		switch {
		case errors.Is(err, conversation.ErrConversationNotFound):
			// Create the bound Conversation with the effective participants.
			// Stamp the project's org onto the Conversation: org-scoped endpoints
			// (requireConversationInOrg — incl. a human replying to a waiting_input agent
			// → wake) reject a conversation whose org != actor org, so an unstamped (empty)
			// org would 404 for EVERYONE (the GATE-4 ship-blocker). The org rides the
			// Created event payload (sourced from the project at emit). EvtTaskCreated/
			// EvtIssueCreated is always the FIRST event for an owner_ref (outbox order;
			// you can't subscribe/assign before create), so the create branch always has it.
			if strings.TrimSpace(pl.OrganizationID) == "" {
				// Defensive (§-1 no-silent): a create with no org would be unusable. This
				// should be unreachable (Created carries org); surface loudly if it ever isn't.
				return errors.New("participant projector: cannot create conversation with empty organization_id for " + string(ownerRef))
			}
			nc, nerr := conversation.NewConversation(conversation.NewConversationInput{
				ID:             conversation.ConversationID(p.idgen.NewULID()),
				Kind:           kind,
				OwnerRef:       ownerRef,
				OrganizationID: pl.OrganizationID,
				CreatedBy:      conversation.IdentityRef("system"),
				OpenedAt:       now,
				Participants:   parts,
			})
			if nerr != nil {
				return nerr
			}
			if serr := p.convRepo.Save(txCtx, nc); serr != nil {
				return serr
			}
		case err != nil:
			return err
		default:
			// Existing Conversation: overwrite participants to the effective set.
			if uerr := p.convRepo.UpdateParticipants(txCtx, conv.ID(), parts, conv.Version(), now); uerr != nil {
				return uerr
			}
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// participantsFrom maps effective subscriber identity refs to projected
// ParticipantElements (role=member, joined_by=system).
func participantsFrom(subs []string, now time.Time) []conversation.ParticipantElement {
	out := make([]conversation.ParticipantElement, 0, len(subs))
	joinedAt := now.UTC().Format(time.RFC3339Nano)
	for _, s := range subs {
		if strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, conversation.ParticipantElement{
			IdentityID: conversation.IdentityRef(s),
			Role:       "member",
			JoinedAt:   joinedAt,
			JoinedBy:   conversation.IdentityRef("system"),
		})
	}
	return out
}

var _ outbox.Projector = (*ParticipantProjector)(nil)
