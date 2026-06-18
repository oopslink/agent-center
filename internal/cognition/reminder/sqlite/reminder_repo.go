// Package sqlite is the SQLite adapter for the Cognition Reminder aggregate
// (design 03-reminder.md §4). It implements reminder.Repository with
// version-CAS Update and honors persistence.ExecutorFromCtx so a Fire composes
// the RecordFire + reminder_firings write + outbox event into ONE tx.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ReminderRepo implements reminder.Repository over SQLite.
type ReminderRepo struct{ db *sql.DB }

// NewReminderRepo constructs the repo.
func NewReminderRepo(db *sql.DB) *ReminderRepo { return &ReminderRepo{db: db} }

// compile-time interface check.
var _ reminder.Repository = (*ReminderRepo)(nil)

const reminderCols = `id, organization_id, project_id, creator_ref, remindee_agent_id, schedule, content,
	status, next_run_at, last_fired_at, skip_if_overlap, end_condition, fired_count, version, created_at, updated_at`

func (r *ReminderRepo) Save(ctx context.Context, rm *reminder.Reminder) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	sched, err := encodeSchedule(rm.Schedule())
	if err != nil {
		return err
	}
	end, err := encodeEndCondition(rm.EndCondition())
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx,
		`INSERT INTO reminders (`+reminderCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rm.ID().String(), rm.OrganizationID(), rm.ProjectID(), rm.CreatorRef(), rm.RemindeeAgentID(),
		sched, rm.Content(), string(rm.Status()), tsPtr(rm.NextRunAt()), tsPtr(rm.LastFiredAt()),
		boolToInt(rm.SkipIfOverlap()), end, rm.FiredCount(), rm.Version(), ts(rm.CreatedAt()), ts(rm.UpdatedAt()))
	return err
}

// Update persists a mutated reminder with optimistic concurrency on version
// (§4: RowsAffected==0 → CAS failure mapped to a domain error). It gates on the
// in-memory (loaded) version and increments the stored version by one.
func (r *ReminderRepo) Update(ctx context.Context, rm *reminder.Reminder) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	sched, err := encodeSchedule(rm.Schedule())
	if err != nil {
		return err
	}
	end, err := encodeEndCondition(rm.EndCondition())
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx,
		`UPDATE reminders SET schedule=?, content=?, status=?, next_run_at=?, last_fired_at=?,
			skip_if_overlap=?, end_condition=?, fired_count=?, version=version+1, updated_at=?
		 WHERE id=? AND version=?`,
		sched, rm.Content(), string(rm.Status()), tsPtr(rm.NextRunAt()), tsPtr(rm.LastFiredAt()),
		boolToInt(rm.SkipIfOverlap()), end, rm.FiredCount(), ts(rm.UpdatedAt()),
		rm.ID().String(), rm.Version())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either the row is gone or another writer bumped the version first.
		return reminder.ErrReminderNotFound
	}
	return nil
}

func (r *ReminderRepo) Get(ctx context.Context, id reminder.ReminderID) (*reminder.Reminder, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, `SELECT `+reminderCols+` FROM reminders WHERE id=?`, id.String())
	rm, err := scanReminder(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, reminder.ErrReminderNotFound
	}
	return rm, err
}

func (r *ReminderRepo) ListByCreator(ctx context.Context, creatorRef string, f reminder.ListFilter) ([]*reminder.Reminder, error) {
	return r.list(ctx, `WHERE creator_ref=?`, creatorRef, f)
}

func (r *ReminderRepo) ListByRemindee(ctx context.Context, remindeeAgentID string, f reminder.ListFilter) ([]*reminder.Reminder, error) {
	return r.list(ctx, `WHERE remindee_agent_id=?`, remindeeAgentID, f)
}

// ListByOrg returns every reminder in organizationID (T207 — the web console
// "全部" view), org-scoped by construction so no cross-org row leaks.
func (r *ReminderRepo) ListByOrg(ctx context.Context, organizationID string, f reminder.ListFilter) ([]*reminder.Reminder, error) {
	return r.list(ctx, `WHERE organization_id=?`, organizationID, f)
}

// FindDue returns active reminders whose next_run_at <= now (§3.3 scan predicate),
// oldest-due first so a backlog drains in order.
func (r *ReminderRepo) FindDue(ctx context.Context, now time.Time) ([]*reminder.Reminder, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT `+reminderCols+` FROM reminders
		 WHERE status=? AND next_run_at IS NOT NULL AND next_run_at <= ?
		 ORDER BY next_run_at, id`,
		string(reminder.StatusActive), ts(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectReminders(rows)
}

func (r *ReminderRepo) AppendFiring(ctx context.Context, fr reminder.Firing) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO reminder_firings (id, reminder_id, fired_at, outcome, detail) VALUES (?,?,?,?,?)`,
		fr.ID, fr.ReminderID, ts(fr.FiredAt), string(fr.Outcome), fr.Detail)
	return err
}

// HasPendingFiring reports whether the reminder has a still-in-flight fire (a
// reminder_firings row with outcome=pending) — the skip_if_overlap predicate.
func (r *ReminderRepo) HasPendingFiring(ctx context.Context, reminderID string) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var pending int
	err := exec.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM reminder_firings WHERE reminder_id=? AND outcome=?)`,
		reminderID, string(reminder.OutcomePending)).Scan(&pending)
	if err != nil {
		return false, err
	}
	return pending != 0, nil
}

// UpdateFiringOutcome resolves a firing's outcome by id (pending → delivered once
// the delivery projector posts the DM). Idempotent; a missing/already-resolved row
// is a no-op (RowsAffected==0 is not an error — the at-least-once projector may
// run again).
func (r *ReminderRepo) UpdateFiringOutcome(ctx context.Context, firingID string, outcome reminder.FiringOutcome) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE reminder_firings SET outcome=? WHERE id=?`, string(outcome), firingID)
	return err
}

