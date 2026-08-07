package environment

import (
	"context"
	"time"
)

type CapabilityWaitStatus string

const (
	CapabilityWaitWaiting  CapabilityWaitStatus = "waiting_for_capability"
	CapabilityWaitResolved CapabilityWaitStatus = "resolved"
	CapabilityWaitCanceled CapabilityWaitStatus = "canceled"
	CapabilityWaitTimedOut CapabilityWaitStatus = "timed_out"
)

type CapabilityWait struct {
	TaskID        string
	AgentID       string
	AssigneeRef   string
	WorkerID      string
	RequiredCLI   string
	Reason        string
	Status        CapabilityWaitStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     time.Time
	RedriveCount  int
	LastRedriveAt time.Time
}

type CapabilityWaitRepository interface {
	UpsertWaiting(ctx context.Context, wait CapabilityWait) error
	Resolve(ctx context.Context, taskID, agentID string, at time.Time) error
	CancelByTask(ctx context.Context, taskID, reason string, at time.Time) error
	MarkTimedOut(ctx context.Context, taskID, agentID, reason string, at time.Time) error
	RecordRedrive(ctx context.Context, taskID, agentID string, at time.Time) error
	ListWaitingByWorker(ctx context.Context, workerID string) ([]CapabilityWait, error)
	ListWaiting(ctx context.Context) ([]CapabilityWait, error)
	ListExpiredWaiting(ctx context.Context, at time.Time, limit int) ([]CapabilityWait, error)
}
