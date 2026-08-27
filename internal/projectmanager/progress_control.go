package projectmanager

import (
	"context"
	"time"
)

type ProgressDecision string

const (
	ProgressFactVerified     ProgressDecision = "progress_fact_verified"
	ResponsibilityBound      ProgressDecision = "responsibility_bound"
	CannotDetermine          ProgressDecision = "cannot_determine"
	ProgressDecisionVerified                  = ProgressFactVerified
	ProgressDecisionBound                     = ResponsibilityBound
	ProgressDecisionCannot                    = CannotDetermine
	ProgressEscalationRaised                  = "EscalationRequested"
	ProgressWakeRequested                     = "WakeRequested"
	ProgressWakeDelivered                     = "WakeDelivered"
	ProgressWakeAcknowledged                  = "WakeAcknowledged"
	ProgressDecisionRecorded                  = "DecisionRecorded"
)

type ProgressQuality string

const (
	ProgressQualityValid   ProgressQuality = "valid"
	ProgressQualitySuspect ProgressQuality = "suspect"
)

type ProgressFactQuality string

const (
	ProgressFactQualityValid   ProgressFactQuality = "valid"
	ProgressFactQualitySuspect ProgressFactQuality = "suspect"
	ProgressFactQualityUnknown ProgressFactQuality = "unknown"
)

type ObservationSource struct {
	Kind        string    `json:"kind"`
	SourceID    string    `json:"source_id,omitempty"`
	Revision    string    `json:"revision"`
	WatermarkAt time.Time `json:"watermark_at"`
	ObservedAt  time.Time `json:"observed_at"`
}

type ProgressFact struct {
	ID           string              `json:"id"`
	SourceKind   string              `json:"source_kind"`
	SourceID     string              `json:"source_id"`
	OccurredAt   time.Time           `json:"occurred_at"`
	ObservedAt   time.Time           `json:"observed_at"`
	Revision     string              `json:"revision"`
	Summary      string              `json:"summary"`
	Quality      ProgressFactQuality `json:"quality"`
	CannotAbsent bool                `json:"cannot_absent,omitempty"`
}

type ProgressCoverage struct {
	TotalNodes                     int     `json:"total_nodes"`
	CoveredNodes                   int     `json:"covered_nodes"`
	CoverageRatio                  float64 `json:"coverage_ratio"`
	UncoveredProgressWindowSeconds int64   `json:"uncovered_progress_window_seconds"`
}

type ObservationVector struct {
	ID                             string
	PlanID                         PlanID
	TaskID                         TaskID
	NodeID                         string
	Decision                       ProgressDecision
	Quality                        ProgressQuality
	AsOf                           time.Time
	EvaluatedAt                    time.Time
	SourceRevisions                []ObservationSource
	Facts                          []ProgressFact
	SuspectKey                     string
	SuspectCycles                  int
	ProgressContract               DeliveryContract
	ProgressContractDefaulted      bool
	UncoveredProgressWindowSeconds int64
	Coverage                       ProgressCoverage
}

type ProgressObligationKind string

const (
	ObligationProduceDelivery ProgressObligationKind = "produce_delivery"
	ObligationSourceRecovery  ProgressObligationKind = "source_recovery"
	ProgressObligationAckWake                        = "ack_wake"
)

type ProgressIncidentKind string

const (
	IncidentWatermarkLag                  ProgressIncidentKind = "watermark_lag"
	IncidentProgressClassificationUnknown ProgressIncidentKind = "progress_classification_unknown"
	IncidentMigrationGap                  ProgressIncidentKind = "migration_gap"
	IncidentProjectorUnavailable          ProgressIncidentKind = "projector_unavailable"
	ProgressIncidentWakeAckLost                                = "wake_ack_lost"
	ProgressIncidentOperational                                = "operational_incident"
	ProgressIncidentHoldSLOBreach                              = "hold_slo_breached"
)

type ResponsibilityStatus string

const (
	ResponsibilityOpen     ResponsibilityStatus = "open"
	ResponsibilityResolved ResponsibilityStatus = "resolved"
)

type ProgressObligation struct {
	ID                   string
	PlanID               PlanID
	TaskID               TaskID
	NodeID               string
	Kind                 ProgressObligationKind
	OwnerRef             IdentityRef
	OwnerDisplay         string
	DeadlineAt           time.Time
	AckRequired          bool
	AckedAt              *time.Time
	EscalateToRef        IdentityRef
	EscalationDeadlineAt time.Time
	SourceFactRefs       []string
	EpisodeKey           string
	Status               ResponsibilityStatus
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

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

type ProgressIncident struct {
	ID                   string
	PlanID               PlanID
	TaskID               TaskID
	NodeID               string
	Kind                 ProgressIncidentKind
	OwnerRef             IdentityRef
	OwnerDisplay         string
	DeadlineAt           time.Time
	AckRequired          bool
	AckedAt              *time.Time
	EscalateToRef        IdentityRef
	EscalationDeadlineAt time.Time
	SourceFactRefs       []string
	EpisodeKey           string
	Status               ResponsibilityStatus
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
	Severity             string
	Summary              string
	SourceRef            string
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
	RecordWake(context.Context, ProgressWake) (bool, error)
	MarkWakeDelivered(context.Context, string, time.Time) error
	AcknowledgeWake(context.Context, string, IdentityRef, time.Time, string) (bool, error)
	ListExpiredUnackedWakes(context.Context, time.Time, int) ([]ProgressWake, error)
	UpsertObligation(context.Context, ProgressObligation) (bool, error)
	UpsertIncident(context.Context, ProgressIncident) (bool, error)
	UpsertHold(context.Context, ProgressHold) (bool, error)
	ListOpenHoldsByPlan(context.Context, PlanID) ([]ProgressHold, error)
	ListOpenHoldsByTask(context.Context, TaskID) ([]ProgressHold, error)
	ListDueHolds(context.Context, time.Time, int) ([]ProgressHold, error)
	ListBreachedHolds(context.Context, time.Time, int) ([]ProgressHold, error)
	ReleaseHoldsByFact(context.Context, PlanID, TaskID, IdentityRef, string, time.Time) (int, error)
	ReleaseHoldsByReason(context.Context, string, string, IdentityRef, string, time.Time) (int, error)
	ResolveOpenObligationsByFact(context.Context, PlanID, TaskID, IdentityRef, string, time.Time) (int, error)
	ResolveOpenIncidentsBySource(context.Context, PlanID, TaskID, string, string, time.Time) (int, error)
	RecordEscalation(context.Context, ProgressEscalation) (bool, error)
	SnapshotPlan(context.Context, PlanID, time.Time) (ProgressControlSnapshot, error)
}
