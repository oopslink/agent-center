package projectmanager

import (
	"context"
	"time"
)

type ProgressDecision string

const (
	ProgressDecisionVerified      ProgressDecision = "progress_fact_verified"
	ProgressDecisionBound         ProgressDecision = "responsibility_bound"
	ProgressDecisionCannot        ProgressDecision = "cannot_determine"
	ProgressObligationAckWake     string           = "ack_wake"
	ProgressIncidentWakeAckLost   string           = "wake_ack_lost"
	ProgressIncidentOperational   string           = "operational_incident"
	ProgressIncidentHoldSLOBreach string           = "hold_slo_breached"
	ProgressEscalationRaised      string           = "EscalationRequested"
	ProgressWakeRequested         string           = "WakeRequested"
	ProgressWakeDelivered         string           = "WakeDelivered"
	ProgressWakeAcknowledged      string           = "WakeAcknowledged"
	ProgressDecisionRecorded      string           = "DecisionRecorded"
)

type ProgressWake struct {
	ID                   string
	PlanID               PlanID
	TaskID               TaskID
	NodeID               string
	OwnerRef             IdentityRef
	OwnerDisplay         string
	Reason               string
	Status               string
	IdempotencyKey       string
	RequestedAt          time.Time
	DeliveredAt          time.Time
	AcknowledgedAt       time.Time
	AckFactRef           string
	AckDeadline          time.Time
	MaxHoldDuration      time.Duration
	EscalationLevel      int
	NextEscalationAt     time.Time
	OrganizationOwnerRef string
}

type ProgressObligation struct {
	ID                   string
	PlanID               PlanID
	TaskID               TaskID
	NodeID               string
	Kind                 string
	OwnerRef             IdentityRef
	OwnerDisplay         string
	DeadlineAt           time.Time
	AckRequired          bool
	AckedAt              time.Time
	EscalateToRef        string
	EscalationDeadlineAt time.Time
	SourceFactRefs       []string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

type ProgressIncident struct {
	ID           string
	PlanID       PlanID
	TaskID       TaskID
	NodeID       string
	Kind         string
	Severity     string
	OwnerRef     string
	OwnerDisplay string
	Summary      string
	SourceRef    string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ProgressHold struct {
	ID               string
	PlanID           PlanID
	TaskID           TaskID
	NodeID           string
	ReasonKind       string
	ReasonID         string
	OwnerRef         string
	OwnerDisplay     string
	EnteredAt        time.Time
	HoldAckDeadline  time.Time
	MaxHoldDuration  time.Duration
	EscalationLevel  int
	NextEscalationAt time.Time
	BlocksDispatch   bool
	BlocksAcceptance bool
	BlocksCompletion bool
	ReleasedAt       time.Time
	ReleaseFactRef   string
}

type ProgressEscalation struct {
	ID            string
	PlanID        PlanID
	TaskID        TaskID
	NodeID        string
	ObligationID  string
	HoldID        string
	Kind          string
	Severity      string
	EscalateToRef string
	DeadlineAt    time.Time
	CreatedAt     time.Time
}

type ProgressControlSnapshot struct {
	AsOf            time.Time
	Decision        ProgressDecision
	OpenObligations []ProgressObligation
	OpenIncidents   []ProgressIncident
	OpenHolds       []ProgressHold
}

type ProgressControlRepository interface {
	RecordWake(ctx context.Context, w ProgressWake) (created bool, err error)
	MarkWakeDelivered(ctx context.Context, wakeID string, at time.Time) error
	AcknowledgeWake(ctx context.Context, wakeID string, actor IdentityRef, at time.Time, factRef string) (bool, error)
	ListExpiredUnackedWakes(ctx context.Context, now time.Time, limit int) ([]ProgressWake, error)
	UpsertObligation(ctx context.Context, o ProgressObligation) (created bool, err error)
	UpsertIncident(ctx context.Context, i ProgressIncident) (created bool, err error)
	UpsertHold(ctx context.Context, h ProgressHold) (created bool, err error)
	ListOpenHoldsByPlan(ctx context.Context, planID PlanID) ([]ProgressHold, error)
	ListOpenHoldsByTask(ctx context.Context, taskID TaskID) ([]ProgressHold, error)
	ListDueHolds(ctx context.Context, now time.Time, limit int) ([]ProgressHold, error)
	ListBreachedHolds(ctx context.Context, now time.Time, limit int) ([]ProgressHold, error)
	ReleaseHoldsByFact(ctx context.Context, planID PlanID, taskID TaskID, actor IdentityRef, factRef string, at time.Time) (int, error)
	ReleaseHoldsByReason(ctx context.Context, reasonKind, reasonID string, actor IdentityRef, factRef string, at time.Time) (int, error)
	ResolveOpenObligationsByFact(ctx context.Context, planID PlanID, taskID TaskID, actor IdentityRef, factRef string, at time.Time) (int, error)
	ResolveOpenIncidentsBySource(ctx context.Context, planID PlanID, taskID TaskID, sourceRef string, factRef string, at time.Time) (int, error)
	RecordEscalation(ctx context.Context, e ProgressEscalation) (created bool, err error)
	SnapshotPlan(ctx context.Context, planID PlanID, asOf time.Time) (ProgressControlSnapshot, error)
}
