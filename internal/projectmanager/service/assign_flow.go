package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Assign/state-flow AppServices (B2-c). All write ONLY ProjectManager state +
// an outbox event in one tx (OQ1). The cross-BC effects — creating/superseding
// AgentWorkItems and syncing Conversation participants — are handled by the
// projectors (ParticipantProjector + WorkItemProjector) consuming these events.

// AssignTask assigns (or reassigns) a Task to an identity. open→assigned, or
// re-target an already-assigned Task. On reassign the previous assignee leaves
// the effective subscriber set unless they are creator or a manual subscriber
// (falls out of EffectiveTaskSubscribers automatically). Emits pm.task.assigned
// (first assignment) or pm.task.reassigned.
func (s *Service) AssignTask(ctx context.Context, taskID pm.TaskID, assignee, actor pm.IdentityRef) error {
	if err := assignee.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject (re)assign on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		// T130 note (指派 vs 可运行): assignment is DELIBERATELY decoupled from
		// runnability. Assigning a task — even a backlog one — only records ownership
		// (and grants the agent project membership); it is a legitimate, widely-used
		// operation (e.g. assign before planning). What is gated is ENTERING RUNNING:
		// a backlog task is rejected at the open→running boundary — claim (T83) and
		// now start_work (the TaskRunGate, ErrWorkItemTaskNotRunnable) — NOT at
		// assign. So a directly-assigned backlog task can sit assigned but cannot run
		// until it is added to a real plan or dispatched into the Assignment Pool.
		prev := t.Assignee()
		if err := t.Assign(assignee, now); err != nil {
			return err
		}
		// #5a (ADR-0049/0052/OQ6): when the assignee is an AGENT, grant it
		// ProjectMember so it can pass the project write-gate for its MCP tools
		// (OQ4 = agents have project-level write). Cross-org-guarded + idempotent,
		// and in THIS tx so the assignment and membership commit atomically. Human
		// (`user:`) assignees are untouched.
		if err := s.grantAgentProjectMembership(txCtx, t, assignee, now); err != nil {
			return err
		}
		// OQ13: reassigning away keeps the outgoing assignee in the Conversation
		// as a sticky manual subscriber (not the new assignee, which is effective
		// via the assignee role). Must persist before emit so the recomputed
		// effective set includes them.
		if prev != "" && prev != assignee {
			if err := s.retainAsTaskSubscriber(txCtx, t, prev, now); err != nil {
				return err
			}
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		evt := EvtTaskAssigned
		if prev != "" {
			evt = EvtTaskReassigned
		}
		if err := s.emitTaskAssignEvent(txCtx, t, evt, string(prev)); err != nil {
			return err
		}
		// BUG B (v2.9 assign-after-select): if this task is already in a plan,
		// emit pm.plan.participants_changed so the NEW assignee additively joins the
		// Plan conversation (#284). SelectTaskIntoPlan only syncs the assignee that
		// existed AT SELECT TIME; an agent assigned LATER would otherwise be left out
		// of the plan conversation → the dispatch @mention (BUG C) can't reach it.
		return s.syncPlanParticipantOnAssign(txCtx, t, assignee)
	})
}

// syncPlanParticipantOnAssign emits pm.plan.participants_changed for the task's
// plan (if any) with `assignee` as an ADD-ONLY participant delta — mirroring
// SelectTaskIntoPlan's emit (plan_flow.go) so the assign-after-select sequence
// (task selected first, agent assigned later) still adds the agent to the Plan
// conversation (#284, additive). A task not in a plan (PlanID == "") or an empty
// assignee is a no-op (nothing to sync). Runs in the AssignTask tx.
func (s *Service) syncPlanParticipantOnAssign(ctx context.Context, t *pm.Task, assignee pm.IdentityRef) error {
	planID := t.PlanID()
	if planID == "" || strings.TrimSpace(string(assignee)) == "" {
		return nil
	}
	return s.emit(ctx, EvtPlanParticipantsChanged,
		refsJSON(map[string]string{"plan_id": string(planID), "project_id": string(t.ProjectID())}),
		planEventPayload{
			PlanID: string(planID), ProjectID: string(t.ProjectID()),
			OrganizationID: "", OwnerRef: "pm://plans/" + string(planID),
			Participants: []string{string(assignee)},
		})
}

