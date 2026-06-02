package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/workforce"
)

func newToggleFixture(t *testing.T) *reportCapsFixture {
	t.Helper()
	return newReportCapsFixture(t) // same deps (now also wires WorkerConfigSvc)
}

func (f *reportCapsFixture) addOperatorToken(t *testing.T, plaintext, owner string) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID:        admintoken.TokenID("T-" + plaintext),
		Owner:     admintoken.Owner(owner),
		Scopes:    []admintoken.Scope{"*"},
		ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

func (f *reportCapsFixture) patchToggle(t *testing.T, bearer, workerID, cli string, enabled bool) int {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	defer httpsrv.Close()
	buf, _ := json.Marshal(map[string]any{"enabled": enabled})
	url := httpsrv.URL + "/admin/workforce/worker/" + workerID + "/capabilities/" + cli + "/enabled"
	req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func capEnabled(t *testing.T, f *reportCapsFixture, id, cli string) bool {
	t.Helper()
	w, err := f.deps.WorkerRepo.FindByID(context.Background(), workforce.WorkerID(id))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range w.CapabilityList() {
		if c.AgentCLI == cli {
			return c.Enabled
		}
	}
	t.Fatalf("cli %s not found on %s", cli, id)
	return false
}

// Operator toggles a worker's per-CLI Enabled flag off.
func TestWorkerToggleCapability_OperatorDisables(t *testing.T) {
	f := newToggleFixture(t)
	f.seedWorker(t, "W-1", []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true},
		{AgentCLI: "codex", Detected: true, Enabled: true},
	})
	f.addOperatorToken(t, "acat_op", "operator:admin")

	status := f.patchToggle(t, "acat_op", "W-1", "codex", false)
	if status != http.StatusOK {
		t.Fatalf("operator toggle must be 200, got %d", status)
	}
	if capEnabled(t, f, "W-1", "codex") {
		t.Fatal("codex should be disabled after operator toggle")
	}
}

// §-1: a worker token must NOT be able to toggle capabilities — that is an
// operator decision. Worker-owned caller → 403.
func TestWorkerToggleCapability_WorkerForbidden(t *testing.T) {
	f := newToggleFixture(t)
	f.seedWorker(t, "W-1", []workforce.Capability{
		{AgentCLI: "codex", Detected: true, Enabled: true},
	})
	f.addWorkerToken(t, "acat_w1", "W-1") // worker token, not operator

	status := f.patchToggle(t, "acat_w1", "W-1", "codex", false)
	if status != http.StatusForbidden {
		t.Fatalf("worker self-toggle must be 403, got %d", status)
	}
	if !capEnabled(t, f, "W-1", "codex") {
		t.Fatal("codex must stay enabled — worker toggle was rejected")
	}
}
