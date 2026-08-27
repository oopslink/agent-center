package projectmanager

import "time"

type ProgressFreshnessState string

const (
	ProgressFreshnessFresh    ProgressFreshnessState = "fresh"
	ProgressFreshnessStale    ProgressFreshnessState = "stale"
	ProgressFreshnessDegraded ProgressFreshnessState = "degraded"
)

type ProgressDecision string

const (
	ProgressDecisionVerified        ProgressDecision = "progress_fact_verified"
	ProgressDecisionResponsibility  ProgressDecision = "responsibility_bound"
	ProgressDecisionCannotDetermine ProgressDecision = "cannot_determine"
)

type ProgressQuality string

const (
	ProgressQualityValid   ProgressQuality = "valid"
	ProgressQualitySuspect ProgressQuality = "suspect"
)

type ProgressHealth string

const (
	ProgressHealthHealthy   ProgressHealth = "healthy"
	ProgressHealthAttention ProgressHealth = "attention"
	ProgressHealthDegraded  ProgressHealth = "degraded"
)

type ProgressAttentionKind string

const (
	ProgressAttentionProgress   ProgressAttentionKind = "progress"
	ProgressAttentionObligation ProgressAttentionKind = "obligation"
	ProgressAttentionIncident   ProgressAttentionKind = "incident"
	ProgressAttentionHold       ProgressAttentionKind = "hold"
	ProgressAttentionAcceptance ProgressAttentionKind = "acceptance"
	ProgressAttentionAck        ProgressAttentionKind = "ack"
)

type ProgressControl struct {
	AsOf                time.Time
	Health              ProgressHealth
	Freshness           ProgressFreshness
	Decision            ProgressDecision
	ObservationVectorID string
	Quality             ProgressQuality
	OpenObligations     []ProgressObligation
	OpenIncidents       []ProgressIncident
	OpenHolds           []ProgressHold
	RequiredActions     []ProgressRequiredAction
	PrimaryAttention    *ProgressRequiredAction
	ValidInFlight       []ProgressInFlight
	Coverage            ProgressCoverage
}

type ProgressFreshness struct {
	State        ProgressFreshnessState
	WatermarkLag time.Duration
	Threshold    time.Duration
	Source       string
}

type ProgressObligation struct {
	ID                   string
	PlanID               PlanID
	TaskID               TaskID
	Kind                 string
	OwnerRef             IdentityRef
	OwnerDisplay         string
	DeadlineAt           time.Time
	AckRequired          bool
	AckedAt              time.Time
	EscalateToRef        IdentityRef
	EscalationDeadlineAt time.Time
	SourceFactRefs       []string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ProgressIncident struct {
	ID                   string
	PlanID               PlanID
	TaskID               TaskID
	Kind                 string
	OwnerRef             IdentityRef
	OwnerDisplay         string
	DeadlineAt           time.Time
	AckRequired          bool
	AckedAt              time.Time
	EscalateToRef        IdentityRef
	EscalationDeadlineAt time.Time
	SourceFactRefs       []string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ProgressHold struct {
	ID                               string
	PlanID                           PlanID
	TaskID                           TaskID
	ReasonKind                       string
	ReasonID                         string
	BlocksNewDispatch                bool
	BlocksGatePassToken              bool
	BlocksDestructiveDownstreamStart bool
	InFlightPolicy                   string
	HoldAckDeadline                  time.Time
	MaxHoldDuration                  time.Duration
	StartedAt                        time.Time
	DeadlineAt                       time.Time
	ReleasedAt                       time.Time
	ReleaseFactRef                   string
	AckedAt                          time.Time
	Age                              time.Duration
	DeadlineRemaining                time.Duration
}

type ProgressRequiredAction struct {
	ID           string
	Kind         ProgressAttentionKind
	Action       string
	SubjectID    string
	NodeID       string
	OwnerRef     IdentityRef
	DeadlineAt   time.Time
	AckRequired  bool
	AckedAt      time.Time
	HoldID       string
	IncidentID   string
	ObligationID string
	Severity     string
	Source       string
	Summary      string
}

type ProgressInFlight struct {
	TaskID      TaskID
	NodeID      string
	Status      NodeStatus
	AssigneeRef IdentityRef
	StartedAt   time.Time
	Quality     ProgressQuality
	Source      string
}

type ProgressCoverage struct {
	TotalNodes            int
	ClassifiedNodes       int
	VerifiedProgressNodes int
	ResponsibilityNodes   int
	CannotDetermineNodes  int
	SuspectNodes          int
	ValidInFlightNodes    int
	OpenObligations       int
	OpenIncidents         int
	OpenHolds             int
	BlockedOnRowsObserved int
	MissingDeadlineHolds  int
}
