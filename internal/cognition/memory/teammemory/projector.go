package teammemory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/oopslink/agent-center/internal/observability"
)

// Projector mirrors Team Memory proposal transitions into Observability. The
// Git repository remains the write authority; this projector is deliberately a
// read-side append-only projection.
type Projector struct {
	db     *sql.DB
	repo   Repository
	events observability.EventRepository
	seq    observability.SeqAllocator
	now    func() time.Time
}

// NewProjector wires the Team Memory Observability projector.
func NewProjector(db *sql.DB, repo Repository, events observability.EventRepository, seq observability.SeqAllocator) *Projector {
	return &Projector{
		db:     db,
		repo:   repo,
		events: events,
		seq:    seq,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// ReconcileTeam emits idempotent events for all known proposal transitions in a
// team's memory repository and records the projected Git commit checkpoint.
func (p *Projector) ReconcileTeam(ctx context.Context, teamID string) error {
	if p == nil || p.repo == nil || p.events == nil || p.seq == nil {
		return nil
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return nil
	}
	statuses := []ProposalStatus{StatusPending, StatusPromoted, StatusRejected, StatusSuperseded}
	offset := 0
	lastCommit := ""
	for {
		res, err := p.repo.List(ctx, teamID, Filter{Status: statuses, Limit: 100, Offset: offset})
		if err != nil {
			return err
		}
		if res.RepoCommit != "" {
			lastCommit = res.RepoCommit
		}
		for _, view := range res.Proposals {
			if err := p.emitTransition(ctx, "team_memory.proposed", view, view.Proposal.AuthorRef, view.Proposal.CreatedAt); err != nil {
				return err
			}
			switch view.Proposal.Status {
			case StatusPromoted:
				if err := p.emitTransition(ctx, "team_memory.promoted", view, view.Proposal.ReviewerRef, view.Proposal.ReviewedAt); err != nil {
					return err
				}
			case StatusRejected:
				if err := p.emitTransition(ctx, "team_memory.rejected", view, view.Proposal.ReviewerRef, view.Proposal.ReviewedAt); err != nil {
					return err
				}
			case StatusSuperseded:
				if err := p.emitTransition(ctx, "team_memory.superseded", view, view.Proposal.ReviewerRef, view.Proposal.ReviewedAt); err != nil {
					return err
				}
			}
		}
		if !res.HasMore || len(res.Proposals) == 0 {
			break
		}
		offset += len(res.Proposals)
	}
	return p.updateCheckpoint(ctx, teamID, lastCommit)
}

// EmitPromotionConflict records a fail-loud review conflict when a reviewer
// races a newer Team Memory Git commit.
func (p *Projector) EmitPromotionConflict(ctx context.Context, teamID, proposalID, actorRef, expectedCommit, actualCommit string) error {
	if p == nil || p.events == nil || p.seq == nil {
		return nil
	}
	now := p.now()
	key := strings.Join([]string{teamID, proposalID, "promotion_conflicted", actorRef, expectedCommit, actualCommit}, "\x00")
	event, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(stableEventID(now, key)),
		OccurredAt: now,
		Seq:        p.seq.NextSeq(),
		EventType:  observability.EventType("team_memory.promotion_conflicted"),
		Refs:       observability.EventRefs{TeamID: teamID, ProposalID: proposalID},
		Actor:      observability.Actor(validActorOrSystem(actorRef)),
		Payload: map[string]any{
			"reason":          Reason(ErrTeamMemoryVersionConflict),
			"message":         "team memory promotion conflicted with a newer Git commit",
			"team_id":         teamID,
			"proposal_id":     proposalID,
			"expected_commit": expectedCommit,
			"actual_commit":   actualCommit,
		},
		CreatedAt: now,
	})
	if err != nil {
		return err
	}
	return appendEvent(ctx, p.events, event)
}

func (p *Projector) emitTransition(ctx context.Context, typ string, view ProposalView, actorRef string, occurredAt time.Time) error {
	prop := view.Proposal
	if occurredAt.IsZero() {
		occurredAt = p.now()
	}
	status := prop.Status
	if typ == "team_memory.proposed" {
		status = StatusPending
	}
	sourcePath := prop.SourcePath
	if sourcePath == "" && prop.Target != nil {
		sourcePath = prop.Target.SourcePath
	}
	payload := map[string]any{
		"reason":                   "team_memory_transition",
		"message":                  fmt.Sprintf("%s %s", typ, prop.ProposalID),
		"team_id":                  prop.TeamID,
		"proposal_id":              prop.ProposalID,
		"operation":                string(prop.Operation),
		"target_kind":              string(prop.TargetKind),
		"status":                   string(status),
		"source_path":              sourcePath,
		"repo_commit":              view.RepoCommit,
		"warnings":                 append([]string(nil), prop.Warnings...),
		"current_target_blob_hash": view.CurrentTargetBlobHash,
	}
	if prop.PromotionCommit != "" {
		payload["promotion_commit"] = prop.PromotionCommit
	}
	if typ == "team_memory.promoted" {
		payload["effective_for"] = EffectiveForNewSessionsAndForks
	}
	key := strings.Join([]string{prop.TeamID, prop.ProposalID, typ}, "\x00")
	event, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(stableEventID(occurredAt, key)),
		OccurredAt: occurredAt,
		Seq:        p.seq.NextSeq(),
		EventType:  observability.EventType(typ),
		Refs:       observability.EventRefs{TeamID: prop.TeamID, ProposalID: prop.ProposalID},
		Actor:      observability.Actor(validActorOrSystem(actorRef)),
		Payload:    payload,
		CreatedAt:  p.now(),
	})
	if err != nil {
		return err
	}
	return appendEvent(ctx, p.events, event)
}

func (p *Projector) updateCheckpoint(ctx context.Context, teamID, commit string) error {
	if p == nil || p.db == nil || strings.TrimSpace(teamID) == "" {
		return nil
	}
	_, err := p.db.ExecContext(ctx, `INSERT INTO team_memory_observability_checkpoints
		(team_id, last_projected_commit, updated_at) VALUES (?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET
		last_projected_commit=excluded.last_projected_commit,
		updated_at=excluded.updated_at`,
		teamID, strings.TrimSpace(commit), p.now().UTC().Format(time.RFC3339Nano))
	return err
}

func appendEvent(ctx context.Context, repo observability.EventRepository, event *observability.Event) error {
	if err := repo.Append(ctx, event); err != nil && !errors.Is(err, observability.ErrEventAlreadyExists) {
		return err
	}
	return nil
}

func stableEventID(ts time.Time, key string) string {
	if ts.IsZero() {
		ts = time.Unix(0, 0).UTC()
	}
	sum := sha256.Sum256([]byte(key))
	return ulid.MustNew(ulid.Timestamp(ts), bytes.NewReader(sum[:10])).String()
}

func validActorOrSystem(actorRef string) string {
	actorRef = strings.TrimSpace(actorRef)
	if actorRef == "" {
		return "system"
	}
	actor := observability.Actor(actorRef)
	if err := actor.Validate(); err != nil {
		return "system"
	}
	return actorRef
}
