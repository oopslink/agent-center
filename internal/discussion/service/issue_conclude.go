package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// IssueConcludeSpawner is the port the lifecycle service uses to invoke
// the TaskRuntime BC's batch spawn primitive. The contract MUST honor
// tx-via-ctx (the cross-BC writes share the Discussion BC tx per
// ADR-0014 § 2).
type IssueConcludeSpawner interface {
	Spawn(ctx context.Context, spec dispatch.IssueConcludeSpec) ([]taskruntime.TaskID, error)
}

// WithSpawnerAndCommenter injects the cross-BC IssueConcludeSpawn port +
// the conversation message adder used to write the conclusion system
// message. Returns the receiver for fluent setup.
func (s *IssueLifecycleService) WithSpawnerAndCommenter(spawner IssueConcludeSpawner, msgAdder ConversationMessageAdder) *IssueLifecycleService {
	s.spawner = spawner
	s.msgAdder = msgAdder
	return s
}

// ConcludeIssueCommand drives IssueLifecycleService.Conclude.
type ConcludeIssueCommand struct {
	IssueID     discussion.IssueID
	Resolution  discussion.Resolution
	ConcludedBy string
	Actor       observability.Actor
}

// ConcludeIssueResult wraps the produced ids.
type ConcludeIssueResult struct {
	IssueID  discussion.IssueID
	TaskIDs  []taskruntime.TaskID
	EventIDs []observability.EventID
}

