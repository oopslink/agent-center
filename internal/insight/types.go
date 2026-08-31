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
	CompletedExecutions int64             `json:"completed_executions"`
	FailedExecutions    int64             `json:"failed_executions"`
	FailureRate         *float64          `json:"failure_rate"`
	SlotUtilization     *float64          `json:"slot_utilization"`
	SlotCoverageRatio   *float64          `json:"slot_coverage_ratio"`
	QueueWaitMS         PercentileSummary `json:"queue_wait_ms"`
	ExecutionDurationMS PercentileSummary `json:"execution_duration_ms"`
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
	Window      Window      `json:"window"`
	AsOf        string      `json:"as_of"`
	RefreshedAt string      `json:"refreshed_at"`
	Freshness   Freshness   `json:"freshness"`
	Summary     Summary     `json:"summary"`
	Agents      []LeaderRow `json:"agents"`
	Projects    []LeaderRow `json:"projects"`
	Diagnostics Diagnostics `json:"diagnostics"`
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

type V2EvolutionResponse struct {
	V2WindowedEnvelope
	ProjectID string         `json:"project_id"`
	Evolution map[string]any `json:"evolution"`
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
