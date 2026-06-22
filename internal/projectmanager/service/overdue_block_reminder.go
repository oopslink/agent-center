package service

import (
	"context"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// overdue_block_reminder.go (v2.14.0 I14/F3 §13.D) — the blocked-task overdue
// reminder. §12.6/§13.D: a BLOCKED task is a LEGAL pause and is NEVER reclaimed by
// the lease (so a stuck agent does not lose its work), but if the block is never
// resolved — the user never replies (input_required), or the owner/PM never clears
// the obstacle — the task would sit silently forever. §13.D's fix is "不自动回收 +
// 超期提醒 + 人工逃生口": this checker provides the OVERDUE REMINDER half (the escape
// hatch — owner reassign / discard — already exists).
//
// BC boundary (conventions §0): the ProjectManager owns "which tasks are overdue
// blocked"; it does NOT reach into the reminder/Conversation BCs to DELIVER the
// notification. It emits a `pm.task.block_overdue` outbox event (owner_ref +
// effective subscribers + reasonType + blocked_since); a downstream projector turns
// that into the owner-visible reminder (conversation message / cognition reminder),
// the same emit→project seam every other pm notification uses.

// EvtTaskBlockOverdue is the outbox event a blocked task emits once its block has
// outlived the overdue threshold (§13.D). Emitted at most once per block episode.
const EvtTaskBlockOverdue = "pm.task.block_overdue"

// Overdue-reminder tunables. Threshold 4h = how long a block may sit before the
// owner is nudged (well past a normal agent round-trip, short enough that a stuck
// task surfaces the same working day). Tick 5m = the sweep cadence. Both are
// overridable when wiring the checker.
const (
	BlockOverdueDefaultThreshold = 4 * time.Hour
	BlockOverdueDefaultTick      = 5 * time.Minute
)

// blockedSnapshot is one running+blocked task plus when its current block started
// (the latest 'blocked' action-log entry — re-blocking after an unblock starts a
// fresh episode, so the MAX occurred_at is the current episode's start).
type blockedSnapshot struct {
	task  *pm.Task
	since time.Time
}

// listBlockedWithSince returns every running task carrying a block annotation, paired
// with the start of its current block episode (from the action log). Tasks whose
// block start cannot be determined (no action-log repo wired, or no 'blocked' entry)
// are skipped — without a start time "overdue" is undefined. Internal helper for the
// OverdueBlockedReminder sweep.
func (s *Service) listBlockedWithSince(ctx context.Context) ([]blockedSnapshot, error) {
	if s.actionLogs == nil {
		return nil, nil // no log → no reliable block-start → nothing to sweep
	}
	running, err := s.tasks.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskRunning})
	if err != nil {
		return nil, err
	}
	var out []blockedSnapshot
	for _, t := range running {
		if strings.TrimSpace(t.BlockedReason()) == "" {
			continue // running but not blocked
		}
		logs, lerr := s.actionLogs.ListByTask(ctx, t.ID())
		if lerr != nil {
			return nil, lerr
		}
		since := latestBlockedAt(logs)
		if since.IsZero() {
			continue // can't date the block → skip
		}
		out = append(out, blockedSnapshot{task: t, since: since})
	}
	return out, nil
}

// latestBlockedAt returns the occurred_at of the most-recent 'blocked' log entry
// (the current block episode's start), or the zero time when there is none.
func latestBlockedAt(logs []pm.TaskActionLog) time.Time {
	var latest time.Time
	for _, lg := range logs {
		if lg.Action == pm.TaskActionBlocked && lg.OccurredAt.After(latest) {
			latest = lg.OccurredAt
		}
	}
	return latest
}

