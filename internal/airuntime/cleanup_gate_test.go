package airuntime

import (
	"testing"
	"time"
)

func validCleanupEvidence() CleanupEvidence {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	return CleanupEvidence{
		BaselineSHA: "36676f14", WindowStartedAt: start, WindowEndedAt: end,
		FallbackSamples:       []LegacyFallbackSample{{ObjectType: "agent", ObservedAt: start, Count: 0, Source: "prometheus://release/agent"}, {ObjectType: "task", ObservedAt: end, Count: 0, Source: "prometheus://release/task"}},
		Migration:             MigrationReport{PlanSHA256: "migration-sha", Unmapped: []MigrationUnmapped{}, Summary: MigrationSummary{}},
		MigrationReportSHA256: "migration-artifact-sha",
		ConsumerAudit:         CleanupConsumerAudit{Environment: "production", ObservedAt: end, InventorySource: "gateway-and-tool-audit", NewPathProbe: "GET /api/orgs/acme/ai-runtime/catalog", NewPathReachable: true, ReportSHA256: "consumer-report-sha"},
		Acceptance:            CleanupAcceptance{DeploymentID: "isolated-stage6", ProcessFingerprint: "binary-sha/config-sha", Retry: true, Resume: true, Reassign: true, Cancel: true, HistoricalReadable: true, SnapshotStable: true, SecretPlaintextFree: true},
		Rollback:              CleanupRollbackEvidence{ArtifactSHA: "rollback-sha", TestedAt: end.Add(time.Hour), Succeeded: true},
		OwnerConfirmedAt:      end.Add(2 * time.Hour),
	}
}

func TestValidateCleanupGateRejectsMissingConsumerAudit(t *testing.T) {
	e := validCleanupEvidence()
	e.ConsumerAudit = CleanupConsumerAudit{}
	got := ValidateCleanupGate(e, 7*24*time.Hour)
	if got.Allowed {
		t.Fatalf("consumer inventory and replacement probe are mandatory: %+v", got)
	}
}

func TestValidateCleanupGateRejectsUnattributedFallbackSamples(t *testing.T) {
	e := validCleanupEvidence()
	e.FallbackSamples[0].Source = ""
	got := ValidateCleanupGate(e, 7*24*time.Hour)
	if got.Allowed {
		t.Fatalf("fallback samples must identify their data source: %+v", got)
	}
}

func TestValidateCleanupGateAllowsCompleteEvidence(t *testing.T) {
	got := ValidateCleanupGate(validCleanupEvidence(), 7*24*time.Hour)
	if !got.Allowed || len(got.Blockers) != 0 || got.EvidenceSHA256 == "" {
		t.Fatalf("gate = %+v", got)
	}
}

func TestValidateCleanupGateFailsClosed(t *testing.T) {
	got := ValidateCleanupGate(CleanupEvidence{}, 7*24*time.Hour)
	if got.Allowed || len(got.Blockers) < 6 {
		t.Fatalf("empty evidence must be blocked: %+v", got)
	}
}

func TestValidateCleanupGateRejectsNonZeroFallbackAndPendingMigration(t *testing.T) {
	e := validCleanupEvidence()
	e.FallbackSamples[0].Count = 1
	e.Migration.Summary.ObjectSelectionsToWrite = 1
	got := ValidateCleanupGate(e, 7*24*time.Hour)
	if got.Allowed || len(got.Blockers) != 2 {
		t.Fatalf("unsafe evidence must be blocked: %+v", got)
	}
}

func TestValidateCleanupGateRejectsOwnerConfirmationBeforeRollback(t *testing.T) {
	e := validCleanupEvidence()
	e.OwnerConfirmedAt = e.Rollback.TestedAt.Add(-time.Second)
	got := ValidateCleanupGate(e, 7*24*time.Hour)
	if got.Allowed {
		t.Fatalf("owner confirmation ordering must be enforced: %+v", got)
	}
}
