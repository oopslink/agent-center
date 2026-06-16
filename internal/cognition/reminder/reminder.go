// Package reminder is the Reminder aggregate of the Cognition BC (design:
// docs/design/architecture/tactical/cognition/03-reminder.md, v0.1 I4). A
// Reminder is a standalone-lifecycle instruction to wake a target agent at a
// time (once) or on a schedule (cron) and deliver a text. The aggregate owns its
// state machine + invariants; persistence, scheduling and delivery live in the
// sqlite repo + projectors (§3.3–3.5). Pure domain: no clock, no I/O — the caller
// injects `now`/`at` times so next_run_at is a deterministic pure derivation.
package reminder

import (
	"errors"
	"fmt"
	"strings"
	"time"

	cronv3 "github.com/robfig/cron/v3"
)

// Domain errors (sentinel, BC-prefixed — §3.5).
var (
	ErrReminderNotFound      = errors.New("cognition: reminder not found")
	ErrReminderTerminal      = errors.New("cognition: reminder is in a terminal state (canceled/completed) and cannot be changed")
	ErrInvalidSchedule       = errors.New("cognition: invalid reminder schedule")
	ErrCrossProjectReminder  = errors.New("cognition: an agent may only set reminders for agents in its own project")
	ErrInvalidEndCondition   = errors.New("cognition: invalid reminder end condition")
	ErrReminderContentEmpty  = errors.New("cognition: reminder content required")
	ErrReminderRemindeeEmpty = errors.New("cognition: reminder remindee required")
)

// cronParser parses the standard 5-field cron expression (minute hour dom month
// dow). robfig/cron/v3 (§ owner decision: introduce robfig/cron/v3) does the
// parse + next-time math; we drive it with an explicit timezone (Invariant #7).
var cronParser = cronv3.NewParser(cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow)

// --- Schedule VO (§ Ubiquitous Language) -------------------------------------

// ScheduleKind discriminates the two schedule shapes.
type ScheduleKind string

const (
	ScheduleOnce ScheduleKind = "once"
	ScheduleCron ScheduleKind = "cron"
)

// Schedule is OnceSchedule{at} | CronSchedule{expr, timezone}. Timezone applies
// to cron only (Invariant #7); once is an absolute instant.
type Schedule struct {
	Kind     ScheduleKind
	OnceAt   time.Time // when Kind==once
	CronExpr string    // when Kind==cron
	Timezone string    // IANA tz name (cron); "" defaults to UTC
}

// OnceScheduleAt builds a one-shot schedule.
func OnceScheduleAt(at time.Time) Schedule { return Schedule{Kind: ScheduleOnce, OnceAt: at} }

// CronScheduleAt builds a recurring schedule.
func CronScheduleAt(expr, timezone string) Schedule {
	return Schedule{Kind: ScheduleCron, CronExpr: strings.TrimSpace(expr), Timezone: timezone}
}

// location resolves the schedule timezone (default UTC). Invalid tz → error so
// Validate can surface ErrInvalidSchedule rather than silently using UTC.
func (s Schedule) location() (*time.Location, error) {
	if strings.TrimSpace(s.Timezone) == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: bad timezone %q: %v", ErrInvalidSchedule, s.Timezone, err)
	}
	return loc, nil
}

// Validate checks schedule legality (Invariant #1). For once, `now` enforces the
// instant is in the future AT CREATION (callers pass the creation `now`); pass a
// zero `now` to skip the future check (e.g. rehydration / pure cron parse).
func (s Schedule) Validate(now time.Time) error {
	switch s.Kind {
	case ScheduleOnce:
		if s.OnceAt.IsZero() {
			return fmt.Errorf("%w: once schedule needs a time", ErrInvalidSchedule)
		}
		if !now.IsZero() && !s.OnceAt.After(now) {
			return fmt.Errorf("%w: once time %s is not in the future", ErrInvalidSchedule, s.OnceAt.Format(time.RFC3339))
		}
		return nil
	case ScheduleCron:
		if strings.TrimSpace(s.CronExpr) == "" {
			return fmt.Errorf("%w: cron schedule needs an expression", ErrInvalidSchedule)
		}
		if _, err := s.location(); err != nil {
			return err
		}
		if _, err := cronParser.Parse(s.CronExpr); err != nil {
			return fmt.Errorf("%w: bad cron expr %q: %v", ErrInvalidSchedule, s.CronExpr, err)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown schedule kind %q", ErrInvalidSchedule, s.Kind)
	}
}

// nextAfter is the pure next_run_at derivation (Invariant #8): the first firing
// strictly after `after`. For once it returns OnceAt when after<OnceAt, else a
// zero time (no further run). For cron it asks the parsed schedule in the
// schedule's timezone. Assumes the schedule already validated.
func (s Schedule) nextAfter(after time.Time) (time.Time, error) {
	switch s.Kind {
	case ScheduleOnce:
		if s.OnceAt.After(after) {
			return s.OnceAt, nil
		}
		return time.Time{}, nil
	case ScheduleCron:
		loc, err := s.location()
		if err != nil {
			return time.Time{}, err
		}
		sched, err := cronParser.Parse(s.CronExpr)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
		}
		return sched.Next(after.In(loc)), nil
	default:
		return time.Time{}, fmt.Errorf("%w: unknown schedule kind %q", ErrInvalidSchedule, s.Kind)
	}
}