// taskBlockOverduePayload is the EvtTaskBlockOverdue body — everything a downstream
// projector needs to deliver the owner-visible reminder without re-reading the task.
type taskBlockOverduePayload struct {
	TaskID               string   `json:"task_id"`
	ProjectID            string   `json:"project_id"`
	OwnerRef             string   `json:"owner_ref"` // pm://tasks/{id}
	EffectiveSubscribers []string `json:"effective_subscribers"`
	Assignee             string   `json:"assignee,omitempty"`
	BlockReasonType      string   `json:"block_reason_type"`
	BlockedReason        string   `json:"blocked_reason"`
	BlockedSince         string   `json:"blocked_since"`   // RFC3339
	OverdueSeconds       int64    `json:"overdue_seconds"` // now - blocked_since
}

// emitTaskBlockOverdue emits EvtTaskBlockOverdue for an overdue-blocked task (§13.D).
// Carries the effective subscriber set so the projector can address the owner.
func (s *Service) emitTaskBlockOverdue(ctx context.Context, t *pm.Task, since, now time.Time) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtTaskBlockOverdue,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskBlockOverduePayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
			Assignee: string(t.Assignee()), BlockReasonType: string(t.BlockedReasonType()),
			BlockedReason: t.BlockedReason(), BlockedSince: since.UTC().Format(time.RFC3339),
			OverdueSeconds: int64(now.Sub(since).Seconds()),
		})
}

// OverdueBlockedReminder is the background loop that emits a one-time overdue reminder
// for each block episode that outlives the threshold (§13.D). It mirrors the
// LeaseChecker / AutoRedispatchReconciler wiring shape.
//
// The per-task `reminded` latch is EPISODE-scoped: emit at most once per block, and
// prune the latch when a task is no longer blocked (unblocked / completed / reclaimed)
// so a fresh block episode gets a fresh reminder. In-memory is sufficient — a process
// restart at worst re-reminds an already-overdue block once, which is harmless.
type OverdueBlockedReminder struct {
	svc       *Service
	clk       clock.Clock
	threshold time.Duration
	tick      time.Duration
	log       func(string, ...any)

	reminded map[pm.TaskID]bool
}

// NewOverdueBlockedReminder wires the checker. Zero threshold/tick → package defaults;
// nil clk → system clock; nil log → no-op.
func NewOverdueBlockedReminder(svc *Service, clk clock.Clock, threshold, tick time.Duration, log func(string, ...any)) *OverdueBlockedReminder {
	if threshold <= 0 {
		threshold = BlockOverdueDefaultThreshold
	}
	if tick <= 0 {
		tick = BlockOverdueDefaultTick
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &OverdueBlockedReminder{svc: svc, clk: clk, threshold: threshold, tick: tick, log: log, reminded: map[pm.TaskID]bool{}}
}

// Tick runs one sweep: emit a reminder for each newly-overdue block, and prune the
// latch for tasks no longer blocked. Returns the number of reminders emitted this
// sweep. Exposed for tests.
func (c *OverdueBlockedReminder) Tick(ctx context.Context) (int, error) {
	now := c.clk.Now()
	blocked, err := c.svc.listBlockedWithSince(ctx)
	if err != nil {
		return 0, err
	}
	live := make(map[pm.TaskID]bool, len(blocked))
	emitted := 0
	for _, b := range blocked {
		id := b.task.ID()
		live[id] = true
		if now.Sub(b.since) < c.threshold {
			continue // not overdue yet
		}
		if c.reminded[id] {
			continue // already reminded this episode
		}
		if eerr := c.svc.emitTaskBlockOverdue(ctx, b.task, b.since, now); eerr != nil {
			return emitted, eerr
		}
		c.reminded[id] = true
		emitted++
	}
	// Prune the latch: a task no longer blocked starts a fresh episode next time.
	for id := range c.reminded {
		if !live[id] {
			delete(c.reminded, id)
		}
	}
	return emitted, nil
}

// Run sweeps every tick until ctx is canceled (the long-lived server goroutine).
func (c *OverdueBlockedReminder) Run(ctx context.Context) error {
	t := time.NewTicker(c.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if n, err := c.Tick(ctx); err != nil {
				c.log("overdue-block-reminder: tick failed: %v", err)
			} else if n > 0 {
				c.log("overdue-block-reminder: reminded %d overdue-blocked task(s)", n)
			}
		}
	}
}
