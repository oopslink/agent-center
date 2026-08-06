package workforce

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeCapabilityVersion(t *testing.T) {
	got, err := NormalizeCapabilityVersion("v1.2")
	if err != nil {
		t.Fatalf("NormalizeCapabilityVersion: %v", err)
	}
	if got != "1.2.0" {
		t.Fatalf("normalized=%q, want 1.2.0", got)
	}
	if _, err := NormalizeCapabilityVersion("release latest"); !errors.Is(err, ErrWorkerCapabilityVersion) {
		t.Fatalf("bad version err=%v, want ErrWorkerCapabilityVersion", err)
	}
}

func TestCapabilityMatches_TTLVersionAndFeatures(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	cap := Capability{
		AgentCLI: "codex", Detected: true, Enabled: true, Healthy: true,
		Version: "1.4.0", Features: []string{"json", "mcp"},
		ScannedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
	}
	if got := cap.Matches(CapabilityRequirement{AgentCLI: "codex", VersionConstraint: ">=1.3.0", Features: []string{"MCP"}}, now); !got.OK {
		t.Fatalf("capability should match, got %+v", got)
	}
	if got := cap.Matches(CapabilityRequirement{AgentCLI: "codex", VersionConstraint: ">=2.0.0"}, now); got.OK || got.Reason != CapabilityMatchVersionMismatch {
		t.Fatalf("version mismatch got %+v", got)
	}
	if got := cap.Matches(CapabilityRequirement{AgentCLI: "codex", Features: []string{"session"}}, now); got.OK || got.Reason != CapabilityMatchMissingFeature {
		t.Fatalf("missing feature got %+v", got)
	}
	if got := cap.Matches(CapabilityRequirement{AgentCLI: "codex"}, now.Add(2*time.Minute)); got.OK || got.Reason != CapabilityMatchStale {
		t.Fatalf("expired got %+v", got)
	}
}
