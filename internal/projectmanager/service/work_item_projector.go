package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// WorkItemProjector is the B2-c projector that turns Task assignment into Agent
// work (ADR-0049 §3, plan §4.2). It consumes pm.task.assigned / pm.task.reassigned
// and, when the assignee is an Agent, supersedes any prior live AgentWorkItem
// for the Task and creates a fresh queued one — so reassignment (and unblock,
// which we model as reassign) produces a new work segment while the Task keeps
// one stable Conversation. This completes the outbox wiring C2 (#100) deferred.
//
// Side effect + AppliedStore.MarkApplied run in the SAME transaction (finding 2).
type WorkItemProjector struct {
	db        *sql.DB
	workItems agent.WorkItemRepository
	applied   outbox.AppliedStore
	idgen     idgen.Generator
	clock     clock.Clock
}

// NewWorkItemProjector constructs the projector.
func NewWorkItemProjector(db *sql.DB, workItems agent.WorkItemRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *WorkItemProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WorkItemProjector{db: db, workItems: workItems, applied: applied, idgen: gen, clock: clk}
}

// Name is the AppliedStore key.
func (p *WorkItemProjector) Name() string { return "pm-workitem-sync" }

type workItemEvtPayload struct {
	OwnerRef string `json:"owner_ref"` // pm://tasks/{id} — used as the WorkItem.TaskRef
	Assignee string `json:"assignee"`
	Status   string `json:"status"`
}

// Project turns Task events into AgentWorkItem effects (plan §10 OQ11):
//   - assigned/reassigned → supersede prior live WorkItem + create a fresh
//     queued one when the assignee is an Agent (a new dispatch attempt).
//   - state_changed to blocked/canceled → CANCEL the live WorkItem (there is no
//     WorkItem `blocked`; a blocked/canceled Task ends the current attempt, and
//     the Agent goes idle → availability returns to available).
//
// Other events are a no-op.
func (p *WorkItemProjector) Project(ctx context.Context, e outbox.Event) error {
	var pl workItemEvtPayload
	dispatch := false
	cancelLive := false
	switch e.EventType {
	case EvtTaskAssigned, EvtTaskReassigned:
		dispatch = true
	case EvtTaskStateChanged:
		// fall through to parse + decide on status
	default:
		return nil
	}
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if e.EventType == EvtTaskStateChanged {
		switch pl.Status {
		case string(pm.TaskBlocked), string(pm.TaskCanceled):
			cancelLive = true
		default:
			return nil // other state changes don't affect WorkItems
		}
	}
	taskRef := pl.OwnerRef
	agentID, isAgent := agentIDFromRef(pl.Assignee)

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		existing, err := p.workItems.ListByTask(txCtx, taskRef)
		if err != nil {
			return err
		}
		for _, w := range existing {
			if w.Status().IsTerminal() {
				continue
			}
			switch {
			case dispatch:
				if err := w.Supersede(now); err != nil { // reassignment ends the prior attempt
					return err
				}
			case cancelLive:
				if err := w.Cancel(now); err != nil { // Task blocked/canceled ends the attempt
					return err
				}
			}
			if err := p.workItems.Update(txCtx, w); err != nil {
				return err
			}
		}
		// On dispatch, create a fresh queued WorkItem when the assignee is an
		// Agent (a Task may be assigned to a human, which has no AgentWorkItem).
		if dispatch && isAgent {
			nw, nerr := agent.NewWorkItem(agent.NewWorkItemInput{
				ID: p.idgen.NewULID(), AgentID: agentID, TaskRef: taskRef, CreatedAt: now,
			})
			if nerr != nil {
				return nerr
			}
			if serr := p.workItems.Save(txCtx, nw); serr != nil {
				return serr
			}
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// agentIDFromRef extracts the Agent id from an "agent:<id>" identity ref.
// Returns ok=false for non-agent assignees (humans).
func agentIDFromRef(ref string) (agent.AgentID, bool) {
	const p = "agent:"
	if strings.HasPrefix(ref, p) && len(ref) > len(p) {
		return agent.AgentID(strings.TrimPrefix(ref, p)), true
	}
	return "", false
}

var _ outbox.Projector = (*WorkItemProjector)(nil)
