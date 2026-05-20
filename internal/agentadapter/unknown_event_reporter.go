package agentadapter

import (
	"context"
	"sync"

	"github.com/oopslink/agent-center/internal/observability"
)

// UnknownEventReporter dedupes EventUnknown events per
// (adapter_name, cli_type) and emits observability events. It also
// tracks per-execution counts so the worker daemon can escalate after a
// configurable threshold (05-agent-adapters § 3.1).
type UnknownEventReporter struct {
	mu sync.Mutex
	// keyed by execution_id → set of (adapter_name|cli_type) seen
	seen map[string]map[string]struct{}
	// per-execution unknown count
	count map[string]int
	// emit threshold for warning event
	threshold int
	// already-warned executions
	warned    map[string]struct{}
	parseFail map[string]int
	failCap   int
}

// ReporterConfig captures threshold knobs.
type ReporterConfig struct {
	WarningThresholdPerExecution int
	ParseFailFailureThreshold    int
}

// DefaultReporterConfig returns v1 defaults (05-agent-adapters § 3.1 / §
// 3.2).
func DefaultReporterConfig() ReporterConfig {
	return ReporterConfig{
		WarningThresholdPerExecution: 5,
		ParseFailFailureThreshold:    5,
	}
}

// NewUnknownEventReporter constructs a reporter.
func NewUnknownEventReporter(cfg ReporterConfig) *UnknownEventReporter {
	if cfg.WarningThresholdPerExecution == 0 {
		cfg.WarningThresholdPerExecution = 5
	}
	if cfg.ParseFailFailureThreshold == 0 {
		cfg.ParseFailFailureThreshold = 5
	}
	return &UnknownEventReporter{
		seen:      map[string]map[string]struct{}{},
		count:     map[string]int{},
		warned:    map[string]struct{}{},
		parseFail: map[string]int{},
		threshold: cfg.WarningThresholdPerExecution,
		failCap:   cfg.ParseFailFailureThreshold,
	}
}

// Report records an EventUnknown observation for a given execution. Emits
// `agent_adapter.unknown_event_seen` exactly once per (adapter, cli_type)
// per execution; raises `task_execution.warning(adapter_significantly_
// behind)` when count crosses threshold.
func (r *UnknownEventReporter) Report(ctx context.Context, sink *observability.EventSink, adapterName, executionID string, ev AgentTraceEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := adapterName + "|" + ev.CliType
	seen := r.seen[executionID]
	if seen == nil {
		seen = map[string]struct{}{}
		r.seen[executionID] = seen
	}
	r.count[executionID]++
	if _, ok := seen[key]; !ok {
		seen[key] = struct{}{}
		_, err := sink.Emit(ctx, observability.EmitCommand{
			EventType: "agent_adapter.unknown_event_seen",
			Refs: observability.EventRefs{
				ExecutionID: executionID,
			},
			Actor: "worker:agentadapter",
			Payload: map[string]any{
				"execution_id": executionID,
				"adapter_name": adapterName,
				"cli_type":     ev.CliType,
				"sample_raw":   string(ev.Raw),
				"reason":       "unknown_event_seen",
				"message":      "agent CLI emitted an unrecognised event type",
			},
		})
		if err != nil {
			return err
		}
	}
	if r.count[executionID] >= r.threshold {
		if _, ok := r.warned[executionID]; !ok {
			r.warned[executionID] = struct{}{}
			_, err := sink.Emit(ctx, observability.EmitCommand{
				EventType: "task_execution.warning",
				Refs:      observability.EventRefs{ExecutionID: executionID},
				Actor:     "worker:agentadapter",
				Payload: map[string]any{
					"execution_id": executionID,
					"reason":       "adapter_significantly_behind",
					"message":      "agent CLI emitted many unrecognised events",
					"count":        r.count[executionID],
				},
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ReportParseFailure records a JSONL parse error. Returns true when the
// caller should mark the execution failed (jsonl_parse_error).
func (r *UnknownEventReporter) ReportParseFailure(executionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parseFail[executionID]++
	return r.parseFail[executionID] >= r.failCap
}

// Reset clears per-execution counters (test-only).
func (r *UnknownEventReporter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = map[string]map[string]struct{}{}
	r.count = map[string]int{}
	r.warned = map[string]struct{}{}
	r.parseFail = map[string]int{}
}
