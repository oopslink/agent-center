package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// I109 ① — the tool-return half of "don't be silent": rejecting the edit server-side is
// only half the fix; the caller has to be TOLD, and told something it can act on. This
// locks the mapping against the two ways it could rot back into silence:
//
//   - falling through to the default 500 "internal" — which reads as "the center broke,
//     retry" rather than "this edit cannot reach the running executor", and
//   - dropping the reason text, leaving a bare status code to interpret.
//
// Asserts the RESPONSE a caller actually receives, not that mapDomainError was called.
func TestMapDomainError_TaskDescriptionFrozen_IsExplicit409WithReason(t *testing.T) {
	rec := httptest.NewRecorder()
	mapDomainError(rec, pm.ErrTaskDescriptionFrozen)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (a deliberate refusal must not masquerade as a 500 internal)", rec.Code)
	}
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "task_description_frozen" {
		t.Fatalf("error code = %q, want task_description_frozen (a distinguishable code, not a generic bucket)", body.Error)
	}
	// The message must actually explain the situation + the way out, not just name it —
	// the caller's next move is the judge gate or a re-dispatch, and it can only pick one
	// if we say so.
	for _, want := range []string{"frozen", "judge gate", "re-dispatch"} {
		if !strings.Contains(body.Message, want) {
			t.Fatalf("message = %q, want it to mention %q so the caller knows what happened and what to do instead", body.Message, want)
		}
	}
}
