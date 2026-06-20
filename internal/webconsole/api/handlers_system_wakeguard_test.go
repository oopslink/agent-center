package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/wakeguard"
	settingssql "github.com/oopslink/agent-center/internal/settings/sqlite"
)

// getGuardrail GETs the effective config off the test server.
func getGuardrail(t *testing.T, url string) (int, wakeGuardrailBody) {
	t.Helper()
	resp, err := http.Get(url + "/api/system/wake-guardrail")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body wakeGuardrailBody
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// putGuardrail PUTs a body and returns the status code.
func putGuardrail(t *testing.T, url string, body wakeGuardrailBody) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", url+"/api/system/wake-guardrail", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// With no settings store wired, GET reports the conservative code defaults (the
// guard is never shown as disabled) and PUT is not_configured.
func TestWakeGuardrail_NoStore_GetDefaults_PutNotConfigured(t *testing.T) {
	deps, _ := setupAPI(t) // SettingsStore is nil
	s := newTestServer(t, deps)
	defer s.Close()

	code, body := getGuardrail(t, s.URL)
	if code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", code)
	}
	want := wakeGuardrailBodyFromConfig(wakeguard.DefaultConfig())
	if body != want {
		t.Errorf("GET (no store) = %+v, want defaults %+v", body, want)
	}
	if code := putGuardrail(t, s.URL, want); code != http.StatusNotImplemented {
		t.Errorf("PUT (no store) status = %d, want 501", code)
	}
}

// PUT persists valid thresholds; a subsequent GET reflects them (the read path
// the WakeGuard also uses → params take effect without restart).
func TestWakeGuardrail_PutThenGet(t *testing.T) {
	deps, db := setupAPI(t)
	deps.SettingsStore = settingssql.NewStore(db, nil)
	s := newTestServer(t, deps)
	defer s.Close()

	put := wakeGuardrailBody{MaxDepth: 6, CycleWindowSec: 120, CycleThreshold: 5, RatePerMin: 20, ChainTokenBudget: 32}
	if code := putGuardrail(t, s.URL, put); code != http.StatusOK {
		t.Fatalf("PUT valid status = %d, want 200", code)
	}
	code, body := getGuardrail(t, s.URL)
	if code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", code)
	}
	if body != put {
		t.Errorf("GET after PUT = %+v, want %+v", body, put)
	}
}

// An invalid (non-positive) field is rejected with 400 and NOTHING is persisted
// (a later GET still shows defaults).
func TestWakeGuardrail_PutInvalid_Rejected(t *testing.T) {
	deps, db := setupAPI(t)
	deps.SettingsStore = settingssql.NewStore(db, nil)
	s := newTestServer(t, deps)
	defer s.Close()

	bad := wakeGuardrailBody{MaxDepth: 0, CycleWindowSec: 120, CycleThreshold: 5, RatePerMin: 20, ChainTokenBudget: 32}
	if code := putGuardrail(t, s.URL, bad); code != http.StatusBadRequest {
		t.Fatalf("PUT invalid status = %d, want 400", code)
	}
	_, body := getGuardrail(t, s.URL)
	if want := wakeGuardrailBodyFromConfig(wakeguard.DefaultConfig()); body != want {
		t.Errorf("after rejected PUT, GET = %+v, want unchanged defaults %+v", body, want)
	}
}
