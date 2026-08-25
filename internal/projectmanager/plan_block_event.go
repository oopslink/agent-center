package projectmanager

import (
	"errors"
	"strings"
	"time"
)

type PlanAttentionStatus string

const (
	PlanAttentionNone      PlanAttentionStatus = "none"
	PlanAttentionRequired  PlanAttentionStatus = "attention_required"
	PlanAttentionEscalated PlanAttentionStatus = "escalated"
)

func (s PlanAttentionStatus) IsValid() bool {
	switch s {
	case PlanAttentionNone, PlanAttentionRequired, PlanAttentionEscalated:
		return true
	}
	return false
}

type PlanRecoveryPolicy struct {
	NotifyAfterSeconds   int
	RemindAfterSeconds   int
	EscalateAfterSeconds int
}

func DefaultPlanRecoveryPolicy() PlanRecoveryPolicy {
	return PlanRecoveryPolicy{NotifyAfterSeconds: 0, RemindAfterSeconds: 15 * 60, EscalateAfterSeconds: 60 * 60}
}

type PlanBlockNotificationState string

const (
	PlanBlockNotifyPending PlanBlockNotificationState = "pending"
	PlanBlockNotifySent    PlanBlockNotificationState = "sent"
	PlanBlockNotifyFailed  PlanBlockNotificationState = "failed"
)

type PlanBlockEvent struct {
	EventID                PlanBlockEventID
	IdempotencyKey         string
	PlanID                 PlanID
	GenerationID           PlanGenerationID
	TaskID                 TaskID
	NodeID                 string
	ExecutionID            string
	BlockVersion           int
	BlockedReason          string
	ReasonType             BlockReasonType
	BlockedBy              IdentityRef
	BlockedAt              time.Time
	Active                 bool
	Effective              bool
	ImpactedDownstreamJSON string
	OwnerRef               IdentityRef
	NextActionsJSON        string
	AcknowledgedAt         time.Time
	AcknowledgedBy         IdentityRef
	ResolvedAt             time.Time
	ResolvedBy             IdentityRef
	ResolutionKind         string
	ResolutionNote         string
	NotificationState      PlanBlockNotificationState
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func NewPlanBlockEvent(in PlanBlockEvent) (*PlanBlockEvent, error) {
	if strings.TrimSpace(string(in.EventID)) == "" {
		return nil, errors.New("projectmanager: plan block event id required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" || in.PlanID == "" || in.GenerationID == "" || in.TaskID == "" {
		return nil, errors.New("projectmanager: invalid plan block event identity")
	}
	if in.BlockVersion < 1 {
		in.BlockVersion = 1
	}
	if strings.TrimSpace(in.BlockedReason) == "" {
		return nil, ErrBlockReasonRequired
	}
	if !in.ReasonType.IsValid() {
		return nil, ErrInvalidBlockReasonType
	}
	if err := in.OwnerRef.Validate(); err != nil {
		return nil, err
	}
	if in.ImpactedDownstreamJSON == "" {
		in.ImpactedDownstreamJSON = "[]"
	}
	if in.NextActionsJSON == "" {
		in.NextActionsJSON = `["acknowledge","resolve","evolve_plan_generation","pause_or_discard_plan"]`
	}
	if in.NotificationState == "" {
		in.NotificationState = PlanBlockNotifyPending
	}
	if in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() || in.BlockedAt.IsZero() {
		return nil, errors.New("projectmanager: plan block event timestamps required")
	}
	in.BlockedAt = in.BlockedAt.UTC()
	in.CreatedAt = in.CreatedAt.UTC()
	in.UpdatedAt = in.UpdatedAt.UTC()
	return &in, nil
}
