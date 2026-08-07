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
	mapDomainError(rec, pm.NewTaskNoValidDeliveryError(&pm.Delivery{
		Probed: true, Pushed: false, Dirty: true, BaseKnown: true, AheadOfBase: 0,
		Branch: "feat/x", HeadSHA: "abc", PushError: "remote rejected",
	}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", rec.Code)
	}
	var body struct {
		Error       string   `json:"error"`
		Message     string   `json:"message"`
		ReasonCodes []string `json:"reason_codes"`
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
	joined := strings.Join(body.ReasonCodes, ",")
	for _, want := range []string{"head_not_pushed", "worktree_dirty", "no_commit_ahead_of_base"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("reason_codes=%v missing %s", body.ReasonCodes, want)
		}
	}
}
