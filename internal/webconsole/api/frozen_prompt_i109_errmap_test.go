package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// I109 ① — the console surface reaches the same UpdateTask / BatchUpdateTask entrypoints
// (PATCH .../tasks/{id}), so it needs the same explicit refusal: a 500 "pm_error" would
// read as a center fault instead of "this edit cannot reach the running executor".
// Asserts the response a caller receives, not that the mapper ran.
func TestMapPMError_TaskDescriptionFrozen_IsExplicit409WithReason(t *testing.T) {
	rec := httptest.NewRecorder()
	mapPMError(rec, pm.ErrTaskDescriptionFrozen)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (must not fall through to the default 500 pm_error)", rec.Code)
	}
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "task_description_frozen" {
		t.Fatalf("error code = %q, want task_description_frozen", body.Error)
	}
	if body.Message == "" {
		t.Fatal("message is empty — the refusal must carry its reason to the caller")
	}
}
