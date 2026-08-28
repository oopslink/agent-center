package insight

import (
	"errors"
	"time"
)

const (
	Window24h             = "24h"
	DefaultFreshnessSLA   = 2 * time.Minute
	SchemaVersion         = 2
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
	Semantics           SummarySemantics  `json:"semantics"`
}

type MetricEnvelope struct {
	Value       any        `json:"value"`
	Status      string     `json:"status"`
	Coverage    *float64   `json:"coverage,omitempty"`
	Freshness   *Freshness `json:"freshness,omitempty"`
	Window      *Window    `json:"window,omitempty"`
	SampleCount *int64     `json:"sample_count,omitempty"`
}

type SummarySemantics struct {
	CompletedExecutions  MetricEnvelope `json:"completed_executions"`
	FailedExecutions     MetricEnvelope `json:"failed_executions"`
	FailureRate          MetricEnvelope `json:"failure_rate"`
	SlotUtilization      MetricEnvelope `json:"slot_utilization"`
	SlotCoverageRatio    MetricEnvelope `json:"slot_coverage_ratio"`
	QueueWaitP50MS       MetricEnvelope `json:"queue_wait_p50_ms"`
	QueueWaitP95MS       MetricEnvelope `json:"queue_wait_p95_ms"`
	ExecutionDurationP50 MetricEnvelope `json:"execution_duration_p50_ms"`
	ExecutionDurationP95 MetricEnvelope `json:"execution_duration_p95_ms"`
}

type ExecutionStatusSemantic struct {
	State             string `json:"state"`
	Label             string `json:"label"`
	Severity          string `json:"severity"`
	CountsAsFailure   bool   `json:"counts_as_failure"`
	Recovered         bool   `json:"recovered"`
	AuditOutcome      string `json:"audit_outcome,omitempty"`
	AuditCommandState string `json:"audit_command_status,omitempty"`
}

type ExecutionQualitySemantic struct {
	State        string `json:"state"`
	Label        string `json:"label"`
	Severity     string `json:"severity"`
	AuditQuality string `json:"audit_quality,omitempty"`
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
	ExecutionID     string                   `json:"execution_id"`
	CommandID       *string                  `json:"command_id"`
	TaskID          *string                  `json:"task_id"`
	TaskRef         *string                  `json:"task_ref"`
	TaskTitle       *string                  `json:"task_title"`
	AgentRef        string                   `json:"agent_ref"`
	AgentName       *string                  `json:"agent_name"`
	ProjectID       *string                  `json:"project_id"`
	ProjectName     *string                  `json:"project_name"`
	WorkerID        *string                  `json:"worker_id"`
	Outcome         *string                  `json:"outcome"`
	FailureReason   *string                  `json:"failure_reason"`
	FailureMessage  *string                  `json:"failure_message"`
	CommandStatus   *string                  `json:"command_status"`
	StatusReason    *string                  `json:"status_reason"`
	StatusMessage   *string                  `json:"status_message"`
	QueuedAt        *string                  `json:"queued_at"`
	StartedAt       *string                  `json:"started_at"`
	FinishedAt      *string                  `json:"finished_at"`
	QueueWaitMS     *int64                   `json:"queue_wait_ms"`
	DurationMS      *int64                   `json:"duration_ms"`
	Recovered       bool                     `json:"recovered"`
	Quality         string                   `json:"quality"`
	StatusSemantic  ExecutionStatusSemantic  `json:"status"`
	QualitySemantic ExecutionQualitySemantic `json:"quality_semantic"`
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

type ErrorResponse struct {
	Error       string    `json:"error"`
	Code        string    `json:"code"`
	Window      Window    `json:"window"`
	AsOf        string    `json:"as_of"`
	RefreshedAt string    `json:"refreshed_at"`
	Freshness   Freshness `json:"freshness"`
}