// --- ReminderStatus (§ enum) -------------------------------------------------

type ReminderStatus string

const (
	StatusActive    ReminderStatus = "active"
	StatusPaused    ReminderStatus = "paused"
	StatusCompleted ReminderStatus = "completed"
	StatusCanceled  ReminderStatus = "canceled"
)

func (s ReminderStatus) IsValid() bool {
	switch s {
	case StatusActive, StatusPaused, StatusCompleted, StatusCanceled:
		return true
	}
	return false
}

// IsTerminal reports whether the status can never change again (Invariant #6).
func (s ReminderStatus) IsTerminal() bool { return s == StatusCompleted || s == StatusCanceled }

// --- EndCondition VO (§ enum, cron only) -------------------------------------

type EndConditionKind string

const (
	EndNever    EndConditionKind = "never"
	EndUntil    EndConditionKind = "until"
	EndMaxCount EndConditionKind = "max_count"
)

// EndCondition bounds a recurring reminder: never | until(date) | max_count(n).
type EndCondition struct {
	Kind     EndConditionKind
	Until    time.Time // when Kind==until
	MaxCount int       // when Kind==max_count (>=1)
}

// NeverEnd is the default (unbounded) end condition.
func NeverEnd() EndCondition { return EndCondition{Kind: EndNever} }

func (e EndCondition) validate() error {
	switch e.Kind {
	case EndNever:
		return nil
	case EndUntil:
		if e.Until.IsZero() {
			return fmt.Errorf("%w: until needs a date", ErrInvalidEndCondition)
		}
		return nil
	case EndMaxCount:
		if e.MaxCount < 1 {
			return fmt.Errorf("%w: max_count must be >= 1", ErrInvalidEndCondition)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown end condition %q", ErrInvalidEndCondition, e.Kind)
	}
}

// reached reports whether a recurring reminder should COMPLETE: firedCount is the
// count AFTER the just-recorded fire; nextRun is the candidate next run (zero =
// none). max_count completes once fired n times; until completes when the next
// run would fall after the Until instant (or there is no next run).
func (e EndCondition) reached(firedCount int, nextRun time.Time) bool {
	switch e.Kind {
	case EndMaxCount:
		return firedCount >= e.MaxCount
	case EndUntil:
		return nextRun.IsZero() || nextRun.After(e.Until)
	default: // never
		return nextRun.IsZero()
	}
}

// --- Reminder aggregate root (§3.1) ------------------------------------------

type ReminderID string

func (id ReminderID) String() string { return string(id) }

// Reminder is the aggregate root. Fields are private; mutate only through the
// lifecycle ops so invariants + version bumps stay enforced.
type Reminder struct {
	id              ReminderID
	organizationID  string
	projectID       string
	creatorRef      string
	remindeeAgentID string
	schedule        Schedule
	content         string
	status          ReminderStatus
	nextRunAt       *time.Time
	lastFiredAt     *time.Time
	skipIfOverlap   bool
	endCondition    EndCondition
	firedCount      int
	version         int
	createdAt       time.Time
	updatedAt       time.Time
}

// NewReminderInput is the Factory input (§3.6). The caller (app layer) resolves
// project membership and passes CreatorIsOwner / CreatorProjectID so the
// cross-project guard (Invariant #2) is enforced here without the domain reading
// Workforce/Identity.
type NewReminderInput struct {
	ID               string
	OrganizationID   string
	ProjectID        string // the reminder's project (the remindee's project)
	CreatorRef       string // user:owner or agent:<id>
	CreatorIsOwner   bool   // owner bypasses the cross-project guard
	CreatorProjectID string // creator agent's project (ignored when owner)
	RemindeeAgentID  string
	Schedule         Schedule
	Content          string
	SkipIfOverlap    bool
	EndCondition     EndCondition
	Now              time.Time // creation instant: future-check + initial next_run_at base
}

// NewReminder is the ReminderFactory (§3.6): validate schedule + guard, compute
// the initial next_run_at, and produce an ACTIVE reminder (version 1).
func NewReminder(in NewReminderInput) (*Reminder, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("cognition: reminder id required")
	}
	if strings.TrimSpace(in.RemindeeAgentID) == "" {
		return nil, ErrReminderRemindeeEmpty
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, ErrReminderContentEmpty
	}
	if in.Now.IsZero() {
		return nil, errors.New("cognition: creation time (now) required")
	}
	// Invariant #2 — cross-project guard. An agent creator may only target its own
	// project; owner may cross projects.
	if !in.CreatorIsOwner && in.CreatorProjectID != in.ProjectID {
		return nil, ErrCrossProjectReminder
	}
	if err := in.Schedule.Validate(in.Now); err != nil {
		return nil, err
	}
	if err := in.EndCondition.validate(); err != nil {
		return nil, err
	}
	// Initial next_run_at derived strictly after Now (Invariant #8).
	next, err := in.Schedule.nextAfter(in.Now)
	if err != nil {
		return nil, err
	}
	if next.IsZero() {
		return nil, fmt.Errorf("%w: schedule yields no future run", ErrInvalidSchedule)
	}
	r := &Reminder{
		id:              ReminderID(in.ID),
		organizationID:  in.OrganizationID,
		projectID:       in.ProjectID,
		creatorRef:      in.CreatorRef,
		remindeeAgentID: in.RemindeeAgentID,
		schedule:        in.Schedule,
		content:         strings.TrimSpace(in.Content),
		status:          StatusActive,
		nextRunAt:       &next,
		skipIfOverlap:   in.SkipIfOverlap,
		endCondition:    in.EndCondition,
		firedCount:      0,
		version:         1,
		createdAt:       in.Now,
		updatedAt:       in.Now,
	}
	return r, nil
}

