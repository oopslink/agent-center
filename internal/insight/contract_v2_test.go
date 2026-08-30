package insight

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRatioContractFixtures(t *testing.T) {
	tests := []struct {
		name                   string
		numerator, denominator int64
		want                   *float64
	}{
		{name: "zero is known", numerator: 0, denominator: 4, want: floatPtr(0)},
		{name: "quarter", numerator: 1, denominator: 4, want: floatPtr(.25)},
		{name: "empty is unknown", numerator: 0, denominator: 0, want: nil},
		{name: "invalid negative is unknown", numerator: -1, denominator: 4, want: nil},
		{name: "invalid overflow is unknown", numerator: 5, denominator: 4, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Ratio(tt.numerator, tt.denominator)
			if tt.want == nil && got != nil {
				t.Fatalf("got %v, want nil", *got)
			}
			if tt.want != nil && (got == nil || *got != *tt.want) {
				t.Fatalf("got %v, want %v", got, *tt.want)
			}
		})
	}
}

func TestV2ContractDistinguishesZeroAndUnknown(t *testing.T) {
	zero := float64(0)
	fresh := Freshness{State: "fresh", AgeMS: 1, ThresholdMS: 120000}
	known := MetricValue{Value: &zero, Unit: "ratio", Meta: MetricMeta{MetricVersion: MetricVersionV2, SampleCount: 4, Coverage: &zero, Freshness: fresh, Known: true}}
	unknown := MetricValue{Value: nil, Unit: "ratio", Meta: MetricMeta{MetricVersion: MetricVersionV2, Freshness: fresh, UnknownCount: 4, Known: false}}
	b, err := json.Marshal([]MetricValue{known, unknown})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got[0]["value"] != float64(0) {
		t.Fatalf("known zero lost: %s", b)
	}
	if got[1]["value"] != nil {
		t.Fatalf("unknown must marshal null: %s", b)
	}
}

func TestFunnelBreakKindsAreCompleteAndUnique(t *testing.T) {
	all := []FunnelBreakKind{FunnelIssueWithoutTask, FunnelTaskWithoutPlan, FunnelTaskMultipleContainers, FunnelDonePlanNonTerminalTask, FunnelDonePlanOpenIssue, FunnelEvolutionOldGenerationResidue, FunnelDeliverySHALineageMismatch}
	seen := map[FunnelBreakKind]bool{}
	for _, kind := range all {
		if seen[kind] {
			t.Fatalf("duplicate %q", kind)
		}
		seen[kind] = true
	}
	if len(seen) != 7 {
		t.Fatalf("got %d funnel break kinds", len(seen))
	}
}

func TestHealthDecisionIsBackendOwned(t *testing.T) {
	d := HealthDecision{Status: HealthUnknown, ReasonCodes: []ReasonCode{ReasonDataNoSamples, ReasonDataStale}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"status":"unknown","reason_codes":["data.no_samples","data.stale"],"evidence":null}` {
		t.Fatalf("unexpected wire contract: %s", b)
	}
}

func TestV2JSONSchemaIsValidAndVersioned(t *testing.T) {
	b, err := os.ReadFile("../../docs/contracts/insight-v2.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties missing")
	}
	version, ok := properties["metric_version"].(map[string]any)
	if !ok || version["const"] != MetricVersionV2 {
		t.Fatalf("schema version = %#v", version["const"])
	}
}

func floatPtr(v float64) *float64 { return &v }
