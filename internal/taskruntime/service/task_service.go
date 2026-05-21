// Package service hosts cross-aggregate application-layer services for
// TaskRuntime (e.g. TaskService.Create which writes task + conversation
// in the same tx per ADR-0017).
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// ProjectExistenceChecker is the minimal port the TaskService uses to
// validate that a referenced project exists before creating a Task. The
// schema no longer declares FOREIGN KEY (conventions § 9.w), so referential
// integrity for task.project_id is enforced here at the application layer.
//
// The implementation is normally a thin wrapper around
// workforce.ProjectRepository.FindByID; passing nil disables the check
// (used by some narrow unit tests that bypass the full wiring).
type ProjectExistenceChecker interface {
	ProjectExists(ctx context.Context, projectID string) (bool, error)
}

// ErrProjectNotFound is returned by TaskService.Create when the requested
// project does not exist (app-layer enforcement per conventions § 9.w).
var ErrProjectNotFound = errors.New("task service: project not found")

// TaskService is the application-layer wrapper for Task CRUD operations.
type TaskService struct {
	db           *sql.DB
	taskRepo     task.Repository
	convRepo     conversation.ConversationRepository
	execRepo     execution.Repository
	msgRepo      conversation.MessageRepository
	projectCheck ProjectExistenceChecker
	sink         *observability.EventSink
	idgen        idgen.Generator
	clock        clock.Clock
}

// NewTaskService constructs the service.
//
// projectCheck may be nil; when nil, app-layer project existence checks
// are skipped. Production wiring (cli/app.go) passes a concrete checker.
func NewTaskService(
	db *sql.DB,
	taskRepo task.Repository,
	convRepo conversation.ConversationRepository,
	execRepo execution.Repository,
	msgRepo conversation.MessageRepository,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *TaskService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &TaskService{
		db: db, taskRepo: taskRepo, convRepo: convRepo, execRepo: execRepo,
		msgRepo: msgRepo, sink: sink, idgen: gen, clock: clk,
	}
}

// WithProjectExistenceChecker wires an app-layer project existence check
// into the service. Returns the receiver for fluent setup.
func (s *TaskService) WithProjectExistenceChecker(c ProjectExistenceChecker) *TaskService {
	s.projectCheck = c
	return s
}

// TaskCreateInput captures `task create` parameters.
type TaskCreateInput struct {
	ProjectID            string
	Title                string
	Description          string
	ParentTaskID         taskruntime.TaskID
	FromIssueID          string
	Priority             task.Priority
	RequiresWorktree     bool
	DependsOnTaskIDs     []taskruntime.TaskID
	WithConversation     bool   // true = a/e path (sync-create)
	ConversationTitle    string
	Actor                observability.Actor
}

// TaskCreateResult wraps the created ids.
type TaskCreateResult struct {
	TaskID         taskruntime.TaskID
	ConversationID conversation.ConversationID
}

