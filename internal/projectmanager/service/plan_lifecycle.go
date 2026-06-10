package service

import (
	"context"
	"fmt"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanDispatcher posts the node-ready @mention into the Plan's conversation
// (v2.9 #285, design §4/§9.3). Dispatch = posting `@assignee …ready` into the
// 1:1 Plan conversation; the existing wake+mention path (#220) wakes an agent
// assignee, and a human is notified. It is the SOLE cross-BC effect the advance
// orchestrator triggers, kept behind a narrow interface so the pm Service stays
// decoupled from the Conversation BC (the production wiring adapts MessageWriter).
//
// PostMention returns the new message id, which AdvancePlan persists as the
// dispatch record's dispatch_message_id (§9.3). It is OPTIONAL on the Service
// (nil ⇒ AdvancePlan returns ErrDispatcherUnavailable, fail-loud).
type PlanDispatcher interface {
	PostMention(ctx context.Context, conversationID, assigneeRef, content string) (messageID string, err error)
}

// StartPlan validates and moves a Plan draft→running (§9.6). Rejects unless:
//
//	(a) the DAG is acyclic (ValidateNoCycle over the plan's edges);
//	(b) the Plan has ≥1 selected task;
//	(c) EVERY task has a resolvable assignee — identity present, and if an agent,
//	    the agent exists and is not archived/deleted (verified via AgentDirectory,
//	    same OrgOfAgent probe AssignTask uses: an unresolvable agent errors);
//	(d) every task belongs to the Plan's project.
//
// Pre-done tasks are allowed (counted satisfied immediately, §9.6). The actor
// must be a project member. After validation it calls plan.Start + plans.Update.
func (s *Service) StartPlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		tasks, err := s.tasks.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		// (b) ≥1 task.
		if len(tasks) == 0 {
			return pm.ErrPlanNoTasks
		}
		// (a) acyclic DAG.
		edges, err := s.plans.ListDependencies(txCtx, planID)
		if err != nil {
			return err
		}
		if err := pm.ValidateNoCycle(edges); err != nil {
			return err
		}
		// (c)+(d): every task resolvable-assignee + same project.
		for _, t := range tasks {
			if t.ProjectID() != p.ProjectID() {
				return pm.ErrPlanProjectMismatch
			}
			if err := s.validateResolvableAssignee(txCtx, p, t); err != nil {
				return err
			}
		}
		if err := p.Start(now); err != nil {
			return err
		}
		return s.plans.Update(txCtx, p)
	})
}

// validateResolvableAssignee enforces §9.6(c): the task must have an assignee,
// and an agent assignee must resolve to the Plan's project org (exists + not
// archived/deleted — an archived/deleted/cross-org agent is unresolvable via the
// directory, mirroring AssignTask's cross-org guard). A human (`user:`) assignee
// only needs to be present. A nil AgentDirectory with an agent assignee is a hard
// error (fail-closed, same as grantAgentProjectMembership).
func (s *Service) validateResolvableAssignee(ctx context.Context, p *pm.Plan, t *pm.Task) error {
	assignee := t.Assignee()
	if strings.TrimSpace(string(assignee)) == "" {
		return fmt.Errorf("%w: task %s has no assignee", pm.ErrPlanUnassignedTask, t.ID())
	}
	if !strings.HasPrefix(string(assignee), "agent:") {
		return nil // human assignee: presence is enough.
	}
	if s.agentDir == nil {
		return pm.ErrAgentDirectoryUnavailable
	}
	agentID := strings.TrimPrefix(string(assignee), "agent:")
	agentOrg, err := s.agentDir.OrgOfAgent(ctx, agentID)
	if err != nil {
		// Unresolvable agent (not found / archived / deleted) → reject (§9.6c).
		return fmt.Errorf("%w: agent assignee %s of task %s is unresolvable", pm.ErrPlanUnresolvableAssignee, assignee, t.ID())
	}
	proj, perr := s.projects.FindByID(ctx, p.ProjectID())
	if perr != nil {
		return perr
	}
	if agentOrg != proj.OrganizationID() {
		// Cross-org agent can never be dispatched into this plan's conversation.
		return pm.ErrCrossOrgAssignee
	}
	return nil
}

