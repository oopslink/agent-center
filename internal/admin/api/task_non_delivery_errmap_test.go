package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestMapDomainError_TaskNonDeliveryIsExplicitConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	mapDomainError(rec, pm.ErrTaskNoValidDelivery)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", rec.Code)
	}
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "task_non_delivery" {
		t.Fatalf("error=%q", body.Error)
	}
	for _, want := range []string{"pushed delivery", "block or retry"} {
		if !strings.Contains(body.Message, want) {
			t.Fatalf("message=%q missing %q", body.Message, want)
		}
	}
}
