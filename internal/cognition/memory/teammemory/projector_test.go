package teammemory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
)

func TestProjectorReconcileTeamBackfillsAndIsIdempotent(t *testing.T) {
	created := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	reviewed := time.Date(2026, 8, 9, 1, 30, 0, 0, time.UTC)
	repo := &projectorFakeRepo{views: []ProposalView{
		{Proposal: Proposal{
			TeamID: "team-1", ProposalID: "proposal-pending", Operation: OperationAdd, TargetKind: TargetRule,
			AuthorRef: "agent:author", CreatedAt: created, Status: StatusPending,
			Candidate: &Candidate{Slug: "do-not-project-body", Description: "desc", Body: "secret body must not be projected"},
		}, RepoCommit: "commit-1"},
		{Proposal: Proposal{
			TeamID: "team-1", ProposalID: "proposal-promoted", Operation: OperationUpdate, TargetKind: TargetEntry,
			AuthorRef: "agent:author", CreatedAt: created, Status: StatusPromoted,
			ReviewerRef: "user:owner", ReviewedAt: reviewed, PromotionCommit: "commit-2",
		}, RepoCommit: "commit-2"},
	}}
	events := &projectorFakeEvents{events: map[observability.EventID]*observability.Event{}}
	seq := &projectorFakeSeq{}
	projector := NewProjector(nil, repo, events, seq)

	if err := projector.ReconcileTeam(context.Background(), "team-1"); err != nil {
		t.Fatalf("ReconcileTeam: %v", err)
	}
	if got := events.countByType("team_memory.proposed"); got != 2 {
		t.Fatalf("proposed events = %d, want 2", got)
	}
	if got := events.countByType("team_memory.promoted"); got != 1 {
		t.Fatalf("promoted events = %d, want 1", got)
	}
	for _, ev := range events.events {
		if ev.Refs().TeamID != "team-1" || ev.Refs().ProposalID == "" {
			t.Fatalf("bad event refs: %+v", ev.Refs())
		}
		if _, leaked := ev.Payload()["candidate"]; leaked {
			t.Fatalf("projected payload leaked candidate content: %#v", ev.Payload())
		}
	}

	if err := projector.ReconcileTeam(context.Background(), "team-1"); err != nil {
		t.Fatalf("second ReconcileTeam: %v", err)
	}
	if len(events.events) != 3 {
		t.Fatalf("second reconcile appended duplicates: got %d events, want 3", len(events.events))
	}
}

type projectorFakeRepo struct {
	views []ProposalView
}

func (r *projectorFakeRepo) Propose(context.Context, string, ProposeCommand) (Result, error) {
	return Result{}, errors.New("unused")
}

func (r *projectorFakeRepo) List(_ context.Context, teamID string, filter Filter) (ListResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = len(r.views)
	}
	offset := filter.Offset
	if offset > len(r.views) {
		offset = len(r.views)
	}
	end := offset + limit
	if end > len(r.views) {
		end = len(r.views)
	}
	return ListResult{
		TeamID:     teamID,
		RepoCommit: "commit-2",
		Proposals:  r.views[offset:end],
		Total:      len(r.views),
		HasMore:    end < len(r.views),
	}, nil
}

func (r *projectorFakeRepo) Get(context.Context, string, string) (ProposalView, error) {
	return ProposalView{}, errors.New("unused")
}

func (r *projectorFakeRepo) Review(context.Context, string, ReviewCommand) (Result, error) {
	return Result{}, errors.New("unused")
}

type projectorFakeEvents struct {
	events map[observability.EventID]*observability.Event
}

func (r *projectorFakeEvents) Append(_ context.Context, event *observability.Event) error {
	if _, exists := r.events[event.ID()]; exists {
		return observability.ErrEventAlreadyExists
	}
	r.events[event.ID()] = event
	return nil
}

func (r *projectorFakeEvents) FindByID(_ context.Context, id observability.EventID) (*observability.Event, error) {
	event, ok := r.events[id]
	if !ok {
		return nil, observability.ErrEventNotFound
	}
	return event, nil
}

func (r *projectorFakeEvents) Find(context.Context, observability.EventQueryFilter) ([]*observability.Event, error) {
	return nil, nil
}

func (r *projectorFakeEvents) countByType(eventType string) int {
	n := 0
	for _, event := range r.events {
		if event.Type().String() == eventType {
			n++
		}
	}
	return n
}

type projectorFakeSeq struct {
	seq int64
}

func (s *projectorFakeSeq) NextSeq() int64 {
	s.seq++
	return s.seq
}