// ListFirings returns a reminder's trigger history newest-first (T207 历史触发).
func (r *ReminderRepo) ListFirings(ctx context.Context, reminderID string) ([]reminder.Firing, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT id, reminder_id, fired_at, outcome, detail FROM reminder_firings
		 WHERE reminder_id=? ORDER BY fired_at DESC, id DESC`, reminderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reminder.Firing
	for rows.Next() {
		var f reminder.Firing
		var firedAt, outcome string
		if err := rows.Scan(&f.ID, &f.ReminderID, &firedAt, &outcome, &f.Detail); err != nil {
			return nil, err
		}
		f.Outcome = reminder.FiringOutcome(outcome)
		f.FiredAt = parseTime(firedAt)
		out = append(out, f)
	}
	return out, rows.Err()
}

// list runs a filtered creator/remindee query (optionally narrowing status).
func (r *ReminderRepo) list(ctx context.Context, where, arg string, f reminder.ListFilter) ([]*reminder.Reminder, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := `SELECT ` + reminderCols + ` FROM reminders ` + where
	args := []any{arg}
	if len(f.Statuses) > 0 {
		q += ` AND status IN (` + placeholders(len(f.Statuses)) + `)`
		for _, s := range f.Statuses {
			args = append(args, string(s))
		}
	}
	q += ` ORDER BY created_at, id`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectReminders(rows)
}

func collectReminders(rows *sql.Rows) ([]*reminder.Reminder, error) {
	var out []*reminder.Reminder
	for rows.Next() {
		rm, err := scanReminder(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rm)
	}
	return out, rows.Err()
}

// scanReminder rehydrates a Reminder from a row scanner.
func scanReminder(scan func(...any) error) (*reminder.Reminder, error) {
	var (
		id, org, proj, creator, remindee, schedRaw, content, status, endRaw string
		nextRaw, lastRaw                                                    sql.NullString
		skip, firedCount, version                                           int
		createdRaw, updatedRaw                                              string
	)
	if err := scan(&id, &org, &proj, &creator, &remindee, &schedRaw, &content, &status,
		&nextRaw, &lastRaw, &skip, &endRaw, &firedCount, &version, &createdRaw, &updatedRaw); err != nil {
		return nil, err
	}
	sched, err := decodeSchedule(schedRaw)
	if err != nil {
		return nil, err
	}
	end, err := decodeEndCondition(endRaw)
	if err != nil {
		return nil, err
	}
	return reminder.Rehydrate(reminder.RehydrateInput{
		ID: id, OrganizationID: org, ProjectID: proj, CreatorRef: creator, RemindeeAgentID: remindee,
		Schedule: sched, Content: content, Status: reminder.ReminderStatus(status),
		NextRunAt: parseTimePtr(nextRaw), LastFiredAt: parseTimePtr(lastRaw),
		SkipIfOverlap: skip != 0, EndCondition: end, FiredCount: firedCount, Version: version,
		CreatedAt: parseTime(createdRaw), UpdatedAt: parseTime(updatedRaw),
	})
}

// --- VO JSON codecs ---------------------------------------------------------

type scheduleDTO struct {
	Kind     string     `json:"kind"`
	OnceAt   *time.Time `json:"once_at,omitempty"`
	CronExpr string     `json:"cron_expr,omitempty"`
	Timezone string     `json:"timezone,omitempty"`
}

func encodeSchedule(s reminder.Schedule) (string, error) {
	dto := scheduleDTO{Kind: string(s.Kind), CronExpr: s.CronExpr, Timezone: s.Timezone}
	if s.Kind == reminder.ScheduleOnce {
		at := s.OnceAt
		dto.OnceAt = &at
	}
	b, err := json.Marshal(dto)
	return string(b), err
}

func decodeSchedule(raw string) (reminder.Schedule, error) {
	var dto scheduleDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		return reminder.Schedule{}, err
	}
	s := reminder.Schedule{Kind: reminder.ScheduleKind(dto.Kind), CronExpr: dto.CronExpr, Timezone: dto.Timezone}
	if dto.OnceAt != nil {
		s.OnceAt = *dto.OnceAt
	}
	return s, nil
}

type endConditionDTO struct {
	Kind     string     `json:"kind"`
	Until    *time.Time `json:"until,omitempty"`
	MaxCount int        `json:"max_count,omitempty"`
}

func encodeEndCondition(e reminder.EndCondition) (string, error) {
	dto := endConditionDTO{Kind: string(e.Kind), MaxCount: e.MaxCount}
	if e.Kind == reminder.EndUntil {
		u := e.Until
		dto.Until = &u
	}
	b, err := json.Marshal(dto)
	return string(b), err
}

func decodeEndCondition(raw string) (reminder.EndCondition, error) {
	var dto endConditionDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		return reminder.EndCondition{}, err
	}
	e := reminder.EndCondition{Kind: reminder.EndConditionKind(dto.Kind), MaxCount: dto.MaxCount}
	if dto.Until != nil {
		e.Until = *dto.Until
	}
	return e, nil
}

// --- small helpers (package-local; the pm/sqlite ones are unexported there) --

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func parseTime(s string) time.Time {
	v, _ := time.Parse(time.RFC3339Nano, s)
	return v.UTC()
}

func parseTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*n-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}
