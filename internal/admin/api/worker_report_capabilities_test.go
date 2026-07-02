package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/workforce"
)

// reportCapsFixture wires the report-capabilities endpoint behind the real
// AuthMiddleware so the §-1① ownership check (caller token Owner ==
// "worker:"+worker_id) is exercised end-to-end.
type reportCapsFixture struct {
	deps     HandlerDeps
	verifier *fakeVerifier
}

func newReportCapsFixture(t *testing.T) *reportCapsFixture {
	t.Helper()
	deps := newWorkerEnrollTestDeps(t)
	return &reportCapsFixture{
		deps:     deps,
		verifier: &fakeVerifier{tokens: map[string]*admintoken.AdminToken{}},
	}
}

func (f *reportCapsFixture) addWorkerToken(t *testing.T, plaintext, workerID string) {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID:        admintoken.TokenID("T-" + plaintext),
		Owner:     admintoken.Owner("worker:" + workerID),
		Scopes:    []admintoken.Scope{"workforce:enroll"},
		ValueHash: admintoken.HashPlaintext(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.verifier.tokens[plaintext] = tok
}

func (f *reportCapsFixture) seedWorker(t *testing.T, id string, caps []workforce.Capability) {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             workforce.WorkerID(id),
		CapabilityList: caps,
		EnrolledAt:     time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deps.WorkerRepo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

func (f *reportCapsFixture) postReport(t *testing.T, bearer string, body any) int {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	defer httpsrv.Close()
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, httpsrv.URL+"/admin/workforce/worker/capabilities", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// §-1①: a worker token may only report ITS OWN capabilities. A token owned by
// worker B reporting for worker A must be rejected 403 (no cross-worker write).
func TestWorkerReportCapabilities_CrossWorkerRejected(t *testing.T) {
	f := newReportCapsFixture(t)
	f.seedWorker(t, "W-1", []workforce.Capability{{AgentCLI: "claude-code", Detected: true, Enabled: true}})
	f.addWorkerToken(t, "acat_w2", "W-2") // caller is worker W-2

	status := f.postReport(t, "acat_w2", map[string]any{
		"worker_id":    "W-1",
		"capabilities": []map[string]any{{"agent_cli": "codex", "detected": true}},
	})
	if status != http.StatusForbidden {
		t.Fatalf("cross-worker report must be 403, got %d", status)
	}
	// W-1 must be untouched (codex not added by the rejected call).
	w, _ := f.deps.WorkerRepo.FindByID(context.Background(), "W-1")
	for _, c := range w.CapabilityList() {
		if c.AgentCLI == "codex" {
			t.Fatal("cross-worker report leaked codex onto W-1")
		}
	}
}

// Happy path: a worker reports its own probed capabilities and the new CLI is
// merged in (detected→enabled).
func TestWorkerReportCapabilities_SameWorkerMerges(t *testing.T) {
	f := newReportCapsFixture(t)
	f.seedWorker(t, "W-1", []workforce.Capability{{AgentCLI: "claude-code", Detected: true, Enabled: true}})
	f.addWorkerToken(t, "acat_w1", "W-1")

	status := f.postReport(t, "acat_w1", map[string]any{
		"worker_id": "W-1",
		"capabilities": []map[string]any{
			{"agent_cli": "claude-code", "detected": true},
			{"agent_cli": "codex", "detected": true, "version": "0.9"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("same-worker report must be 200, got %d", status)
	}
	w, _ := f.deps.WorkerRepo.FindByID(context.Background(), "W-1")
	var found bool
	for _, c := range w.CapabilityList() {
		if c.AgentCLI == "codex" {
			found = true
			if !c.Detected || !c.Enabled {
				t.Fatalf("codex should be detected+enabled, got %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("codex was not merged onto W-1")
	}
}

// T752: system_info in the report body is decoded and persisted through the HTTP
// layer, so the Worker Profile page can surface real host + build values.
func TestWorkerReportCapabilities_PersistsSystemInfo(t *testing.T) {
	f := newReportCapsFixture(t)
	f.seedWorker(t, "W-1", nil)
	f.addWorkerToken(t, "acat_w1", "W-1")

	status := f.postReport(t, "acat_w1", map[string]any{
		"worker_id": "W-1",
		"system_info": map[string]any{
			"hostname":             "dev001.local",
			"os":                   "linux",
			"arch":                 "amd64",
			"agent_center_version": "v2.10.2",
			"install_path":         "/usr/local/bin/agent-center",
			"worker_version":       "v2.10.2+abc1234",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("report must be 200, got %d", status)
	}
	w, _ := f.deps.WorkerRepo.FindByID(context.Background(), "W-1")
	got := w.SystemInfo()
	want := workforce.SystemInfo{
		Hostname: "dev001.local", OS: "linux", Arch: "amd64",
		AgentCenterVersion: "v2.10.2", InstallPath: "/usr/local/bin/agent-center",
		WorkerVersion: "v2.10.2+abc1234",
	}
	if got != want {
		t.Fatalf("persisted system_info = %+v, want %+v", got, want)
	}
}
