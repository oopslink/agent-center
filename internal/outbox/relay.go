package outbox

import (
	"context"

	"github.com/oopslink/agent-center/internal/clock"
)

// Projector applies one outbox Event's cross-BC effect (e.g. sync
// ConversationParticipant from a ProjectManager subscriber change). A
// projector should be idempotent on Event.ID; the Relay additionally guards
// each projector with the AppliedStore so the body runs at most once per
// event even if the projector itself is not strictly idempotent.
type Projector interface {
	// Name is the stable projector identifier used as the AppliedStore key.
	Name() string
	// Project applies the event. Returning an error leaves the event
	// unprocessed for retry on the next relay pass.
	Project(ctx context.Context, e Event) error
}

// Relay drains the outbox: it fetches unprocessed events and dispatches each
// to every registered projector, skipping (projector, event) pairs already
// recorded in the AppliedStore. An event is marked processed only once all
// projectors have applied it. A0 ships the relay; wiring it to a background
// loop (and the first projector) lands in phase B.
type Relay struct {
	repo       Repository
	applied    AppliedStore
	projectors []Projector
	clock      clock.Clock
}

// NewRelay constructs a Relay.
func NewRelay(repo Repository, applied AppliedStore, clk clock.Clock, projectors ...Projector) *Relay {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Relay{repo: repo, applied: applied, projectors: projectors, clock: clk}
}

// RunOnce drains up to batchSize unprocessed events. It returns the number of
// events fully processed. Safe to call repeatedly (idempotent via the
// AppliedStore); a background loop just calls this on a tick.
func (r *Relay) RunOnce(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	events, err := r.repo.FetchUnprocessed(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, e := range events {
		allApplied := true
		for _, p := range r.projectors {
			already, err := r.applied.IsApplied(ctx, p.Name(), e.ID)
			if err != nil {
				return processed, err
			}
			if already {
				continue
			}
			if err := p.Project(ctx, e); err != nil {
				// Leave the event unprocessed; retry next pass.
				allApplied = false
				break
			}
			if err := r.applied.MarkApplied(ctx, p.Name(), e.ID, r.clock.Now()); err != nil {
				return processed, err
			}
		}
		if allApplied {
			if err := r.repo.MarkProcessed(ctx, e.ID, r.clock.Now()); err != nil {
				return processed, err
			}
			processed++
		}
	}
	return processed, nil
}
