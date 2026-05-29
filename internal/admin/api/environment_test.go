package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// =============================================================================
// v2.7 D1 (#102) — Environment BC control-channel LOG-LAYER acceptance.
//
// These tests prove the CENTER-SIDE LOG invariants only:
//   (a) a reconnecting Worker replays from its ack cursor — already-ACKED
//       commands are NOT re-delivered; and
//   (b) every delivered command CARRIES its idempotency_key.
//
// They do NOT (and must not) claim EXECUTION-LAYER idempotency. The scenario
// where a Worker EXECUTES a command, CRASHES before acking, and on replay must
// SKIP re-executing the destructive command (using the carried idempotency_key)
// is the Worker command processor's job — that is D2, not D1. D1 guarantees the
// stream content + replay cursor; D2 guarantees exactly-once execution on top.
// =============================================================================

func newEnvTestDeps(t *testing.T) (HandlerDeps, environment.WorkerRepository) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)

	// Seed the workforce.Worker so connect can resolve org provenance.
	wfRepo := wfsqlite.NewWorkerRepo(db)
	wfw, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:             "w-1",
		OrganizationID: "org-1",
		EnrolledAt:     clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := wfRepo.Save(ctx, wfw); err != nil {
		t.Fatal(err)
	}

	envWorkers := envsql.NewWorkerRepo(db)
	envControl := envservice.New(envservice.Deps{
		DB:      db,
		Workers: envWorkers,
		Events:  envsql.NewControlEventRepo(db),
		IDGen:   gen,
		Clock:   clk,
	})
	return HandlerDeps{
		DB:            db,
		WorkerRepo:    wfRepo,
		EnvControlSvc: envControl,
	}, envWorkers
}

func envServer(t *testing.T, deps HandlerDeps) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := WithDeps(deps)(srv.Handler())
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

