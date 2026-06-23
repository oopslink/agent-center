package usage

import (
	"fmt"
	"strings"
	"time"
)

// Source records how a usage event reached the center, for report-vs-transcript
// reconciliation/dedup (the transcript path is a later F2 sub-task; F1 only
// reserves the column).
type Source string

const (
	// SourceReport is the live report_usage MCP / worker-hook path (F2).
	SourceReport Source = "report"
	// SourceTranscript is the offline transcript-reclaim reconciliation fallback.
	SourceTranscript Source = "transcript"
)

// Valid reports whether s is a recognized source.
func (s Source) Valid() bool { return s == SourceReport || s == SourceTranscript }

// UsageEvent is one raw per-turn usage record — the drill-down detail behind the
// dashboard. CostMicros is materialized (computed from the model_prices row in
// force at TS via PriceBook.CostMicrosAt) before persistence. TaskID is optional:
// "" means the turn was not task-scoped (stored NULL).
type UsageEvent struct {
	ID         string
	AgentRef   string
	ProjectID  string
	TaskID     string // "" = not task-scoped
	Model      string
	Tokens     TokenCounts
	CostMicros int64
	TS         time.Time
	Source     Source
}

// Validate checks the invariants a row must satisfy before persistence. It does
// NOT verify cost against the price book — materialization is the caller's job;
// this guards shape only.
func (e UsageEvent) Validate() error {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return fmt.Errorf("usage: empty event id")
	case strings.TrimSpace(e.AgentRef) == "":
		return fmt.Errorf("usage: empty agent_ref")
	case strings.TrimSpace(e.ProjectID) == "":
		return fmt.Errorf("usage: empty project_id")
	case strings.TrimSpace(e.Model) == "":
		return fmt.Errorf("usage: empty model")
	case e.TS.IsZero():
		return fmt.Errorf("usage: zero ts")
	case !e.Source.Valid():
		return fmt.Errorf("usage: invalid source %q", e.Source)
	case e.Tokens.Input < 0 || e.Tokens.Output < 0 || e.Tokens.CacheRead < 0 || e.Tokens.CacheWrite < 0:
		return fmt.Errorf("usage: negative token count")
	case e.CostMicros < 0:
		return fmt.Errorf("usage: negative cost_micros")
	}
	return nil
}
