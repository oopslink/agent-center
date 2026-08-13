package ruleregistry

import (
	"context"
	"strings"
	"time"
)

// LoadAudit is the fact that an agent successfully read one Team Rule body from
// a specific Team Memory commit. The body itself remains in git.
type LoadAudit struct {
	ExecutionID       string
	PlanningSessionID string
	TeamID            string
	TeamMemoryCommit  string
	RuleSlug          string
	Phase             string
	AgentID           string
	LoadedAt          time.Time
}

// AuditRecorder is the narrow port get_team_rule needs. Durable storage and
// read-model projection are intentionally separate follow-up work.
type AuditRecorder interface {
	AppendLoaded(ctx context.Context, audit LoadAudit) (inserted bool, err error)
}

func NormalizeLoadAudit(in LoadAudit) LoadAudit {
	out := LoadAudit{
		ExecutionID:       strings.TrimSpace(in.ExecutionID),
		PlanningSessionID: strings.TrimSpace(in.PlanningSessionID),
		TeamID:            strings.TrimSpace(in.TeamID),
		TeamMemoryCommit:  strings.TrimSpace(in.TeamMemoryCommit),
		RuleSlug:          strings.TrimSpace(in.RuleSlug),
		Phase:             strings.TrimSpace(in.Phase),
		AgentID:           strings.TrimSpace(in.AgentID),
		LoadedAt:          in.LoadedAt,
	}
	if out.LoadedAt.IsZero() {
		out.LoadedAt = time.Now().UTC()
	} else {
		out.LoadedAt = out.LoadedAt.UTC()
	}
	if out.ExecutionID != "" {
		out.PlanningSessionID = ""
	}
	return out
}
