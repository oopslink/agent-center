package projectmanager

import "time"

type ProgressDecision string

const (
	ProgressFactVerified ProgressDecision = "progress_fact_verified"
	ResponsibilityBound  ProgressDecision = "responsibility_bound"
	CannotDetermine      ProgressDecision = "cannot_determine"
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
)

type ProgressIncidentKind string

const (
	IncidentWatermarkLag                  ProgressIncidentKind = "watermark_lag"
	IncidentProgressClassificationUnknown ProgressIncidentKind = "progress_classification_unknown"
	IncidentMigrationGap                  ProgressIncidentKind = "migration_gap"
	IncidentProjectorUnavailable          ProgressIncidentKind = "projector_unavailable"
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
}