// RehydrateInput rebuilds a Reminder from persisted state (repo only). No
// validation/derivation — trusts the row.
type RehydrateInput struct {
	ID              string
	OrganizationID  string
	ProjectID       string
	CreatorRef      string
	RemindeeAgentID string
	Schedule        Schedule
	Content         string
	Status          ReminderStatus
	NextRunAt       *time.Time
	LastFiredAt     *time.Time
	SkipIfOverlap   bool
	EndCondition    EndCondition
	FiredCount      int
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Rehydrate reconstructs a Reminder from a repository row.
func Rehydrate(in RehydrateInput) (*Reminder, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("cognition: reminder id required")
	}
	if !in.Status.IsValid() {
		return nil, fmt.Errorf("cognition: invalid reminder status %q", in.Status)
	}
	if in.Version < 1 {
		return nil, errors.New("cognition: reminder version must be >= 1")
	}
	return &Reminder{
		id:              ReminderID(in.ID),
		organizationID:  in.OrganizationID,
		projectID:       in.ProjectID,
		creatorRef:      in.CreatorRef,
		remindeeAgentID: in.RemindeeAgentID,
		schedule:        in.Schedule,
		content:         in.Content,
		status:          in.Status,
		nextRunAt:       in.NextRunAt,
		lastFiredAt:     in.LastFiredAt,
		skipIfOverlap:   in.SkipIfOverlap,
		endCondition:    in.EndCondition,
		firedCount:      in.FiredCount,
		version:         in.Version,
		createdAt:       in.CreatedAt,
		updatedAt:       in.UpdatedAt,
	}, nil
}

// Getters.
func (r *Reminder) ID() ReminderID             { return r.id }
func (r *Reminder) OrganizationID() string     { return r.organizationID }
func (r *Reminder) ProjectID() string          { return r.projectID }
func (r *Reminder) CreatorRef() string         { return r.creatorRef }
func (r *Reminder) RemindeeAgentID() string    { return r.remindeeAgentID }
func (r *Reminder) Schedule() Schedule         { return r.schedule }
func (r *Reminder) Content() string            { return r.content }
func (r *Reminder) Status() ReminderStatus     { return r.status }
func (r *Reminder) NextRunAt() *time.Time      { return r.nextRunAt }
func (r *Reminder) LastFiredAt() *time.Time    { return r.lastFiredAt }
func (r *Reminder) SkipIfOverlap() bool        { return r.skipIfOverlap }
func (r *Reminder) EndCondition() EndCondition { return r.endCondition }
func (r *Reminder) FiredCount() int            { return r.firedCount }
func (r *Reminder) Version() int               { return r.version }
func (r *Reminder) CreatedAt() time.Time       { return r.createdAt }
func (r *Reminder) UpdatedAt() time.Time       { return r.updatedAt }

