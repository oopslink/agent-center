package airuntime

import (
	"encoding/json"
	"testing"
)

func TestContractJSONRoundTripAndStableReasons(t *testing.T) {
	selection := RuntimeSelection{Mode: "override", CLIID: "cli-1", ModelID: "model-1", Parameters: map[string]any{"temperature": 0.2}}
	raw, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	var got RuntimeSelection
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != selection.Mode || got.CLIID != selection.CLIID || got.ModelID != selection.ModelID {
		t.Fatalf("round trip = %+v", got)
	}
	want := []Reason{ReasonCLINotFound, ReasonModelNotFound, ReasonIncompatible, ReasonParametersInvalid, ReasonProfileDisabled, ReasonDefaultMissing, ReasonRevisionConflict}
	seen := map[string]bool{}
	for _, reason := range want {
		if reason == "" || seen[string(reason)] {
			t.Fatalf("unstable/duplicate reason %q", reason)
		}
		seen[string(reason)] = true
	}
	flags := DefaultFeatureFlags()
	if flags.CatalogV2 || flags.SchedulerMatching {
		t.Fatal("feature flags must default off to preserve legacy behavior")
	}
}

func TestParameterSchemaValidation(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["fast","safe"]},"retries":{"type":"integer"}},"required":["mode"],"additionalProperties":false}`)
	if err := validateParameters(schema, map[string]any{"mode": "safe", "retries": float64(2)}); err != nil {
		t.Fatal(err)
	}
	for _, params := range []map[string]any{{}, {"mode": "other"}, {"mode": "safe", "extra": true}, {"mode": "safe", "retries": 1.5}} {
		if err := validateParameters(schema, params); err == nil {
			t.Fatalf("expected invalid: %#v", params)
		}
	}
}
