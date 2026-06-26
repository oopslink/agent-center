package reminder

import (
	"context"
	"time"
)

// FiringOutcome is the recorded result of a (would-be) fire — the append-only
// reminder_firings outcome column (§4/§D6).
type FiringOutcome string

const (
	// OutcomePending is a dispatched-but-not-yet-delivered fire (in flight). The
	// scheduler appends it at fire time; the delivery projector (F1) resolves it
	// to delivered/failed once it consumes the fired event. It is the "previous
	// occurrence not yet processed" signal the skip_if_overlap path reads.
	OutcomePending        FiringOutcome = "pending"
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

// ListFilter narrows ListByCreator / ListByRemindee by status (empty = all),
// and (for the *Page variants) carries a content search + sort + page window.
type ListFilter struct {
	Statuses []ReminderStatus
	// Q is a case-insensitive substring of the reminder content; "" = no search.
	Q string
	// SortColumn is a vetted sort key (created_at|updated_at|status|next_run_at);
	// "" = the default (created_at, id). SortDesc selects direction.
	SortColumn string
	SortDesc   bool
	// Limit (<=0 = no limit) / Offset are the page window (the *Page variants).
	Limit  int
	Offset int
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
	// Delete hard-removes a reminder and its append-only firing history (T477).
	// RowsAffected==0 on the reminders row → ErrReminderNotFound.
	Delete(ctx context.Context, id ReminderID) error
	// ListByCreator returns reminders created by creatorRef (optionally filtered).
	ListByCreator(ctx context.Context, creatorRef string, f ListFilter) ([]*Reminder, error)
	// ListByRemindee returns reminders targeting remindeeAgentID (optionally filtered).
	ListByRemindee(ctx context.Context, remindeeAgentID string, f ListFilter) ([]*Reminder, error)
	// ListByOrg returns every reminder in organizationID (optionally status-filtered)
	// — the org-wide "全部" view for the human web console (T207). Org-scoped by
	// construction (no cross-org leak); the handler gates who may see all.
	ListByOrg(ctx context.Context, organizationID string, f ListFilter) ([]*Reminder, error)
	// ListByCreatorPage / ListByRemindeePage / ListByOrgPage are the server-side
	// paginated variants: same scope + status/q filter, but ORDER BY + LIMIT/OFFSET
	// in SQL, returning the page slice + the TOTAL (pre-page) count for the web
	// console's paginated reminder list.
	ListByCreatorPage(ctx context.Context, creatorRef string, f ListFilter) ([]*Reminder, int, error)
	ListByRemindeePage(ctx context.Context, remindeeAgentID string, f ListFilter) ([]*Reminder, int, error)
	ListByOrgPage(ctx context.Context, organizationID string, f ListFilter) ([]*Reminder, int, error)
	// FindDue returns active reminders whose next_run_at <= now (§3.3 scan predicate).
	FindDue(ctx context.Context, now time.Time) ([]*Reminder, error)
	// AppendFiring writes one append-only reminder_firings row.
	AppendFiring(ctx context.Context, f Firing) error
	// HasPendingFiring reports whether the reminder has a still-in-flight fire —
	// a reminder_firings row with outcome=pending (dispatched but not yet
	// delivered). It is the skip_if_overlap "previous occurrence not processed"
	// predicate (§3.3 overlap). skipped_overlap/delivered/failed rows do not count.
	HasPendingFiring(ctx context.Context, reminderID string) (bool, error)
	// UpdateFiringOutcome resolves a firing's outcome by id (e.g. pending →
	// delivered once the delivery projector posts the DM). Idempotent on id; a
	// missing row is a no-op (the at-least-once projector may redeliver).
	UpdateFiringOutcome(ctx context.Context, firingID string, outcome FiringOutcome) error
	// ListFirings returns a reminder's trigger history (newest-first) — the
	// "历史触发" the UI shows, incl. overlap-skips (T207).
	ListFirings(ctx context.Context, reminderID string) ([]Firing, error)
}