// Conclude executes the conclude flow:
//
//   - load issue + validate status ∈ {open, under_discussion, concluded}
//   - branch on resolution kind:
//     · closed_no_action: UpdateStatus → emit issue.concluded
//     · closed_with_tasks: Spawn N tasks (cross-BC) → write system Message →
//       UpdateStatus + UpdateConclusion → emit issue.concluded + issue.tasks_spawned
//     · withdrawn: delegate to Withdraw helper (uses resolution.Summary as
//       reason; the conclude-via-withdrawn shape is rare in v1 but the
//       state machine allows it for amended flows)
//
// All work is done in one tx; any error rolls back the entire path
// (including task.created emits) — per ADR-0014 § 2 + § 17.
func (s *IssueLifecycleService) Conclude(ctx context.Context, cmd ConcludeIssueCommand) (*ConcludeIssueResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(cmd.IssueID)) == "" {
		return nil, errors.New("issue conclude: issue_id required")
	}
	if err := cmd.Resolution.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cmd.ConcludedBy) == "" {
		return nil, errors.New("issue conclude: concluded_by required")
	}
	if cmd.Resolution.Kind == discussion.ResolutionClosedWithTasks && s.spawner == nil {
		return nil, errors.New("issue conclude: closed_with_tasks requires IssueConcludeSpawner (nil)")
	}
	res := &ConcludeIssueResult{IssueID: cmd.IssueID}

	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		issue, err := s.issueRepo.FindByID(txCtx, cmd.IssueID)
		if err != nil {
			return err
		}
		if issue.Status() == discussion.StatusWithdrawn {
			return discussion.ErrIssueWithdrawn
		}
		if issue.IsTerminal() {
			return discussion.ErrIssueAlreadyConcluded
		}
		// Spawn first (if applicable). Failing here ⇒ tx rollback ⇒ no
		// task rows + no issue state change + no events committed.
		var taskIDs []taskruntime.TaskID
		if cmd.Resolution.Kind == discussion.ResolutionClosedWithTasks {
			spec := dispatch.IssueConcludeSpec{
				IssueID:        string(issue.ID()),
				ProjectID:      issue.ProjectID(),
				Resolution:     string(cmd.Resolution.Kind),
				ClosingComment: cmd.Resolution.Summary,
				Tasks:          cmd.Resolution.Tasks,
				ActorID:        cmd.Actor.String(),
			}
			ids, err := s.spawner.Spawn(txCtx, spec)
			if err != nil {
				return fmt.Errorf("issue conclude: spawn: %w", err)
			}
			taskIDs = ids
			res.TaskIDs = ids
			// If conversation is bound, write a system Message announcing
			// the spawn. Caller may omit the conversation (lazy path) in
			// which case we silently skip the announce — that's an explicit
			// design decision per discussion/00 § 1.1, NOT a § 17 silence.
			if issue.HasConversation() && s.msgAdder != nil {
				announce := buildSpawnSystemMessage(ids)
				if _, err := s.msgAdder.AddMessage(txCtx, convservice.AddMessageCommand{
					ConversationID:   issue.ConversationID(),
					SenderIdentityID: conversation.IdentityRef("system"),
					ContentKind:      conversation.MessageContentSystem,
					Content:          announce,
					Direction:        conversation.DirectionInternal,
					Actor:            cmd.Actor,
				}); err != nil {
					return fmt.Errorf("issue conclude: write system message: %w", err)
				}
			}
		}
		// Withdrawn-via-conclude path delegates to UpdateWithdraw so the
		// reason/message fields populate even though resolution.Summary
		// drives both reason + message (we treat summary as message text;
		// reason defaults to "concluded_withdrawn"). conventions § 16
		// requires both fields non-empty.
		if cmd.Resolution.Kind == discussion.ResolutionWithdrawn {
			now := s.clock.Now()
			if err := s.issueRepo.UpdateWithdraw(txCtx, issue.ID(),
				"concluded_withdrawn", cmd.Resolution.Summary,
				cmd.ConcludedBy, now, issue.Version()); err != nil {
				return err
			}
			id, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "issue.withdrawn",
				Refs: observability.EventRefs{
					IssueID:        string(issue.ID()),
					ProjectID:      issue.ProjectID(),
					ConversationID: string(issue.ConversationID()),
				},
				Actor: cmd.Actor,
				Payload: map[string]any{
					"issue_id":     string(issue.ID()),
					"reason":       "concluded_withdrawn",
					"message":      cmd.Resolution.Summary,
					"withdrawn_by": cmd.ConcludedBy,
				},
			})
			if err != nil {
				return err
			}
			res.EventIDs = append(res.EventIDs, id)
			return nil
		}
		// Closed_* (non-withdrawn) path: AR state mutation → CAS UPDATE.
		target := cmd.Resolution.Kind.TargetStatus()
		now := s.clock.Now()
		prevStatus := issue.Status()
		prevVersion := issue.Version()
		if err := issue.Conclude(cmd.Resolution, cmd.ConcludedBy, now); err != nil {
			return err
		}
		if err := s.issueRepo.UpdateStatus(txCtx, issue.ID(), prevStatus, target, prevVersion, now); err != nil {
			return err
		}
		// Persist conclusion fields atomically (separate UPDATE since
		// UpdateStatus only writes status). version was bumped by
		// UpdateStatus, so we pass the bumped version.
		if err := s.issueRepo.UpdateConclusion(txCtx, issue.ID(),
			cmd.Resolution.Summary, cmd.ConcludedBy, now, prevVersion+1); err != nil {
			return err
		}
		issueEventID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "issue.concluded",
			Refs: observability.EventRefs{
				IssueID:        string(issue.ID()),
				ProjectID:      issue.ProjectID(),
				ConversationID: string(issue.ConversationID()),
			},
			Actor: cmd.Actor,
			Payload: map[string]any{
				"issue_id":        string(issue.ID()),
				"resolution_kind": string(cmd.Resolution.Kind),
				"summary":         cmd.Resolution.Summary,
				"concluded_by":    cmd.ConcludedBy,
			},
		})
		if err != nil {
			return err
		}
		res.EventIDs = append(res.EventIDs, issueEventID)

		if cmd.Resolution.Kind == discussion.ResolutionClosedWithTasks {
			taskIDStrs := make([]string, len(taskIDs))
			for i, id := range taskIDs {
				taskIDStrs[i] = string(id)
			}
			spawnEventID, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "issue.tasks_spawned",
				Refs: observability.EventRefs{
					IssueID:   string(issue.ID()),
					ProjectID: issue.ProjectID(),
				},
				Actor: cmd.Actor,
				Payload: map[string]any{
					"issue_id": string(issue.ID()),
					"count":    len(taskIDs),
					"task_ids": taskIDStrs,
				},
			})
			if err != nil {
				return err
			}
			res.EventIDs = append(res.EventIDs, spawnEventID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func buildSpawnSystemMessage(ids []taskruntime.TaskID) string {
	if len(ids) == 0 {
		return "已结论（无新任务）"
	}
	asStr := make([]string, len(ids))
	for i, id := range ids {
		asStr[i] = string(id)
	}
	return fmt.Sprintf("已 spawn %d task: %s", len(asStr), strings.Join(asStr, " / "))
}