// bump advances updatedAt (version is bumped by the repo on CAS save).
func (r *Reminder) bump(at time.Time) { r.updatedAt = at }

// Pause sets an active reminder aside (active→paused). A paused reminder does not
// compute next_run_at and does not fire (Invariant #3). Idempotent on paused.
func (r *Reminder) Pause(at time.Time) error {
	if r.status.IsTerminal() {
		return ErrReminderTerminal
	}
	if r.status == StatusPaused {
		return nil
	}
	r.status = StatusPaused
	r.nextRunAt = nil
	r.bump(at)
	return nil
}

// Resume returns a paused reminder to active and recomputes next_run_at strictly
// after `at`. Idempotent on active.
func (r *Reminder) Resume(at time.Time) error {
	if r.status.IsTerminal() {
		return ErrReminderTerminal
	}
	if r.status == StatusActive {
		return nil
	}
	next, err := r.schedule.nextAfter(at)
	if err != nil {
		return err
	}
	if next.IsZero() {
		// No future run (e.g. a once whose time has passed) → complete.
		r.status = StatusCompleted
		r.nextRunAt = nil
		r.bump(at)
		return nil
	}
	r.status = StatusActive
	r.nextRunAt = &next
	r.bump(at)
	return nil
}

// Update changes the schedule and/or content of a non-terminal reminder and
// recomputes next_run_at when the schedule changed (and the reminder is active).
// Pass schedule==nil to leave it unchanged; content=="" to leave it unchanged.
func (r *Reminder) Update(schedule *Schedule, content string, at time.Time) error {
	if r.status.IsTerminal() {
		return ErrReminderTerminal
	}
	if schedule != nil {
		if err := schedule.Validate(at); err != nil {
			return err
		}
		r.schedule = *schedule
		if r.status == StatusActive {
			next, err := schedule.nextAfter(at)
			if err != nil {
				return err
			}
			if next.IsZero() {
				return fmt.Errorf("%w: updated schedule yields no future run", ErrInvalidSchedule)
			}
			r.nextRunAt = &next
		}
	}
	if strings.TrimSpace(content) != "" {
		r.content = strings.TrimSpace(content)
	}
	r.bump(at)
	return nil
}

// Cancel terminally cancels a non-terminal reminder (active|paused → canceled).
func (r *Reminder) Cancel(at time.Time) error {
	if r.status.IsTerminal() {
		return ErrReminderTerminal
	}
	r.status = StatusCanceled
	r.nextRunAt = nil
	r.bump(at)
	return nil
}

// RecordFire applies a successful firing (§3.3): bumps last_fired_at + fired_count
// and either completes (once, or recurring whose EndCondition is reached) or
// recomputes next_run_at (recurring). Only an ACTIVE reminder fires (Invariant #3);
// a paused/terminal reminder returns an error so the scheduler skips it.
func (r *Reminder) RecordFire(at time.Time) error {
	if r.status.IsTerminal() {
		return ErrReminderTerminal
	}
	if r.status != StatusActive {
		return fmt.Errorf("cognition: cannot fire a %s reminder", r.status)
	}
	r.lastFiredAt = &at
	r.firedCount++
	if r.schedule.Kind == ScheduleOnce {
		// once fires exactly once → completed (Invariant #5).
		r.status = StatusCompleted
		r.nextRunAt = nil
		r.bump(at)
		return nil
	}
	// Recurring: derive the next run strictly after this fire, then test the
	// EndCondition (Invariant #5).
	next, err := r.schedule.nextAfter(at)
	if err != nil {
		return err
	}
	if r.endCondition.reached(r.firedCount, next) {
		r.status = StatusCompleted
		r.nextRunAt = nil
		r.bump(at)
		return nil
	}
	r.nextRunAt = &next
	r.bump(at)
	return nil
}

// IsDue reports whether an active reminder is due to fire at-or-before `now`
// (§3.3 FindDue predicate at the aggregate level). Paused/terminal are never due.
func (r *Reminder) IsDue(now time.Time) bool {
	if r.status != StatusActive || r.nextRunAt == nil {
		return false
	}
	return !r.nextRunAt.After(now)
}
