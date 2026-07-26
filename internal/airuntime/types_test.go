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

func TestParameterSchemaValidationUsesFullJSONSchemaSemantics(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"config":{
				"type":"object",
				"properties":{
					"name":{"type":"string","minLength":3,"pattern":"^[a-z]+$"},
					"retries":{"type":"integer","minimum":1,"maximum":3},
					"tags":{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string"}}
				},
				"required":["name","retries","tags"],
				"additionalProperties":false
			}
		},
		"required":["config"]
	}`)
	valid := map[string]any{"config": map[string]any{
		"name": "codex", "retries": float64(2), "tags": []any{"fast", "safe"},
	}}
	if err := validateParameters(schema, valid); err != nil {
		t.Fatal(err)
	}
	invalid := []map[string]any{
		{"config": map[string]any{"name": "x", "retries": float64(2), "tags": []any{"fast"}}},
		{"config": map[string]any{"name": "Codex", "retries": float64(2), "tags": []any{"fast"}}},
		{"config": map[string]any{"name": "codex", "retries": float64(4), "tags": []any{"fast"}}},
		{"config": map[string]any{"name": "codex", "retries": float64(2), "tags": []any{"fast", "fast"}}},
	}
	for _, params := range invalid {
		if err := validateParameters(schema, params); err == nil {
			t.Fatalf("expected nested schema violation: %#v", params)
		}
	}
}

func TestValidateSchemaRejectsInvalidKeywordSemantics(t *testing.T) {
	for _, schema := range []json.RawMessage{
		json.RawMessage(`{"type":"object","properties":{"x":{"type":"not-a-type"}}}`),
		json.RawMessage(`{"type":"object","required":"x"}`),
		json.RawMessage(`{"type":"object","properties":{"x":{"minimum":"one"}}}`),
	} {
		if err := validateSchema(schema); err == nil {
			t.Fatalf("expected invalid schema: %s", schema)
		}
	}
}