// AdvancePlan computes the Plan's DERIVED view and dispatches EVERY ready node
// that has no dispatch record yet (§9.3 + the all-ready lock): for each such
// node it posts an `@assignee …ready` message into the Plan conversation
// (PlanDispatcher.PostMention → the wake+mention path #220 wakes an agent) and
// writes the once-only dispatch record. It is IDEMPOTENT: a node already
// dispatched is skipped (no second @mention), so re-running advance / event
// replay / a second upstream completing never double-dispatches. After
// dispatching, if every node is `done` the Plan is marked done (§9.1).
//
// Returns the list of NEWLY-dispatched task ids (empty when nothing was ready or
// everything ready was already dispatched). The Plan MUST be running
// (ErrPlanNotRunning otherwise); the actor must be a project member. The message
// post + dispatch record + (optional) MarkDone all commit in ONE tx — RunInTx is
// reentrant, so the dispatcher's AddMessage joins this tx (atomic dispatch).
func (s *Service) AdvancePlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]pm.TaskID, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	if s.planDispatcher == nil {
		return nil, ErrDispatcherUnavailable
	}
	now := s.clock.Now()
	var dispatched []pm.TaskID
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		if p.Status() != pm.PlanRunning {
			return pm.ErrPlanNotRunning
		}
		if strings.TrimSpace(p.ConversationID()) == "" {
			// A running Plan must have its 1:1 conversation bound (#284) — dispatch
			// has nowhere to post otherwise (fail-loud, not silent).
			return fmt.Errorf("projectmanager: plan %s has no conversation to dispatch into", planID)
		}
		tasks, err := s.tasks.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		edges, err := s.plans.ListDependencies(txCtx, planID)
		if err != nil {
			return err
		}
		records, err := s.plans.ListDispatchRecords(txCtx, planID)
		if err != nil {
			return err
		}
		view := pm.ComputePlanView(tasks, edges, records)

		// Index task → assignee for the @mention.
		assigneeOf := make(map[pm.TaskID]pm.IdentityRef, len(tasks))
		titleOf := make(map[pm.TaskID]string, len(tasks))
		for _, t := range tasks {
			assigneeOf[t.ID()] = t.Assignee()
			titleOf[t.ID()] = t.Title()
		}

		// Dispatch every ready node (the ready-set is exactly the nodes with no
		// dispatch record — ComputePlanView derives `dispatched` from the records,
		// so a dispatched-but-not-running node is NodeDispatched, never NodeReady).
		for _, taskID := range view.ReadySet {
			assignee := assigneeOf[taskID]
			content := fmt.Sprintf("%s your task %q is ready — all upstream dependencies are done.", string(assignee), titleOf[taskID])
			msgID, perr := s.planDispatcher.PostMention(txCtx, p.ConversationID(), string(assignee), content)
			if perr != nil {
				return perr
			}
			if rerr := s.plans.RecordDispatch(txCtx, planID, taskID, now, msgID); rerr != nil {
				return rerr
			}
			dispatched = append(dispatched, taskID)
		}

		// §9.1: a Plan is done iff EVERY node is done. Mark it here so a final
		// advance (after the last task completes) transitions running→done.
		if view.AllDone {
			if merr := p.MarkDone(now); merr != nil {
				return merr
			}
			if uerr := s.plans.Update(txCtx, p); uerr != nil {
				return uerr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dispatched, nil
}

// StopPlan moves a running Plan back to draft (§9.4) so its DAG/tasks become
// editable again. The actor must be a project member.
func (s *Service) StopPlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		if err := p.Stop(now); err != nil {
			return err
		}
		return s.plans.Update(txCtx, p)
	})
}

// RerunFailedNode clears one node's dispatch record (§9.3 creator re-run) so the
// next advance re-dispatches it. Used to resolve a failed node after the creator
// reopens/restarts the underlying task. The actor must be a project member; the
// Plan must be running. Normal advance never clears — only this explicit path.
func (s *Service) RerunFailedNode(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		return s.plans.ClearDispatch(txCtx, planID, taskID)
	})
}