// StartTask moves an assigned Task to running (the explicit "picked up/started"
// transition — needed before block/complete are reachable).
func (s *Service) StartTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error { return t.Start(now) }, "")
}

// DiscardTask discards a non-terminal Task (terminal "discarded"; was CancelTask
// pre-v2.8.1, uniform 废弃 semantic).
func (s *Service) DiscardTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error { return t.Discard(now) }, "")
}

// BlockTask records a stuck-reason ANNOTATION on a running task (ADR-0046: status
// stays running, no deadlock). A required reason.
func (s *Service) BlockTask(ctx context.Context, taskID pm.TaskID, reason string, actor pm.IdentityRef) error {
	// v2.14.0 I14 F1 compile-bridge: Task.Block now takes (reason, reasonType,
	// agentRef, at). The full BlockTask rewrite (reasonType param + Conversation
	// input_request write) lands in F3 (§13.A/13.C). Here we preserve the existing
	// behavior: pass the task's own assignee as agentRef (so the assignee check is a
	// no-op) and default the type to obstacle.
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error {
		return t.Block(reason, pm.BlockReasonObstacle, t.Assignee(), now)
	}, reason)
}

// UnblockTask clears a stuck (blocked_reason) annotation on a RUNNING task and
// re-dispatches it (ADR-0046). The task never left running, but its prior WorkItem
// terminated (failed) when it got stuck, so unblocking emits pm.task.assigned and
// the WorkItemProjector mints a fresh WorkItem. No-op if the task carries no
// blocked_reason (not stuck) — avoids a duplicate dispatch on an already-active task.
func (s *Service) UnblockTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject unblock on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		if strings.TrimSpace(t.BlockedReason()) == "" {
			return nil // not stuck → nothing to recover (idempotent, no double-dispatch)
		}
		// v2.14.0 I14 F1 compile-bridge: Unblock now takes (comment, actorRef, at).
		// Full rewrite (comment plumbing + Conversation input_reply) is F3 (§13.C).
		if err := t.Unblock("", actor, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, "")
	})
}

// CompleteTask moves running→completed and records the completer.
//
// v2.13.0 I18/F3: BEFORE the state transition, an Integrate cycle node must pass
// the merge guardrail — its feature branch must have merged back into the
// integration trunk on origin (docs/design/v2.13.0/cycle-node-graph-spec.md §5).
// The guard runs OUTSIDE the DB tx (git fetch/ancestry I/O is slow + must only
// trust origin), so it is a pre-check here, not inside taskStateOp's mutate. When
// it blocks, the task stays `running` (no transition, no event). The guard is
// nil-safe: with no MergeChecker wired it is disabled (pre-F3 behavior), and it
// targets ONLY role==integrate nodes — Dev/Review/Gate/Accept/Ship and every
// ordinary task complete normally.
func (s *Service) CompleteTask(ctx context.Context, taskID pm.TaskID, by pm.IdentityRef) error {
	if err := s.guardIntegrateMerge(ctx, taskID); err != nil {
		return err
	}
	return s.taskStateOp(ctx, taskID, by, func(t *pm.Task, now time.Time) error { return t.Complete(by, now) }, "")
}

