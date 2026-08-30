package insight

// MetricVersionV2 is the immutable semantic version of the Insight posture,
// delivery, and evolution contract. It is intentionally independent from the
// DuckDB storage schema version.
const MetricVersionV2 = "insight.metrics.v2"

const (
	DefaultMinimumSamples      int64 = 5
	DefaultMinimumCoverage           = 0.90
	DefaultFailureRateElevated       = 0.10
	DefaultFailureRateDegraded       = 0.25
	DefaultQueueP95ElevatedMS  int64 = 30_000
	DefaultQueueP95DegradedMS  int64 = 120_000
)

type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthElevated HealthStatus = "elevated"
	HealthDegraded HealthStatus = "degraded"
	HealthUnknown  HealthStatus = "unknown"
)

type ReasonCode string

const (
	ReasonDataNoSamples           ReasonCode = "data.no_samples"
	ReasonDataLowCoverage         ReasonCode = "data.low_coverage"
	ReasonDataStale               ReasonCode = "data.stale"
	ReasonDataUnknownState        ReasonCode = "data.unknown_state"
	ReasonDeliveryFailureElevated ReasonCode = "delivery.failure_rate_elevated"
	ReasonDeliveryFailureDegraded ReasonCode = "delivery.failure_rate_degraded"
	ReasonQueueElevated           ReasonCode = "capacity.queue_p95_elevated"
	ReasonQueueDegraded           ReasonCode = "capacity.queue_p95_degraded"
	ReasonTaskBlocked             ReasonCode = "delivery.task_blocked"
	ReasonPlanAtRisk              ReasonCode = "delivery.plan_at_risk"
	ReasonEvolutionLoop           ReasonCode = "evolution.loop_detected"
	ReasonEvolutionRecoveryFailed ReasonCode = "evolution.recovery_failed"
	ReasonEvolutionResidue        ReasonCode = "evolution.stale_orphan_residue"
	ReasonLineageBroken           ReasonCode = "lineage.integrity_broken"
	ReasonFunnelBroken            ReasonCode = "delivery.funnel_broken"
	ReasonContainerProjection     ReasonCode = "task.container_projection_conflict"
)

// MetricMeta accompanies every aggregate. A numeric zero is meaningful only
// when Known is true; unavailable or insufficient observations use Known=false
// and Value=null in the containing metric.
type MetricMeta struct {
	MetricVersion string    `json:"metric_version"`
	SampleCount   int64     `json:"sample_count"`
	Coverage      *float64  `json:"coverage"`
	Freshness     Freshness `json:"freshness"`
	UnknownCount  int64     `json:"unknown_count"`
	Known         bool      `json:"known"`
}

type MetricValue struct {
	Value *float64   `json:"value"`
	Unit  string     `json:"unit"`
	Meta  MetricMeta `json:"meta"`
}

type CountMetric struct {
	Value *int64     `json:"value"`
	Meta  MetricMeta `json:"meta"`
}

type HealthDecision struct {
	Status      HealthStatus      `json:"status"`
	ReasonCodes []ReasonCode      `json:"reason_codes"`
	Evidence    []DrilldownFilter `json:"evidence"`
}

type DrilldownFilter struct {
	Kind      string `json:"kind"`
	AgentRef  string `json:"agent_ref,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
	IssueID   string `json:"issue_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Anomaly   string `json:"anomaly,omitempty"`
	Window    string `json:"window"`
}

type AggregateEnvelope struct {
	MetricVersion string         `json:"metric_version"`
	Window        Window         `json:"time_window"`
	AsOf          string         `json:"as_of"`
	Health        HealthDecision `json:"health"`
	Meta          MetricMeta     `json:"meta"`
}

type DeliveryMetrics struct {
	OpenIssues    CountMetric `json:"open_issues"`
	BlockedTasks  CountMetric `json:"blocked_tasks"`
	ActivePlans   CountMetric `json:"active_plans"`
	PlansAtRisk   CountMetric `json:"plans_at_risk"`
	FailureRate   MetricValue `json:"failure_rate"`
	QueueP95MS    MetricValue `json:"queue_p95_ms"`
	DurationP95MS MetricValue `json:"duration_p95_ms"`
}

type FunnelBreakKind string

const (
	FunnelIssueWithoutTask              FunnelBreakKind = "issue_without_task"
	FunnelTaskWithoutPlan               FunnelBreakKind = "task_without_plan"
	FunnelTaskMultipleContainers        FunnelBreakKind = "task_multiple_containers"
	FunnelDonePlanNonTerminalTask       FunnelBreakKind = "done_plan_non_terminal_task"
	FunnelDonePlanOpenIssue             FunnelBreakKind = "done_plan_open_issue"
	FunnelEvolutionOldGenerationResidue FunnelBreakKind = "evolution_old_generation_residue"
	FunnelDeliverySHALineageMismatch    FunnelBreakKind = "delivery_sha_lineage_mismatch"
)

