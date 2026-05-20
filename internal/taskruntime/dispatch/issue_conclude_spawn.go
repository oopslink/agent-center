package dispatch

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// IssueConcludeSpawn is the Phase 2 stub for the Discussion BC → TaskRuntime
// cross-BC batch-task-spawn entry point (00-overview § 3.4). Phase 3
// wires the actual Discussion caller and emits the Discussion-side events.
//
// In Phase 2 this stub:
//   - Validates the spec
//   - Atomically creates N Tasks (with batch-internal dep refs resolved)
//   - Emits task.created events
//
// Phase 3 must extend this to: write IssueComment, change Issue.status,
// emit issue.concluded + issue.tasks_spawned. Those are explicitly OUT of
// Phase 2 scope.
type IssueConcludeSpawn struct {
	db       *sql.DB
	taskRepo task.Repository
	sink     *observability.EventSink
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewIssueConcludeSpawn constructs the stub.
func NewIssueConcludeSpawn(db *sql.DB, taskRepo task.Repository, sink *observability.EventSink, gen idgen.Generator, clk clock.Clock) *IssueConcludeSpawn {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &IssueConcludeSpawn{db: db, taskRepo: taskRepo, sink: sink, idgen: gen, clock: clk}
}

// Spawn creates N Tasks atomically per the spec. Returns the new Task ids
// in declaration order, or an error (any failure → integral rollback).
func (s *IssueConcludeSpawn) Spawn(ctx context.Context, spec IssueConcludeSpec) ([]taskruntime.TaskID, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	actor := observability.Actor(spec.ActorID)
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	ids := make([]taskruntime.TaskID, len(spec.Tasks))
	// Map local id → taskruntime.TaskID for dep resolution
	localToID := make(map[string]taskruntime.TaskID, len(spec.Tasks))
	for i, ts := range spec.Tasks {
		ids[i] = taskruntime.TaskID(s.idgen.NewULID())
		localToID[ts.LocalID] = ids[i]
	}
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		for i, ts := range spec.Tasks {
			deps := make([]taskruntime.TaskID, 0, len(ts.DependsOnLocalIDs)+len(ts.DependsOnTaskIDs))
			for _, l := range ts.DependsOnLocalIDs {
				deps = append(deps, localToID[l])
			}
			for _, d := range ts.DependsOnTaskIDs {
				// Verify existing tasks exist
				if _, err := s.taskRepo.FindByID(txCtx, d); err != nil {
					return err
				}
				deps = append(deps, d)
			}
			priority := ts.Priority
			if priority == "" {
				priority = task.PriorityMedium
			}
			t, err := task.New(task.NewInput{
				ID:               ids[i],
				ProjectID:        spec.ProjectID,
				ParentTaskID:     ts.ParentTaskID,
				FromIssueID:      spec.IssueID,
				Title:            ts.Title,
				Description:      ts.Description,
				Priority:         priority,
				EtaAt:            ts.EtaAt,
				RequiresWorktree: ts.RequiresWorktree,
				DependsOnTaskIDs: deps,
				CreatedBy:        spec.ActorID,
				Now:              now,
			})
			if err != nil {
				return err
			}
			if err := s.taskRepo.Save(txCtx, t); err != nil {
				return err
			}
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "task.created",
				Refs: observability.EventRefs{
					TaskID:    string(t.ID()),
					ProjectID: t.ProjectID(),
					IssueID:   spec.IssueID,
				},
				Actor: actor,
				Payload: map[string]any{
					"task_id":       string(t.ID()),
					"project_id":    t.ProjectID(),
					"from_issue_id": spec.IssueID,
					"title":         t.Title(),
				},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ErrNotImplementedInPhase2 indicates a method on this stub that's
// reserved for Phase 3.
var ErrNotImplementedInPhase2 = errors.New("taskruntime: not implemented in phase 2 (deferred to phase 3)")