// guardIntegrateMerge is the F3 Integrate-complete merge guardrail (v2.13.0 I18 —
// docs/design/v2.13.0/cycle-node-graph-spec.md §5). It returns nil (allow) for
// every case EXCEPT an Integrate node whose branch has not (or cannot be verified
// to have) merged back into origin/<base>:
//
//   - mergeChecker == nil                  → guard DISABLED (pre-F3) → allow.
//   - role != integrate                    → not the merge-check node → allow
//     (Dev/Review/Gate/Accept/Ship + ordinary tasks complete normally).
//   - skip_merge_check                      → structural exemption (doc-only) → allow.
//   - branch == "" || base == ""           → no merge target to check → allow.
//   - project has NO CodeRepoRef            → fail CLOSED (ErrIntegrateMergeUnverifiable):
//     the guard is enabled for this Integrate node but there is no repo to verify
//     against — refuse rather than wave it through.
//   - checker returns an error              → fail CLOSED (ErrIntegrateMergeUnverifiable,
//     wrapping the cause): a flaky/missing remote must not let an unmerged branch land.
//   - checker returns merged == false       → block (ErrIntegrateBranchNotMerged).
//   - checker returns merged == true        → allow.
//
// It runs OUTSIDE any tx (plain repo reads + git I/O); CompleteTask invokes it
// before opening the state-transition tx.
func (s *Service) guardIntegrateMerge(ctx context.Context, taskID pm.TaskID) error {
	if s.mergeChecker == nil {
		return nil // guard disabled (pre-F3 behavior)
	}
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Role() != pm.CycleRoleIntegrate {
		return nil // not an Integrate node — completes normally
	}
	if t.SkipMergeCheck() {
		return nil // structural exemption (doc-only / no-code node)
	}
	branch, base := t.Branch(), t.Base()
	if branch == "" || base == "" {
		return nil // no merge target configured — nothing to verify
	}
	url, err := s.primaryRepoURL(ctx, t.ProjectID())
	if err != nil {
		return err
	}
	if url == "" {
		// Enabled for this Integrate node but the project has no repo to check against.
		// Fail closed: the PD must add a CodeRepoRef or set skip_merge_check.
		return fmt.Errorf("%w: merge-check enabled for Integrate node %s but project %s has no code repo configured; add a CodeRepoRef or set skip_merge_check",
			ErrIntegrateMergeUnverifiable, t.ID(), t.ProjectID())
	}
	merged, err := s.mergeChecker.BranchMergedToOrigin(ctx, url, branch, base)
	if err != nil {
		// Could not verify (fetch/transport/ref error). Fail closed.
		return fmt.Errorf("%w: could not verify merge of %s into origin/%s against %s: %v; ensure the server can fetch the repo, or set skip_merge_check",
			ErrIntegrateMergeUnverifiable, branch, base, url, err)
	}
	if !merged {
		return fmt.Errorf("%w: branch %s has not landed on origin/%s — merge %s into %s and push to origin, then retry complete",
			ErrIntegrateBranchNotMerged, branch, base, branch, base)
	}
	return nil
}

// primaryRepoURL resolves the project's primary code-repo URL for the F3 merge
// guard: it lists the project's CodeRepoRefs (the repo's stable created-order, the
// same order ListCodeRepos returns) and returns the FIRST one's URL. "" when the
// project has no repo configured (the caller fails closed on that). A read-only
// helper (no tx).
func (s *Service) primaryRepoURL(ctx context.Context, projectID pm.ProjectID) (string, error) {
	refs, err := s.codeRepoRefs.ListByProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return "", nil
	}
	return refs[0].URL(), nil
}

// retainAsTaskSubscriber persists `identity` as a sticky MANUAL subscriber so a
// person taken off a Task (unassigned / reassigned away / reopened) stays in the
// task Conversation as a subscriber until explicitly Unsubscribed (OQ13 — task
// state resets but Conversation membership is monotonic). The creator and the
// empty ref are skipped (creator is always effective); Add is INSERT OR IGNORE,
// so re-retaining an existing manual row is a no-op. Must run inside the caller's
// tx so the subsequent effective-set recompute sees the new row.
func (s *Service) retainAsTaskSubscriber(ctx context.Context, t *pm.Task, identity pm.IdentityRef, now time.Time) error {
	if identity == "" || identity == t.CreatedBy() {
		return nil
	}
	sub, err := pm.NewTaskSubscriber(t.ID(), identity, "system", now)
	if err != nil {
		return err
	}
	return s.taskSubs.Add(ctx, sub)
}

