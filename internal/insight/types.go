package insight

import (
	"errors"
	"time"
)

const (
	Window24h             = "24h"
	DefaultFreshnessSLA   = 2 * time.Minute
	DefaultProjectorTick  = 30 * time.Second
	SchemaVersion         = 2
	MetricVersionV2       = "insight.metrics.v2"
	SourceActivity        = "agent_activity_events"
	SourceQueue           = "worker_control_events"
	SourceSlotObservation = "agent_concurrency_observations"
)

var ErrExecutionNotFound = errors.New("insight: execution not found")

type Freshness struct {
	State       string `json:"state"`
	AgeMS       int64  `json:"age_ms"`
	ThresholdMS int64  `json:"threshold_ms"`
}

type Window struct {
	Kind     string `json:"kind"`
	Duration string `json:"duration"`
	Start    string `json:"start"`
	End      string `json:"end"`
}

type PercentileSummary struct {
	P50     *int64 `json:"p50"`
	P95     *int64 `json:"p95"`
	Samples int64  `json:"samples"`
}

type Summary struct {
	CompletedExecutions         int64             `json:"completed_executions"`
	FailedExecutions            int64             `json:"failed_executions"`
	RecoveryFinalizedExecutions int64             `json:"recovery_finalized_executions"`
	FailureRate                 *float64          `json:"failure_rate"`
	SlotUtilization             *float64          `json:"slot_utilization"`
	SlotCoverageRatio           *float64          `json:"slot_coverage_ratio"`
	QueueWaitMS                 PercentileSummary `json:"queue_wait_ms"`
	ExecutionDurationMS         PercentileSummary `json:"execution_duration_ms"`
}

type TrendPoint struct {
	BucketStart                 string `json:"bucket_start"`
	CompletedExecutions         int64  `json:"completed_executions"`
	FailedExecutions            int64  `json:"failed_executions"`
	RecoveryFinalizedExecutions int64  `json:"recovery_finalized_executions"`
	AvgDurationMS               *int64 `json:"avg_duration_ms"`
}

type UsageTrendPoint struct {
	BucketStart      string `json:"bucket_start"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CostMicros       int64  `json:"cost_micros"`
}

type UsageModelSummary struct {
	Model       string `json:"model"`
	Events      int64  `json:"events"`
	TotalTokens int64  `json:"total_tokens"`
	CostMicros  int64  `json:"cost_micros"`
}

type UsageSummary struct {
	InputTokens      int64               `json:"input_tokens"`
	OutputTokens     int64               `json:"output_tokens"`
	CacheReadTokens  int64               `json:"cache_read_tokens"`
	CacheWriteTokens int64               `json:"cache_write_tokens"`
	TotalTokens      int64               `json:"total_tokens"`
	CostMicros       int64               `json:"cost_micros"`
	Events           int64               `json:"events"`
	Trend            []UsageTrendPoint   `json:"trend"`
	ByModel          []UsageModelSummary `json:"by_model"`
}

type PlanScaleSummary struct {
	PlanID           string  `json:"plan_id"`
	PlanName         string  `json:"plan_name"`
	ProjectID        string  `json:"project_id"`
	ProjectName      *string `json:"project_name,omitempty"`
	Status           string  `json:"status"`
	TaskCount        int64   `json:"task_count"`
	EdgeCount        int64   `json:"edge_count"`
	GenerationCount  int64   `json:"generation_count"`
	EvolutionCount   int64   `json:"evolution_count"`
	ActiveTaskCount  int64   `json:"active_task_count"`
	BlockedTaskCount int64   `json:"blocked_task_count"`
	FailedTaskCount  int64   `json:"failed_task_count"`
	DoneTaskCount    int64   `json:"done_task_count"`
}

type LeaderRow struct {
	AgentRef    string  `json:"agent_ref,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	ProjectID   string  `json:"project_id,omitempty"`
	Name        *string `json:"name,omitempty"`
	Summary     Summary `json:"summary"`
}

type Diagnostics struct {
	InvalidFacts int64 `json:"invalid_facts"`
	LateEvents   int64 `json:"late_events"`
}

type Overview struct {
	Window      Window             `json:"window"`
	AsOf        string             `json:"as_of"`
	RefreshedAt string             `json:"refreshed_at"`
	Freshness   Freshness          `json:"freshness"`
	Summary     Summary            `json:"summary"`
	Trend       []TrendPoint       `json:"trend"`
	Usage       UsageSummary       `json:"usage"`
	PlanScale   []PlanScaleSummary `json:"plan_scale"`
	Agents      []LeaderRow        `json:"agents"`
	Projects    []LeaderRow        `json:"projects"`
	Diagnostics Diagnostics        `json:"diagnostics"`
}

type ExecutionRow struct {
	ExecutionID    string  `json:"execution_id"`
	CommandID      *string `json:"command_id"`
	CommandStatus  *string `json:"command_status"`
	StatusReason   *string `json:"status_reason"`
	StatusMessage  *string `json:"status_message"`
	TaskID         *string `json:"task_id"`
	TaskRef        *string `json:"task_ref"`
	TaskTitle      *string `json:"task_title"`
	AgentRef       string  `json:"agent_ref"`
	AgentName      *string `json:"agent_name"`
	ProjectID      *string `json:"project_id"`
	ProjectName    *string `json:"project_name"`
	WorkerID       *string `json:"worker_id"`
	Outcome        *string `json:"outcome"`
	FailureReason  *string `json:"failure_reason"`
	FailureMessage *string `json:"failure_message"`
	QueuedAt       *string `json:"queued_at"`
	StartedAt      *string `json:"started_at"`
	FinishedAt     *string `json:"finished_at"`
	QueueWaitMS    *int64  `json:"queue_wait_ms"`
	DurationMS     *int64  `json:"duration_ms"`
	Recovered      bool    `json:"recovered"`
	Quality        string  `json:"quality"`
}

