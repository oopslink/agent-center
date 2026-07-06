package taskexec

import (
	"errors"
	"time"
)

// TaskExecutionStatus is the agent-local execution status (design §6.3).
type TaskExecutionStatus string

const (
	StatusPending TaskExecutionStatus = "pending"
	StatusRunning TaskExecutionStatus = "running"
	StatusPaused  TaskExecutionStatus = "paused"
	StatusFailed  TaskExecutionStatus = "failed"
	StatusDone    TaskExecutionStatus = "done"
)

func (s TaskExecutionStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusRunning, StatusPaused, StatusFailed, StatusDone:
		return true
	}
	return false
}

func (s TaskExecutionStatus) IsTerminal() bool {
	return s == StatusFailed || s == StatusDone
}

// TaskExecutionMeta is persisted as tasks/{task_id}/task.json (design §6.3).
type TaskExecutionMeta struct {
	TaskID    string              `json:"task_id"`
	Status    TaskExecutionStatus `json:"status"`
	PlanID    string              `json:"plan_id,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

func (m *TaskExecutionMeta) Validate() error {
	if m.TaskID == "" {
		return errors.New("taskexec: task_id required")
	}
	if !m.Status.IsValid() {
		return errors.New("taskexec: invalid status")
	}
	return nil
}

// ExecutionContext is persisted as tasks/{task_id}/execution.json (design §6.3).
type ExecutionContext struct {
	SessionID  string            `json:"session_id,omitempty"`
	RetryCount int               `json:"retry_count"`
	LLMModel   string            `json:"llm_model,omitempty"`
	LLMConfig  map[string]string `json:"llm_config,omitempty"`
}