// grantAgentProjectMembership makes an AGENT assignee a ProjectMember of the
// task's project so the agent passes the project write-gate (OQ6) for its MCP
// tools (#5a, ADR-0049/0052; OQ4 = agents get project-level write). It is:
//   - AGENT-ONLY: human (`user:`) assignees and `system` are skipped (the branch
//     keys on the `agent:` prefix) — they are never granted membership here.
//   - FAIL-CLOSED: for an AGENT assignee, a missing directory (s.agentDir == nil)
//     is a hard error (pm.ErrAgentDirectoryUnavailable) — a missing dependency
//     must NEVER silently skip the cross-org guard / membership grant. The nil
//     case only preserves old behavior for human assignees (where no agent
//     authorization is involved). Production always wires the directory.
//   - CROSS-ORG GUARDED: it resolves the agent's org via the directory and the
//     project's org via the project repo; a mismatch (or an unresolvable agent)
//     is rejected with pm.ErrCrossOrgAssignee — an org member is the prerequisite
//     for project membership.
//   - IDEMPOTENT: if the agent is already a member it is a no-op (ErrMemberExists
//     from the member repo is swallowed); no duplicate row, no error.
//
// It runs inside the AssignTask tx (after t.Assign succeeds), so the assignment
// and the membership commit atomically. Membership is monotonic (OQ13-style):
// Unassign/reassign never removes it — only explicit member management does.
func (s *Service) grantAgentProjectMembership(ctx context.Context, t *pm.Task, assignee pm.IdentityRef, now time.Time) error {
	if !strings.HasPrefix(string(assignee), "agent:") {
		return nil // human / system assignees: no agent authorization to grant
	}
	if s.agentDir == nil {
		// Fail-closed: an agent assignee requires the directory to verify its org
		// (OQ6). Refuse rather than silently bypass the cross-org guard.
		return pm.ErrAgentDirectoryUnavailable
	}
	agentID := strings.TrimPrefix(string(assignee), "agent:")

	// Cross-org guard: agent's org must equal the project's org.
	p, err := s.projects.FindByID(ctx, t.ProjectID())
	if err != nil {
		return err
	}
	agentOrg, err := s.agentDir.OrgOfAgent(ctx, agentID)
	if err != nil {
		// Can't verify org (e.g. agent not found) → treat as cross-org/reject.
		return pm.ErrCrossOrgAssignee
	}
	if agentOrg != p.OrganizationID() {
		return pm.ErrCrossOrgAssignee
	}

	// Idempotent add: reuse the same member-repo insert AddProjectMember uses.
	m, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(s.idgen.NewULID()), ProjectID: t.ProjectID(), IdentityID: assignee,
		Role: pm.RoleMember, AddedBy: assignee, CreatedAt: now,
	})
	if err != nil {
		return err
	}
	if err := s.members.Save(ctx, m); err != nil {
		if errors.Is(err, pm.ErrMemberExists) {
			return nil // already a member → no-op
		}
		return err
	}
	return nil
}

// UnassignTask moves assigned→open and clears the assignee (the explicit "drop
// the assignment" verb). Per OQ13 the outgoing assignee is RETAINED as a sticky
// manual subscriber (stays in the Conversation, downgraded to subscriber; only
// an explicit Unsubscribe removes them). The live AgentWorkItem is canceled by
// the WorkItemProjector consuming the resulting state_changed→open (canceling
// the work attempt is independent of keeping the subscription).
func (s *Service) UnassignTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject unassign on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // Unassign is metadata-only; status unchanged.
		prev := t.Assignee()
		if err := t.Unassign(now); err != nil {
			return err
		}
		if err := s.retainAsTaskSubscriber(txCtx, t, prev, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskStateChanged(txCtx, t, prevStatus, "")
	})
}

// ReopenTask moves a completed/verified Task back to open in one step (internally
// completed/verified→reopened→open), clearing assignment + completion truth so a
// subsequent assign starts a fresh work segment. Per OQ13 the prior assignee +
// completer are RETAINED as sticky manual subscribers (they did the work → stay
// informed after reopen). No live WorkItem to cancel (the Task was done).
func (s *Service) ReopenTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject reopen on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // before the reopen chain (completed/verified).
		prevAssignee, prevCompleter := t.Assignee(), t.CompletedBy()
		if err := t.Reopen(now); err != nil {
			return err
		}
		if err := t.ToOpenFromReopened(now); err != nil {
			return err
		}
		if err := s.retainAsTaskSubscriber(txCtx, t, prevAssignee, now); err != nil {
			return err
		}
		if err := s.retainAsTaskSubscriber(txCtx, t, prevCompleter, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskStateChanged(txCtx, t, prevStatus, "")
	})
}

// taskStateOp is the shared "load → gate → mutate → persist → emit
// state_changed" path for status-only transitions.
func (s *Service) taskStateOp(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef, mutate func(*pm.Task, time.Time) error, reason string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject any status transition (Start/Discard/Block/Complete/Verify/
		// SetStatus — every taskStateOp caller) on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // snapshot BEFORE the transition (Discard/SetStatus/...).
		if err := mutate(t, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskStateChanged(txCtx, t, prevStatus, reason)
	})
}

