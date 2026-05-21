package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// CrashRecovery handles center-restart orphan invocations + replay of
// missed events (plan-6 § 3.9).
type CrashRecovery struct {
	db         *sql.DB
	repo       cognition.SupervisorInvocationRepository
	eventRepo  observability.EventRepository
	sink       *observability.EventSink
	clk        clock.Clock
}

// NewCrashRecovery wires a CrashRecovery.
func NewCrashRecovery(db *sql.DB, repo cognition.SupervisorInvocationRepository, eventRepo observability.EventRepository, sink *observability.EventSink, clk clock.Clock) (*CrashRecovery, error) {
	if db == nil {
		return nil, errors.New("crash_recovery: db required")
	}
	if repo == nil {
		return nil, errors.New("crash_recovery: repo required")
	}
	if eventRepo == nil {
		return nil, errors.New("crash_recovery: event_repo required")
	}
	if sink == nil {
		return nil, errors.New("crash_recovery: sink required")
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &CrashRecovery{db: db, repo: repo, eventRepo: eventRepo, sink: sink, clk: clk}, nil
}

// Recover scans for orphan running invocations + transitions them to
// failed(center_restart_orphan). Returns the number transitioned and the
// new cursor (oldest unprocessed event id, if any) that the Coalescer
// should adopt to avoid missing wake events.
//
// Per plan-6 § 3.9 / cognition/00-overview § 3.4: also computes the
// "newest event_id already covered by a successful invocation" for cursor
// seeding — implementation is "minimum id from FindRunning before
// transition" as a conservative approximation.
func (cr *CrashRecovery) Recover(ctx context.Context) (transitioned int, replayCursor observability.EventID, err error) {
	orphans, err := cr.repo.FindRunning(ctx)
	if err != nil {
		return 0, "", err
	}
	now := cr.clk.Now()
	for _, inv := range orphans {
		// Compute replay cursor: earliest trigger_event_id - 1 (we seed
		// the new Coalescer cursor just before this event so it
		// re-processes the same triggers).
		ids := inv.TriggerEvents().IDs()
		if len(ids) > 0 {
			earliest := ids[0]
			if replayCursor == "" || earliest < replayCursor {
				replayCursor = earliest
			}
		}
		if err := inv.MarkFailed(cognition.FailedReasonCenterRestartOrphan,
			fmt.Sprintf("center restarted; orphan invocation marked failed at %s", now.UTC().Format(time.RFC3339Nano)),
			now); err != nil {
			return transitioned, replayCursor, err
		}
		if err := persistence.RunInTx(ctx, cr.db, func(txCtx context.Context) error {
			if err := cr.repo.UpdateStatusToTerminal(txCtx, inv); err != nil {
				return err
			}
			_, err := cr.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "supervisor.invocation_failed_alert",
				Refs:      refsForScope(inv.Scope()),
				Actor:     observability.Actor("supervisor:" + string(inv.ID())),
				Payload: map[string]any{
					"invocation_id": string(inv.ID()),
					"reason":        string(cognition.FailedReasonCenterRestartOrphan),
					"message":       inv.FailedMessage(),
					"ended_at":      now.UTC().Format(time.RFC3339Nano),
				},
				CorrelationID: string(inv.ID()),
			})
			return err
		}); err != nil {
			return transitioned, replayCursor, err
		}
		transitioned++
	}
	// Replay cursor must be the id *just before* the earliest event so
	// Find(cursor=X) returns it. SQLite text ordering: we use string-
	// just-before by stripping one char if non-empty.
	if replayCursor != "" {
		replayCursor = decrementULID(replayCursor)
	}
	return transitioned, replayCursor, nil
}

// decrementULID returns a string strictly less than id such that
// EventRepository.Find(Cursor=result) will include id in the result set.
// We use simple character substitution at the last byte to avoid heavy
// ULID arithmetic; sufficient for cursor purposes since IDs are
// monotonic and unique.
func decrementULID(id observability.EventID) observability.EventID {
	if id == "" {
		return ""
	}
	s := string(id)
	last := s[len(s)-1]
	if last > '0' {
		return observability.EventID(s[:len(s)-1] + string(rune(last-1)))
	}
	// Fall back to truncation if the last char is '0'.
	return observability.EventID(s[:len(s)-1])
}