// Create writes a Task + optionally a Conversation (kind=task) atomically
// (ADR-0017 a/e path).
func (s *TaskService) Create(ctx context.Context, in TaskCreateInput) (*TaskCreateResult, error) {
	if err := in.Actor.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ProjectID) == "" {
		return nil, errors.New("task service: project_id required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("task service: title required")
	}
	// App-layer referential integrity for task.project_id (conventions § 9.w):
	// schema no longer declares FOREIGN KEY, so the existence check moves
	// into the application service. Done before the tx to keep the tx short.
	if s.projectCheck != nil {
		ok, err := s.projectCheck.ProjectExists(ctx, in.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("task service: project existence check: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrProjectNotFound, in.ProjectID)
		}
	}
	now := s.clock.Now()
	res := &TaskCreateResult{
		TaskID: taskruntime.TaskID(s.idgen.NewULID()),
	}
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		var convID conversation.ConversationID
		if in.WithConversation {
			conv, err := conversation.NewConversation(conversation.NewConversationInput{
				ID:       conversation.ConversationID(s.idgen.NewULID()),
				Kind:     conversation.ConversationKindTask,
				Title:    in.ConversationTitle,
				OpenedAt: now,
			})
			if err != nil {
				return err
			}
			if err := s.convRepo.Save(txCtx, conv); err != nil {
				return err
			}
			convID = conv.ID()
			res.ConversationID = convID
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "conversation.opened",
				Refs:      observability.EventRefs{ConversationID: string(convID)},
				Actor:     in.Actor,
				Payload: map[string]any{
					"conversation_id": string(convID),
					"kind":            string(conv.Kind()),
				},
			}); err != nil {
				return err
			}
		}
		t, err := task.New(task.NewInput{
			ID:               res.TaskID,
			ProjectID:        in.ProjectID,
			ParentTaskID:     in.ParentTaskID,
			FromIssueID:      in.FromIssueID,
			Title:            in.Title,
			Description:      in.Description,
			Priority:         priorityOrDefault(in.Priority),
			RequiresWorktree: in.RequiresWorktree,
			DependsOnTaskIDs: in.DependsOnTaskIDs,
			ConversationID:   string(convID),
			CreatedBy:        in.Actor.String(),
			Now:              now,
		})
		if err != nil {
			return err
		}
		if err := s.taskRepo.Save(txCtx, t); err != nil {
			return err
		}
		_, err = s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task.created",
			Refs: observability.EventRefs{
				TaskID:         string(t.ID()),
				ProjectID:      t.ProjectID(),
				ConversationID: string(convID),
				IssueID:        in.FromIssueID,
			},
			Actor: in.Actor,
			Payload: map[string]any{
				"task_id":         string(t.ID()),
				"project_id":      t.ProjectID(),
				"title":           t.Title(),
				"conversation_id": string(convID),
				"from_issue_id":   in.FromIssueID,
			},
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// BindConversationInput parameters for `task bind-conversation`.
type BindConversationInput struct {
	TaskID            taskruntime.TaskID
	Mode              string // "auto" (new conversation) | "to" (existing)
	ExistingConvID    conversation.ConversationID
	Title             string
	ChannelHint       string
	Actor             observability.Actor
}

// BindConversation wires a task to a Conversation (b/c/d lazy path).
func (s *TaskService) BindConversation(ctx context.Context, in BindConversationInput) (conversation.ConversationID, error) {
	if err := in.Actor.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(string(in.TaskID)) == "" {
		return "", errors.New("task service: task_id required")
	}
	now := s.clock.Now()
	var convID conversation.ConversationID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		t, err := s.taskRepo.FindByID(txCtx, in.TaskID)
		if err != nil {
			return err
		}
		switch in.Mode {
		case "auto":
			conv, err := conversation.NewConversation(conversation.NewConversationInput{
				ID:                 conversation.ConversationID(s.idgen.NewULID()),
				Kind:               conversation.ConversationKindTask,
				Title:              in.Title,
				PrimaryChannelHint: in.ChannelHint,
				OpenedAt:           now,
			})
			if err != nil {
				return err
			}
			if err := s.convRepo.Save(txCtx, conv); err != nil {
				return err
			}
			convID = conv.ID()
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "conversation.opened",
				Refs:      observability.EventRefs{ConversationID: string(convID)},
				Actor:     in.Actor,
				Payload: map[string]any{
					"conversation_id": string(convID),
					"kind":            "task",
				},
			}); err != nil {
				return err
			}
		case "to":
			if strings.TrimSpace(string(in.ExistingConvID)) == "" {
				return errors.New("task service: --to requires conversation_id")
			}
			existing, err := s.convRepo.FindByID(txCtx, in.ExistingConvID)
			if err != nil {
				return err
			}
			if !existing.IsOpen() {
				return conversation.ErrConversationClosed
			}
			convID = existing.ID()
		default:
			return fmt.Errorf("task service: invalid bind mode %q (use auto or to)", in.Mode)
		}
		if err := t.BindConversation(string(convID), now); err != nil {
			return err
		}
		if err := s.taskRepo.Update(txCtx, t); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return convID, nil
}