// SetTaskStatus sets the Task to any VALID status with NO adjacency enforcement
// (v2.8.1 @oopslink: task state = the agent's self-reported progress; the center
// does not gate workflow transitions — the Change-status menu offers the full
// enum). Project-member gated; emits pm.task.state_changed (generic) so the
// participant projector + downstream stay in sync. The typed transitions
// (Start/Complete/Block/...) remain for the agent's structured self-reports.
func (s *Service) SetTaskStatus(ctx context.Context, taskID pm.TaskID, target pm.TaskStatus, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error {
		return t.SetStatus(target, now)
	}, "")
}

// BatchTaskPatch is the set of optionally-updated fields for BatchUpdateTask. A
// nil pointer means "leave unchanged"; a non-nil pointer applies the field
// (v2.8.1 edit-task #278). For Assignee, "" means Unassign. Title/Description
// are also accepted so the bare task PATCH stays a superset of the prior
// metadata-only PATCH (single atomic tx for the whole edit).
type BatchTaskPatch struct {
	Status      *string
	Assignee    *string
	Tags        *[]string
	Title       *string
	Description *string
	// DerivedFromIssue (T192): nil = unchanged; "" = clear the link; a non-empty id
	// (re)links — validated to exist + same project, like UpdateTask.
	DerivedFromIssue *pm.IssueID
}

// BatchUpdateTask applies any subset of {status, assignee, tags} to a Task in a
// SINGLE tx — all-or-none (if any field's mutation errors, the tx rolls back and
// nothing is applied). Project-member gated. Emits pm.task.state_changed so the
// participant projector + downstream stay in sync (a tags-only edit still bumps
// version + re-emits, which is harmless/idempotent for the effective set).
func (s *Service) BatchUpdateTask(ctx context.Context, taskID pm.TaskID, patch BatchTaskPatch, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject batch task edit on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // snapshot BEFORE patch.Status applies (if any).
		if patch.Title != nil {
			if err := t.Rename(*patch.Title, now); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			if err := t.SetDescription(*patch.Description, now); err != nil {
				return err
			}
		}
		if patch.Status != nil {
			if err := t.SetStatus(pm.TaskStatus(*patch.Status), now); err != nil {
				return err
			}
		}
		if patch.Assignee != nil {
			if *patch.Assignee == "" {
				if err := t.Unassign(now); err != nil {
					return err
				}
			} else if err := t.Assign(pm.IdentityRef(*patch.Assignee), now); err != nil {
				return err
			}
		}
		if patch.Tags != nil {
			if err := t.SetTags(*patch.Tags, now); err != nil {
				return err
			}
		}
		if patch.DerivedFromIssue != nil {
			if err := s.applyDerivedFromIssue(txCtx, t, *patch.DerivedFromIssue, now); err != nil {
				return err
			}
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskStateChanged(txCtx, t, prevStatus, "")
	})
}

// emitTaskAssignEvent emits an assign/reassign event carrying the current
// assignee, previous assignee, and the recomputed effective subscriber set.
func (s *Service) emitTaskAssignEvent(ctx context.Context, t *pm.Task, evt, previous string) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, evt,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()),
			PreviousAssignee: previous, Status: string(t.Status()),
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}

// emitTaskStateChanged emits pm.task.state_changed carrying the recomputed
// effective subscriber set. A state change can move the effective set — most
// notably unassign/reopen, which clear the assignee so the prior assignee must
// leave the task Conversation. The ParticipantProjector consumes this and
// rewrites participants to the effective set (set semantics → idempotent for
// state changes that don't move the set, e.g. start/block/complete).
//
// prevStatus is the task's status captured by the caller BEFORE the transition
// this emit reports (the task is already transitioned by the time we run, so the
// caller MUST snapshot prev). It rides the payload as PrevStatus so the P2-2
// failure handler can notify ONLY on the →failed transition (#re-discard edge).
// For emits that follow no status transition (assign/reassign/unassign tags-only
// edits), the caller passes the current status — prev == now means "not a
// transition", which is never a failure transition anyway.
func (s *Service) emitTaskStateChanged(ctx context.Context, t *pm.Task, prevStatus pm.TaskStatus, reason string) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtTaskStateChanged,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()),
			Status: string(t.Status()), PrevStatus: string(prevStatus), Reason: reason,
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}