type ExecutionsResponse struct {
	Window      Window         `json:"window"`
	AsOf        string         `json:"as_of"`
	RefreshedAt string         `json:"refreshed_at"`
	Freshness   Freshness      `json:"freshness"`
	Executions  []ExecutionRow `json:"executions"`
	NextCursor  string         `json:"next_cursor,omitempty"`
}

type ExecutionResponse struct {
	Window      Window       `json:"window"`
	AsOf        string       `json:"as_of"`
	RefreshedAt string       `json:"refreshed_at"`
	Freshness   Freshness    `json:"freshness"`
	Execution   ExecutionRow `json:"execution"`
}

type ExecutionFilter struct {
	AgentRef  string
	ProjectID string
	Cursor    string
	Limit     int
	AsOf      time.Time
}

type V2Meta struct {
	MetricVersion string    `json:"metric_version"`
	SampleCount   int64     `json:"sample_count"`
	Coverage      *float64  `json:"coverage"`
	Freshness     Freshness `json:"freshness"`
	UnknownCount  int64     `json:"unknown_count"`
	Known         bool      `json:"known"`
}

type V2Health struct {
	Status      string           `json:"status"`
	ReasonCodes []string         `json:"reason_codes"`
	Evidence    []map[string]any `json:"evidence"`
}

type V2CountMetric struct {
	Value *int64 `json:"value"`
	Meta  V2Meta `json:"meta"`
}

type V2WindowedEnvelope struct {
	MetricVersion string   `json:"metric_version"`
	TimeWindow    Window   `json:"time_window"`
	AsOf          string   `json:"as_of"`
	Health        V2Health `json:"health"`
	Meta          V2Meta   `json:"meta"`
}

type V2EntitySummary struct {
	ID             string        `json:"id"`
	Name           *string       `json:"name,omitempty"`
	Health         V2Health      `json:"health"`
	ExecutionCount V2CountMetric `json:"execution_count"`
	FailureRate    *float64      `json:"failure_rate"`
	OpenIssues     V2CountMetric `json:"open_issues"`
	BlockedTasks   V2CountMetric `json:"blocked_tasks"`
	ActivePlans    V2CountMetric `json:"active_plans"`
	ReasonCodes    []string      `json:"reason_codes"`
}

type V2OverviewResponse struct {
	V2WindowedEnvelope
	Executions V2CountMetric     `json:"executions"`
	Agents     []V2EntitySummary `json:"agents"`
	Projects   []V2EntitySummary `json:"projects"`
}

type V2FunnelBreak struct {
	Kind      string         `json:"kind"`
	Count     V2CountMetric  `json:"count"`
	Drilldown map[string]any `json:"drilldown"`
}

type V2Funnel struct {
	Issues V2CountMetric   `json:"issues"`
	Tasks  V2CountMetric   `json:"tasks"`
	Plans  V2CountMetric   `json:"plans"`
	Done   V2CountMetric   `json:"done"`
	Breaks []V2FunnelBreak `json:"breaks"`
}

type V2ProjectDeliveryResponse struct {
	V2WindowedEnvelope
	ProjectID string   `json:"project_id"`
	Funnel    V2Funnel `json:"funnel"`
}

type V2EvolutionAnomalyDrilldowns struct {
	Rework    map[string]any `json:"rework"`
	Recovery  map[string]any `json:"recovery"`
	LoopDepth map[string]any `json:"loop_depth"`
	Residue   map[string]any `json:"residue"`
}

type V2EvolutionSummary struct {
	Plans                 int64                        `json:"plans"`
	EvolvedPlans          int64                        `json:"evolved_plans"`
	EvolutionRate         *float64                     `json:"evolution_rate"`
	GenerationCount       int64                        `json:"generation_count"`
	ReworkCount           int64                        `json:"rework_count"`
	ReworkRatio           *float64                     `json:"rework_ratio"`
	RecoveryAttempts      int64                        `json:"recovery_attempts"`
	RecoverySuccesses     int64                        `json:"recovery_successes"`
	RecoveryEffectiveness *float64                     `json:"recovery_effectiveness"`
	MaxLoopDepth          int64                        `json:"max_loop_depth"`
	StaleOrphanResidue    int64                        `json:"stale_orphan_residue"`
	AnomalyDrilldowns     V2EvolutionAnomalyDrilldowns `json:"anomaly_drilldowns"`
}

type V2EvolutionResponse struct {
	V2WindowedEnvelope
	ProjectID string             `json:"project_id"`
	Evolution V2EvolutionSummary `json:"evolution"`
}

type V2Generation struct {
	Generation         int              `json:"generation"`
	CreatedAt          string           `json:"created_at"`
	TriggeredBy        string           `json:"triggered_by"`
	Reason             string           `json:"reason"`
	Evidence           []map[string]any `json:"evidence"`
	NodeChanges        []map[string]any `json:"node_changes"`
	RecoveryDurationMS *int64           `json:"recovery_duration_ms"`
	RecoveryOutcome    string           `json:"recovery_outcome"`
	DeliveryBranch     string           `json:"delivery_branch,omitempty"`
	DeliverySHA        string           `json:"delivery_sha,omitempty"`
	AcceptanceVerdict  string           `json:"acceptance_verdict,omitempty"`
}

type V2PlanLineageResponse struct {
	V2WindowedEnvelope
	ProjectID   string         `json:"project_id"`
	PlanID      string         `json:"plan_id"`
	Generations []V2Generation `json:"generations"`
}