func envPost(t *testing.T, base, path string, body any) (int, map[string]any) {
	t.Helper()
	buf, _ := json.Marshal(body)
	resp, err := http.Post(base+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func envGet(t *testing.T, base, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestEnvControl_LogLayer_ReplayFromAckCursor is the #102 acceptance core. It
// proves the LOG layer over the real admin server: connect → enqueue (incl.
// destructive agent.reset/agent.stop) → list carries idempotency_key → ack →
// reconnect replays only un-acked offsets → idempotent enqueue dedups.
func TestEnvControl_LogLayer_ReplayFromAckCursor(t *testing.T) {
	deps, _ := newEnvTestDeps(t)
	srv := envServer(t, deps)

	// --- connect → last_acked_offset 0 ----------------------------------
	status, body := envPost(t, srv.URL, "/admin/environment/worker/connect",
		map[string]any{"worker_id": "w-1"})
	if status != http.StatusOK {
		t.Fatalf("connect status = %d, body = %v", status, body)
	}
	if got := body["last_acked_offset"].(float64); got != 0 {
		t.Fatalf("connect last_acked_offset = %v, want 0", got)
	}
	if body["status"] != "online" {
		t.Fatalf("connect status field = %v, want online", body["status"])
	}

	// --- enqueue several synthetic commands in-process (D1 path), incl.
	//     destructive ones with idempotency keys -------------------------
	enqueue := func(cmdType, key string) {
		t.Helper()
		_, err := deps.EnvControlSvc.EnqueueCommand(context.Background(), environment.AppendCommandInput{
			WorkerID:       "w-1",
			CommandType:    cmdType,
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("enqueue %s/%s: %v", cmdType, key, err)
		}
	}
	enqueue("agent.start", "idem-1")
	enqueue("agent.reset", "idem-2") // destructive
	enqueue("agent.stop", "idem-3")  // destructive

	// --- GET commands?after=0 → all, ascending, each carrying its key ---
	status, body = envGet(t, srv.URL, "/admin/environment/worker/commands?worker_id=w-1&after=0")
	if status != http.StatusOK {
		t.Fatalf("commands status = %d, body = %v", status, body)
	}
	cmds := toCmdList(t, body)
	if len(cmds) != 3 {
		t.Fatalf("got %d commands, want 3: %v", len(cmds), cmds)
	}
	wantTypes := []string{"agent.start", "agent.reset", "agent.stop"}
	wantKeys := []string{"idem-1", "idem-2", "idem-3"}
	for i, c := range cmds {
		if off := c["offset"].(float64); off != float64(i+1) {
			t.Fatalf("cmd[%d] offset = %v, want %d (ascending)", i, off, i+1)
		}
		if c["command_type"] != wantTypes[i] {
			t.Fatalf("cmd[%d] type = %v, want %s", i, c["command_type"], wantTypes[i])
		}
		// (b) every command CARRIES its idempotency_key.
		if c["idempotency_key"] != wantKeys[i] {
			t.Fatalf("cmd[%d] idempotency_key = %v, want %s", i, c["idempotency_key"], wantKeys[i])
		}
	}

	// --- ack {offset: 1} → cursor advances to 1 -------------------------
	status, body = envPost(t, srv.URL, "/admin/environment/worker/ack",
		map[string]any{"worker_id": "w-1", "offset": 1})
	if status != http.StatusOK {
		t.Fatalf("ack status = %d, body = %v", status, body)
	}
	if got := body["last_acked_offset"].(float64); got != 1 {
		t.Fatalf("ack last_acked_offset = %v, want 1", got)
	}

	// --- (a) reconnect simulation: GET commands?after=1 returns ONLY
	//     offsets > 1. The already-acked offset-1 command is NOT re-sent. -
	status, body = envGet(t, srv.URL, "/admin/environment/worker/commands?worker_id=w-1&after=1")
	if status != http.StatusOK {
		t.Fatalf("replay status = %d, body = %v", status, body)
	}
	replay := toCmdList(t, body)
	if len(replay) != 2 {
		t.Fatalf("replay len = %d, want 2 (acked offset-1 not re-delivered)", len(replay))
	}
	for _, c := range replay {
		if off := c["offset"].(float64); off <= 1 {
			t.Fatalf("replay delivered acked/old offset %v (must be > 1)", off)
		}
	}

	// --- idempotent enqueue: same key twice → ONE stream entry ----------
	enqueue("agent.stop", "idem-3") // re-issue of the destructive idem-3
	status, body = envGet(t, srv.URL, "/admin/environment/worker/commands?worker_id=w-1&after=0")
	if status != http.StatusOK {
		t.Fatalf("post-dedup status = %d, body = %v", status, body)
	}
	if all := toCmdList(t, body); len(all) != 3 {
		t.Fatalf("after re-issuing idem-3: %d entries, want 3 (no duplicate destructive command)", len(all))
	}
}

func TestEnvControl_Connect_UnknownWorkforceWorker_404(t *testing.T) {
	deps, _ := newEnvTestDeps(t)
	srv := envServer(t, deps)
	status, body := envPost(t, srv.URL, "/admin/environment/worker/connect",
		map[string]any{"worker_id": "ghost"})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %v", status, body)
	}
}

func TestEnvControl_Heartbeat_OK(t *testing.T) {
	deps, _ := newEnvTestDeps(t)
	srv := envServer(t, deps)
	if status, _ := envPost(t, srv.URL, "/admin/environment/worker/connect",
		map[string]any{"worker_id": "w-1"}); status != http.StatusOK {
		t.Fatalf("connect status = %d", status)
	}
	status, body := envPost(t, srv.URL, "/admin/environment/worker/heartbeat",
		map[string]any{"worker_id": "w-1"})
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("heartbeat status = %d, body = %v", status, body)
	}
}

// toCmdList extracts the "commands" array from a decoded JSON body.
func toCmdList(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["commands"].([]any)
	if !ok {
		t.Fatalf("body has no commands array: %v", body)
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}
