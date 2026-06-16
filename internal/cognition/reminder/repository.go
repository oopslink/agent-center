package reminder

import (
	"context"
	"time"
)

// FiringOutcome is the recorded result of a (would-be) fire — the append-only
// reminder_firings outcome column (§4/§D6).
type FiringOutcome string

const (
	OutcomeDelivered      FiringOutcome = "delivered"
	OutcomeSkippedOverlap FiringOutcome = "skipped_overlap"
	OutcomeFailed         FiringOutcome = "failed"
)

// Firing is one append-only row in reminder_firings (§4) — the trigger history
// the UI shows (incl. overlap-skips). No version (append-only, §4 convention).
type Firing struct {
	ID         string
	ReminderID string
	FiredAt    time.Time
	Outcome    FiringOutcome
	Detail     string
}

// ListFilter narrows ListByCreator / ListByRemindee by status (empty = all).
type ListFilter struct {
	Statuses []ReminderStatus
}

// Repository is the Reminder aggregate's persistence port (§3.5). Implementations
// live in the sqlite adapter. Tx is carried on the context (ExecutorFromCtx
// convention); Update is CAS on version (RowsAffected==0 → ErrReminderNotFound or
// a version-conflict surfaced by the caller).
type Repository interface {
	// Save inserts a new reminder (version 1).
	Save(ctx context.Context, r *Reminder) error
	// Update persists a mutated reminder with optimistic concurrency on version
	// (expects the in-memory version; the row must match, then version++).
	Update(ctx context.Context, r *Reminder) error
	// Get loads a reminder by id; ErrReminderNotFound if absent.
	Get(ctx context.Context, id ReminderID) (*Reminder, error)
	// ListByCreator returns reminders created by creatorRef (optionally filtered).
	ListByCreator(ctx context.Context, creatorRef string, f ListFilter) ([]*Reminder, error)
	// ListByRemindee returns reminders targeting remindeeAgentID (optionally filtered).
	ListByRemindee(ctx context.Context, remindeeAgentID string, f ListFilter) ([]*Reminder, error)
	// ListByOrg returns every reminder in organizationID (optionally status-filtered)
	// — the org-wide "全部" view for the human web console (T207). Org-scoped by
	// construction (no cross-org leak); the handler gates who may see all.
	ListByOrg(ctx context.Context, organizationID string, f ListFilter) ([]*Reminder, error)
	// FindDue returns active reminders whose next_run_at <= now (§3.3 scan predicate).
	FindDue(ctx context.Context, now time.Time) ([]*Reminder, error)
	// AppendFiring writes one append-only reminder_firings row.
	AppendFiring(ctx context.Context, f Firing) error
	// ListFirings returns a reminder's trigger history (newest-first) — the
	// "历史触发" the UI shows, incl. overlap-skips (T207).
	ListFirings(ctx context.Context, reminderID string) ([]Firing, error)
}
