package service

import (
	"context"
	"errors"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// resolved_issue_closer.go — automatically close issues that have sat in
// `resolved` for the grace period. The status_changed_at column is the durable
// "resolved since" timestamp, so no new schema/latch is needed.

const (
	ResolvedIssueCloseDefaultDelay = 72 * time.Hour
	ResolvedIssueCloseDefaultTick  = 1 * time.Hour
)

// CloseResolvedIssues scans resolved issues and moves those whose status has been
// unchanged for delay into closed. Each candidate is re-read in its own tx before
// closing, so a concurrent reopen/status edit wins cleanly. Returns the number of
// issues actually closed.
func (s *Service) CloseResolvedIssues(ctx context.Context, delay time.Duration) (int, error) {
	if delay <= 0 {
		delay = ResolvedIssueCloseDefaultDelay
	}
	now := s.clock.Now()
	cutoff := now.Add(-delay)
	resolved, err := s.issues.FindByStatuses(ctx, []pm.IssueStatus{pm.IssueResolved}, 0)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, snap := range resolved {
		if snap.StatusChangedAt().After(cutoff) {
			continue
		}
		issueID := snap.ID()
		var did bool
		if err := s.runInTx(ctx, func(txCtx context.Context) error {
			i, ferr := s.issues.FindByID(txCtx, issueID)
			if ferr != nil {
				return ferr
			}
			if i.Status() != pm.IssueResolved || i.StatusChangedAt().After(cutoff) {
				return nil
			}
			if err := s.requireProjectMutable(txCtx, i.ProjectID()); err != nil {
				if errors.Is(err, pm.ErrProjectArchived) {
					return nil
				}
				return err
			}
			prev := i.Status()
			if err := i.SetStatus(pm.IssueClosed, now); err != nil {
				return err
			}
			if err := s.issues.Update(txCtx, i); err != nil {
				return err
			}
			did = true
			if err := s.emit(txCtx, EvtIssueStateChanged,
				refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
				issueEventPayload{
					IssueID: string(i.ID()), ProjectID: string(i.ProjectID()),
					OwnerRef: "pm://issues/" + string(i.ID()), Status: string(i.Status()),
				}); err != nil {
				return err
			}
			// audit §5: system-driven auto-close — actor is 'system:resolved-issue-closer'
			// (design §5: never blank / never misattributed to the object owner).
			s.auditIssueStatusChange(txCtx, i, prev, pm.AuditIssueAutoClosed, pm.SystemActor("resolved-issue-closer"))
			return nil
		}); err != nil {
			return closed, err
		}
		if did {
			closed++
		}
	}
	return closed, nil
}

// ResolvedIssueCloser periodically runs CloseResolvedIssues. It mirrors the
// LeaseChecker / PlanReconcileLoop shape: single goroutine, no overlapping sweeps,
// errors logged and retried on the next tick.
type ResolvedIssueCloser struct {
	svc   *Service
	delay time.Duration
	tick  time.Duration
	log   func(string, ...any)
}

func NewResolvedIssueCloser(svc *Service, delay, tick time.Duration, log func(string, ...any)) *ResolvedIssueCloser {
	if delay <= 0 {
		delay = ResolvedIssueCloseDefaultDelay
	}
	if tick <= 0 {
		tick = ResolvedIssueCloseDefaultTick
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &ResolvedIssueCloser{svc: svc, delay: delay, tick: tick, log: log}
}

func (c *ResolvedIssueCloser) Tick(ctx context.Context) (int, error) {
	return c.svc.CloseResolvedIssues(ctx, c.delay)
}

func (c *ResolvedIssueCloser) Run(ctx context.Context) error {
	t := time.NewTicker(c.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if n, err := c.Tick(ctx); err != nil {
				c.log("resolved-issue-closer: tick failed: %v", err)
			} else if n > 0 {
				c.log("resolved-issue-closer: closed %d resolved issue(s)", n)
			}
		}
	}
}