// TaskContext is the read-side payload returned by ReadContext.
type TaskContext struct {
	TaskID             taskruntime.TaskID          `json:"task_id"`
	ProjectID          string                       `json:"project_id"`
	Status             task.Status                  `json:"status"`
	Title              string                       `json:"title"`
	Description        string                       `json:"description"`
	Priority           task.Priority                `json:"priority"`
	RequiresWorktree   bool                         `json:"requires_worktree"`
	ParentTaskID       taskruntime.TaskID           `json:"parent_task_id,omitempty"`
	FromIssueID        string                       `json:"from_issue_id,omitempty"`
	ConversationID     string                       `json:"conversation_id,omitempty"`
	CurrentExecutionID taskruntime.TaskExecutionID  `json:"current_execution_id,omitempty"`
	DependsOnTaskIDs   []taskruntime.TaskID         `json:"depends_on_task_ids,omitempty"`
	CreatedBy          string                       `json:"created_by"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
	RecentMessages     []ConversationMessage        `json:"recent_messages,omitempty"`
	Executions         []ExecutionSummary           `json:"executions,omitempty"`
}

// ConversationMessage is a flattened conversation message representation
// for the read-side context.
type ConversationMessage struct {
	ID              conversation.MessageID          `json:"id"`
	ContentKind     conversation.MessageContentKind `json:"content_kind"`
	Content         string                          `json:"content"`
	SenderIdentity  string                          `json:"sender_identity"`
	InputRequestRef string                          `json:"input_request_ref,omitempty"`
	PostedAt        time.Time                       `json:"posted_at"`
}

// ExecutionSummary describes one execution attached to the task.
type ExecutionSummary struct {
	ID       taskruntime.TaskExecutionID `json:"id"`
	Status   execution.Status            `json:"status"`
	WorkerID string                      `json:"worker_id"`
	AgentCLI string                      `json:"agent_cli"`
}

// ReadContext returns the read-side context for `read-task-context`.
func (s *TaskService) ReadContext(ctx context.Context, taskID taskruntime.TaskID, recentMessagesN int) (*TaskContext, error) {
	t, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := &TaskContext{
		TaskID:             t.ID(),
		ProjectID:          t.ProjectID(),
		Status:             t.Status(),
		Title:              t.Title(),
		Description:        t.Description(),
		Priority:           t.Priority(),
		RequiresWorktree:   t.RequiresWorktree(),
		ParentTaskID:       t.ParentTaskID(),
		FromIssueID:        t.FromIssueID(),
		ConversationID:     t.ConversationID(),
		CurrentExecutionID: t.CurrentExecutionID(),
		DependsOnTaskIDs:   t.DependsOnTaskIDs(),
		CreatedBy:          t.CreatedBy(),
		CreatedAt:          t.CreatedAt(),
		UpdatedAt:          t.UpdatedAt(),
	}
	if recentMessagesN > 0 && t.ConversationID() != "" {
		msgs, err := s.msgRepo.FindRecent(ctx, conversation.ConversationID(t.ConversationID()), recentMessagesN)
		if err == nil {
			for _, m := range msgs {
				out.RecentMessages = append(out.RecentMessages, ConversationMessage{
					ID:              m.ID(),
					ContentKind:     m.ContentKind(),
					Content:         m.Content(),
					SenderIdentity:  string(m.SenderIdentityID()),
					InputRequestRef: m.InputRequestRef(),
					PostedAt:        m.PostedAt(),
				})
			}
		}
	}
	execs, err := s.execRepo.FindByTaskID(ctx, t.ID())
	if err == nil {
		for _, e := range execs {
			out.Executions = append(out.Executions, ExecutionSummary{
				ID:       e.ID(),
				Status:   e.Status(),
				WorkerID: e.WorkerID(),
				AgentCLI: e.AgentCLI(),
			})
		}
	}
	return out, nil
}

func priorityOrDefault(p task.Priority) task.Priority {
	if p == "" {
		return task.PriorityMedium
	}
	return p
}
