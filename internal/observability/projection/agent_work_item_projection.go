package projection

import (
	"errors"
	"time"
)

// AgentWorkItemProjection is the read-model VO mirroring a single
// agent_work_item_projections row (mig 0046) — the new-model equivalent of
// TaskExecutionProjection. PK = work_item_id (1:1 with agent_work_items.id),
// no version column (high-frequency UPSERT, staleness guarded by
// last_activity_at — the new "last_push_at").
type AgentWorkItemProjection struct {
	WorkItemID                string
	AgentID                   string
	Status                    string
	CurrentActivity           string
	CurrentActivityAt         time.Time
	TotalToolCalls            int64
	TotalTokensInput          int64
	TotalTokensOutput         int64
	WorkingSecondsAccumulated int64
	LastActivityAt            time.Time
}

// AgentWorkItemProjectionUpdate is the VO carrying a single incremental
// projection push payload (the new-model analog of ProjectionUpdate). The
// work-item id is the key arg, so it is not part of the update body.
//
// The repository merges into the existing row (UPSERT) and applies staleness
// protection on LastActivityAt.
type AgentWorkItemProjectionUpdate struct {
	AgentID                   string
	Status                    string
	CurrentActivity           string
	CurrentActivityAt         time.Time
	TotalToolCalls            int64
	TotalTokensInput          int64
	TotalTokensOutput         int64
	WorkingSecondsAccumulated int64
	LastActivityAt            time.Time
}

// Validate checks that LastActivityAt is set; other fields are allowed to be
// zero (e.g. an early heartbeat may not yet have tool calls).
func (u AgentWorkItemProjectionUpdate) Validate() error {
	if u.LastActivityAt.IsZero() {
		return errors.New("agent work item projection update: last_activity_at required")
	}
	if u.TotalToolCalls < 0 ||
		u.TotalTokensInput < 0 ||
		u.TotalTokensOutput < 0 ||
		u.WorkingSecondsAccumulated < 0 {
		return errors.New("agent work item projection update: counters cannot be negative")
	}
	return nil
}
