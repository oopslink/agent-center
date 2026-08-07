package airuntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const ReasonCleanupGateBlocked Reason = "runtime_cleanup_gate_blocked"

// CleanupEvidence is the release-owned proof required before irreversible
// legacy cleanup. Missing evidence is deliberately treated as a failed gate.
type CleanupEvidence struct {
	BaselineSHA           string                  `json:"baseline_sha"`
	WindowStartedAt       time.Time               `json:"window_started_at"`
	WindowEndedAt         time.Time               `json:"window_ended_at"`
	FallbackSamples       []LegacyFallbackSample  `json:"fallback_samples"`
	Migration             MigrationReport         `json:"migration_report"`
	MigrationReportSHA256 string                  `json:"migration_report_sha256"`
	ConsumerAudit         CleanupConsumerAudit    `json:"consumer_audit"`
	Acceptance            CleanupAcceptance       `json:"isolated_acceptance"`
	Rollback              CleanupRollbackEvidence `json:"rollback"`
	OwnerConfirmedAt      time.Time               `json:"owner_confirmed_at"`
}

type LegacyFallbackSample struct {
	ObjectType string    `json:"object_type"`
	ObservedAt time.Time `json:"observed_at"`
	Count      uint64    `json:"count"`
	Source     string    `json:"source"`
}

// CleanupConsumerAudit proves that the retired surfaces have no production
// callers and that the replacement surface was probed in the same environment.
// ReportSHA256 binds the decision to the release-owned audit artifact.
type CleanupConsumerAudit struct {
	Environment         string    `json:"environment"`
	ObservedAt          time.Time `json:"observed_at"`
	InventorySource     string    `json:"inventory_source"`
	LegacyConsumerCount uint64    `json:"legacy_consumer_count"`
	NewPathProbe        string    `json:"new_path_probe"`
	NewPathReachable    bool      `json:"new_path_reachable"`
	ReportSHA256        string    `json:"report_sha256"`
}

type CleanupAcceptance struct {
	DeploymentID        string `json:"deployment_id"`
	ProcessFingerprint  string `json:"process_fingerprint"`
	Retry               bool   `json:"retry"`
	Resume              bool   `json:"resume"`
	Reassign            bool   `json:"reassign"`
	Cancel              bool   `json:"cancel"`
	HistoricalReadable  bool   `json:"historical_execution_readable"`
	SnapshotStable      bool   `json:"snapshot_stable"`
	SecretPlaintextFree bool   `json:"secret_plaintext_free"`
}

type CleanupRollbackEvidence struct {
	ArtifactSHA string    `json:"artifact_sha"`
	TestedAt    time.Time `json:"tested_at"`
	Succeeded   bool      `json:"succeeded"`
}

type CleanupGateResult struct {
	Allowed        bool     `json:"allowed"`
	EvidenceSHA256 string   `json:"evidence_sha256"`
	Blockers       []string `json:"blockers"`
}

// ValidateCleanupGate is pure so release tooling can run it before deployment.
// It never assumes absent metrics, reports, or check results are successful.
func ValidateCleanupGate(e CleanupEvidence, minimumWindow time.Duration) CleanupGateResult {
	result := CleanupGateResult{Blockers: []string{}}
	if minimumWindow <= 0 {
		minimumWindow = 24 * time.Hour
	}
	if strings.TrimSpace(e.BaselineSHA) == "" {
		result.Blockers = append(result.Blockers, "baseline_sha is required")
	}
	if e.WindowStartedAt.IsZero() || e.WindowEndedAt.IsZero() || e.WindowEndedAt.Sub(e.WindowStartedAt) < minimumWindow {
		result.Blockers = append(result.Blockers, "fallback observation window is missing or too short")
	}
	if len(e.FallbackSamples) == 0 {
		result.Blockers = append(result.Blockers, "fallback samples are required")
	}
	seenTypes := map[string]bool{}
	for _, sample := range e.FallbackSamples {
		if strings.TrimSpace(sample.ObjectType) == "" || strings.TrimSpace(sample.Source) == "" || sample.ObservedAt.Before(e.WindowStartedAt) || sample.ObservedAt.After(e.WindowEndedAt) {
			result.Blockers = append(result.Blockers, "fallback sample is outside the declared window or has no object_type")
		}
		if sample.Count != 0 {
			result.Blockers = append(result.Blockers, "legacy fallback count is non-zero")
		}
		seenTypes[sample.ObjectType] = true
	}
	if len(seenTypes) == 0 {
		result.Blockers = append(result.Blockers, "no fallback object types were observed")
	}
	if len(e.Migration.Unmapped) != 0 || e.Migration.Summary.Unmapped != 0 || e.Migration.Summary.ProfilesToCreate != 0 || e.Migration.Summary.ObjectSelectionsToWrite != 0 {
		result.Blockers = append(result.Blockers, "migration report still has pending or unmapped work")
	}
	if strings.TrimSpace(e.Migration.PlanSHA256) == "" {
		result.Blockers = append(result.Blockers, "migration report digest is required")
	}
	if strings.TrimSpace(e.MigrationReportSHA256) == "" {
		result.Blockers = append(result.Blockers, "migration report artifact digest is required")
	}
	c := e.ConsumerAudit
	if strings.TrimSpace(c.Environment) == "" || c.ObservedAt.IsZero() ||
		strings.TrimSpace(c.InventorySource) == "" || c.LegacyConsumerCount != 0 ||
		strings.TrimSpace(c.NewPathProbe) == "" || !c.NewPathReachable || strings.TrimSpace(c.ReportSHA256) == "" {
		result.Blockers = append(result.Blockers, "legacy consumer audit or replacement path probe is incomplete")
	}
	a := e.Acceptance
	if strings.TrimSpace(a.DeploymentID) == "" || strings.TrimSpace(a.ProcessFingerprint) == "" ||
		!a.Retry || !a.Resume || !a.Reassign || !a.Cancel || !a.HistoricalReadable || !a.SnapshotStable || !a.SecretPlaintextFree {
		result.Blockers = append(result.Blockers, "isolated deployment acceptance is incomplete")
	}
	if strings.TrimSpace(e.Rollback.ArtifactSHA) == "" || e.Rollback.TestedAt.IsZero() || !e.Rollback.Succeeded {
		result.Blockers = append(result.Blockers, "tested rollback artifact is required")
	}
	if e.OwnerConfirmedAt.IsZero() || e.OwnerConfirmedAt.Before(e.Rollback.TestedAt) {
		result.Blockers = append(result.Blockers, "owner confirmation must follow the rollback test")
	}
	sort.Strings(result.Blockers)
	raw, _ := json.Marshal(e)
	sum := sha256.Sum256(raw)
	result.EvidenceSHA256 = hex.EncodeToString(sum[:])
	result.Allowed = len(result.Blockers) == 0
	return result
}
