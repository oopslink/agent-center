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

	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/observability"
)

type Projector struct {
	db     *sql.DB
	git    *centergit.TeamMemoryGit
	events observability.EventRepository
	seq    observability.SeqAllocator
	now    func() time.Time
}

func NewProjector(db *sql.DB, git *centergit.TeamMemoryGit, events observability.EventRepository, seq observability.SeqAllocator) *Projector {
	return &Projector{
		db: db, git: git, events: events, seq: seq,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (p *Projector) ReconcileTeam(ctx context.Context, teamID string) error {
	if p == nil || p.git == nil || p.events == nil || p.seq == nil {
		return nil
	}
	props, head, err := p.git.ListProposals(ctx, teamID, centergit.ProposalListFilter{})
	if err != nil {
		return err
	}
	for _, prop := range props {
		createdCommit := firstNonEmpty(prop.CreatedCommit, prop.RepoCommit, head)
		if err := p.emitTransition(ctx, "team_memory.proposed", prop, prop.AuthorRef, prop.CreatedAt, createdCommit); err != nil {
			return err
		}
		switch prop.Status {
		case centergit.ProposalPromoted:
			commit := firstNonEmpty(prop.StatusCommit, prop.RepoCommit, head)
			if err := p.emitTransition(ctx, "team_memory.promoted", prop, prop.ReviewerRef, prop.ReviewedAt, commit); err != nil {
				return err
			}
		case centergit.ProposalRejected:
			commit := firstNonEmpty(prop.StatusCommit, prop.RepoCommit, head)
			if err := p.emitTransition(ctx, "team_memory.rejected", prop, prop.ReviewerRef, prop.ReviewedAt, commit); err != nil {
				return err
			}
		}
	}
	return p.updateCheckpoint(ctx, teamID, head)
}

func (p *Projector) EmitPromotionConflict(ctx context.Context, teamID, proposalID, actorRef, expectedCommit, actualCommit string) error {
	if p == nil || p.events == nil || p.seq == nil {
		return nil
	}
	now := p.now()
	key := strings.Join([]string{teamID, proposalID, "promotion_conflicted", actorRef, expectedCommit, actualCommit}, "\x00")
	eid := stableEventID(now, key)
	event, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(eid),
		OccurredAt: now,
		Seq:        p.seq.NextSeq(),
		EventType:  observability.EventType("team_memory.promotion_conflicted"),
		Refs:       observability.EventRefs{TeamID: teamID, ProposalID: proposalID},
		Actor:      observability.Actor(firstNonEmpty(actorRef, "system")),
		Payload: map[string]any{
			"reason":          ReasonTeamMemoryVersionConflict,
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
	if err := p.events.Append(ctx, event); err != nil && !errors.Is(err, observability.ErrEventAlreadyExists) {
		return err
	}
	return nil
}

func (p *Projector) emitTransition(ctx context.Context, typ string, prop centergit.ProposalView, actorRef string, occurredAt time.Time, commit string) error {
	if occurredAt.IsZero() {
		occurredAt = p.now()
	}
	key := strings.Join([]string{prop.TeamID, prop.ProposalID, string(prop.Status), typ, commit}, "\x00")
	eid := stableEventID(occurredAt, key)
	status := prop.Status
	if typ == "team_memory.proposed" {
		status = centergit.ProposalPending
	}
	payload := map[string]any{
		"reason":        "team_memory_transition",
		"message":       fmt.Sprintf("%s %s", typ, prop.ProposalID),
		"team_id":       prop.TeamID,
		"proposal_id":   prop.ProposalID,
		"operation":     string(prop.Operation),
		"target_kind":   string(prop.TargetKind),
		"status":        string(status),
		"source_path":   prop.Target.SourcePath,
		"new_commit":    commit,
		"warnings":      prop.Warnings,
		"effective_for": prop.EffectiveFor,
	}
	if typ == "team_memory.promoted" {
		payload["effective_for"] = "new_sessions_and_forks"
	}
	event, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(eid),
		OccurredAt: occurredAt,
		Seq:        p.seq.NextSeq(),
		EventType:  observability.EventType(typ),
		Refs:       observability.EventRefs{TeamID: prop.TeamID, ProposalID: prop.ProposalID},
		Actor:      observability.Actor(firstNonEmpty(actorRef, "system")),
		Payload:    payload,
		CreatedAt:  p.now(),
	})
	if err != nil {
		return err
	}
	if err := p.events.Append(ctx, event); err != nil && !errors.Is(err, observability.ErrEventAlreadyExists) {
		return err
	}
	return nil
}

func (p *Projector) updateCheckpoint(ctx context.Context, teamID, commit string) error {
	if p.db == nil || strings.TrimSpace(teamID) == "" {
		return nil
	}
	_, err := p.db.ExecContext(ctx, `INSERT INTO team_memory_observability_checkpoints
		(team_id, last_projected_commit, updated_at) VALUES (?,?,?)
		ON CONFLICT(team_id) DO UPDATE SET
		last_projected_commit=excluded.last_projected_commit,
		updated_at=excluded.updated_at`,
		teamID, commit, p.now().UTC().Format(time.RFC3339Nano))
	return err
}

func stableEventID(ts time.Time, key string) string {
	if ts.IsZero() {
		ts = time.Unix(0, 0).UTC()
	}
	sum := sha256.Sum256([]byte(key))
	return ulid.MustNew(ulid.Timestamp(ts), bytes.NewReader(sum[:10])).String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