type FunnelBreakdown struct {
	Kind      FunnelBreakKind `json:"kind"`
	Count     CountMetric     `json:"count"`
	Drilldown DrilldownFilter `json:"drilldown"`
}

type DeliveryFunnel struct {
	Issues CountMetric       `json:"issues"`
	Tasks  CountMetric       `json:"tasks"`
	Plans  CountMetric       `json:"plans"`
	Done   CountMetric       `json:"done"`
	Breaks []FunnelBreakdown `json:"breaks"`
}

type EvolutionReason string

const (
	EvolutionBlocked           EvolutionReason = "blocked"
	EvolutionReviewReject      EvolutionReason = "review_reject"
	EvolutionRequirementChange EvolutionReason = "requirement_change"
	EvolutionExecutionFailure  EvolutionReason = "execution_failure"
	EvolutionManualAdjustment  EvolutionReason = "manual_adjustment"
	EvolutionReasonUnknown     EvolutionReason = "unknown"
)

type ScopeChange struct {
	Added     CountMetric `json:"added"`
	Replaced  CountMetric `json:"replaced"`
	Retained  CountMetric `json:"retained"`
	Discarded CountMetric `json:"discarded"`
}

type EvolutionMetrics struct {
	EvolutionRate          MetricValue                     `json:"evolution_rate"`
	EvolutionCount         CountMetric                     `json:"evolution_count"`
	GenerationCount        CountMetric                     `json:"generation_count"`
	Reasons                map[EvolutionReason]CountMetric `json:"reasons"`
	TriggerStages          map[string]CountMetric          `json:"trigger_stages"`
	ScopeChange            ScopeChange                     `json:"scope_change"`
	ReworkRatio            MetricValue                     `json:"rework_ratio"`
	RecoveryStartedRate    MetricValue                     `json:"recovery_started_rate"`
	RecoveryCompletedRate  MetricValue                     `json:"recovery_completed_rate"`
	RecoveryFailedRate     MetricValue                     `json:"recovery_failed_rate"`
	TimeToNewGenerationMS  MetricValue                     `json:"time_to_new_generation_ms"`
	TimeToStableProgressMS MetricValue                     `json:"time_to_stable_progress_ms"`
	LoopDepth              CountMetric                     `json:"loop_depth"`
	OutcomeByGeneration    map[string]CountMetric          `json:"outcome_by_generation"`
	StaleOrphanResidue     CountMetric                     `json:"stale_orphan_residue"`
	LineageIntegrityRate   MetricValue                     `json:"lineage_integrity_rate"`
}

type GenerationNodeChange struct {
	TaskID         string `json:"task_id"`
	Change         string `json:"change"`
	ReplacesTaskID string `json:"replaces_task_id,omitempty"`
}

type GenerationLineage struct {
	Generation         int                    `json:"generation"`
	CreatedAt          string                 `json:"created_at"`
	TriggeredBy        string                 `json:"triggered_by"`
	Reason             EvolutionReason        `json:"reason"`
	Evidence           []DrilldownFilter      `json:"evidence"`
	NodeChanges        []GenerationNodeChange `json:"node_changes"`
	RecoveryDurationMS *int64                 `json:"recovery_duration_ms"`
	RecoveryOutcome    string                 `json:"recovery_outcome"`
	DeliveryBranch     string                 `json:"delivery_branch,omitempty"`
	DeliverySHA        string                 `json:"delivery_sha,omitempty"`
	AcceptanceVerdict  string                 `json:"acceptance_verdict,omitempty"`
}

type ProjectDeliveryResponse struct {
	AggregateEnvelope
	ProjectID string           `json:"project_id"`
	Delivery  DeliveryMetrics  `json:"delivery"`
	Funnel    DeliveryFunnel   `json:"funnel"`
	Evolution EvolutionMetrics `json:"evolution"`
}

type PlanLineageResponse struct {
	AggregateEnvelope
	ProjectID   string              `json:"project_id"`
	PlanID      string              `json:"plan_id"`
	Generations []GenerationLineage `json:"generations"`
}

// Ratio returns nil for an empty denominator. Unknown inputs must be removed
// from both numerator and denominator by the caller and recorded in metadata.
func Ratio(numerator, denominator int64) *float64 {
	if denominator <= 0 || numerator < 0 || numerator > denominator {
		return nil
	}
	v := float64(numerator) / float64(denominator)
	return &v
}
