package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

func postHeartbeat(t *testing.T, deps HandlerDeps, body any) int {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := WithDeps(deps)(srv.Handler())
	httpsrv := httptest.NewServer(h)
	defer httpsrv.Close()
	buf, _ := json.Marshal(body)
	resp, err := http.Post(httpsrv.URL+"/admin/workforce/worker/heartbeat", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// v2.19.0: the heartbeat writes the per-agent snapshots to LiveState (when present),
// and a LEGACY body without the field still succeeds (back-compat).
func TestWorkerHeartbeat_WritesConcurrencySnapshots(t *testing.T) {
	deps := newWorkerEnrollTestDeps(t)
	store := concurrency.NewInMemoryStore()
	deps.LiveState = store
	// The worker must exist (enroll creates it).
	if status, _ := postEnroll(t, deps, map[string]any{"worker_id": "w-1", "capabilities": []string{"claude-code"}}); status != http.StatusOK {
		t.Fatalf("enroll status = %d", status)
	}

	// Heartbeat WITH snapshots → 200 + stored.
	status := postHeartbeat(t, deps, map[string]any{
		"worker_id": "w-1",
		"agent_concurrency_snapshots": map[string]any{
			"agent-1": map[string]any{
				"active": 2,
				"executors": []map[string]any{
					{"executor_id": "e1", "task_id": "t1", "cli": "codex", "model": "gpt-5.5", "state": "running", "pid": 111},
					{"executor_id": "e2", "state": "starting"},
				},
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want 200", status)
	}
	snap, _, ok := store.Get("agent-1", time.Now())
	if !ok {
		t.Fatal("snapshot not written to LiveState")
	}
	if snap.Active != 2 || len(snap.Executors) != 2 {
		t.Fatalf("stored snapshot = %+v, want active=2 execs=2", snap)
	}
	if snap.Executors[0].CLI != "codex" || snap.Executors[0].Model != "gpt-5.5" || snap.Executors[0].TaskID != "t1" {
		t.Errorf("executor[0] = %+v", snap.Executors[0])
	}
}

// A legacy worker (heartbeat body with NO agent_concurrency_snapshots) must still
// get 200 and write nothing — the field is purely additive.
func TestWorkerHeartbeat_LegacyBody_BackCompat(t *testing.T) {
	deps := newWorkerEnrollTestDeps(t)
	store := concurrency.NewInMemoryStore()
	deps.LiveState = store
	if status, _ := postEnroll(t, deps, map[string]any{"worker_id": "w-old", "capabilities": []string{"claude-code"}}); status != http.StatusOK {
		t.Fatalf("enroll status = %d", status)
	}
	status := postHeartbeat(t, deps, map[string]any{
		"worker_id":                  "w-old",
		"additional_working_seconds": 5,
	})
	if status != http.StatusOK {
		t.Fatalf("legacy heartbeat status = %d, want 200", status)
	}
	if _, _, ok := store.Get("any", time.Now()); ok {
		t.Error("legacy heartbeat must not write any snapshot")
	}
}
