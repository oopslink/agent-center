package airuntime

import "time"

const (
	ReferenceSourceCatalogDefault = "catalog_default"
	ReferenceSourceProfile        = "runtime_profile"
)

type RuntimeRolloutPlan struct {
	Enabled bool   `json:"enabled"`
	Percent int    `json:"percent,omitempty"`
	Label   string `json:"label,omitempty"`
}

type RuntimeReferenceCount struct {
	Source     string `json:"source"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Count      int    `json:"count"`
	Mutable    bool   `json:"mutable"`
}

type RuntimeImpactRequest struct {
	EntityType string             `json:"entity_type"`
	EntityID   string             `json:"entity_id"`
	Action     string             `json:"action"`
	Rollout    RuntimeRolloutPlan `json:"rollout,omitempty"`
}

type RuntimeImpactPreview struct {
	EntityType               string                  `json:"entity_type"`
	EntityID                 string                  `json:"entity_id"`
	Action                   string                  `json:"action"`
	ReferenceCounts          []RuntimeReferenceCount `json:"reference_counts"`
	BasicCapabilityCoverage  []RuntimeCoverage       `json:"basic_capability_coverage"`
	ExecutionSchedulability  []RuntimeCoverage       `json:"execution_schedulability"`
	SnapshotBackMutation     bool                    `json:"snapshot_back_mutation"`
	HistoricalSnapshotPolicy string                  `json:"historical_snapshot_policy"`
	Rollout                  RuntimeRolloutPlan      `json:"rollout,omitempty"`
	CalculatedAt             time.Time               `json:"calculated_at"`
}

func normalizeCoverageScopes(in []RuntimeCoverage) []RuntimeCoverage {
	out := append([]RuntimeCoverage(nil), in...)
	if out == nil {
		return []RuntimeCoverage{}
	}
	for i := range out {
		if out[i].Scope == "" {
			out[i].Scope = CoverageScopeBasicCapability
		}
	}
	return out
}
