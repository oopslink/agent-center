package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	tsqlite "github.com/oopslink/agent-center/internal/admintoken/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

// v2.4-D-X1 fix B5: worker enroll handler must mint a long-term
// admin token + return it in the response, so the daemon can stop
// using its one-time enroll token (which AuthMiddleware burns during
// the same request).

func newWorkerEnrollTestDeps(t *testing.T) HandlerDeps {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.SystemClock{}
	gen := idgen.NewGenerator(clk)
	eventRepo, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(eventRepo, eventRepo, gen, clk)
	tokenRepo := tsqlite.New(db)
	tokenSvc := admintokensvc.New(tokenRepo, gen, clk)
	workerRepo := wfsqlite.NewWorkerRepo(db)
	enrollSvc := wfservice.NewWorkerEnrollService(db, workerRepo, sink, clk)
	configSvc := wfservice.NewWorkerConfigService(db, workerRepo, sink, clk)
	return HandlerDeps{
		Actor:           observability.Actor("user:test"),
		DB:              db,
		WorkerRepo:      workerRepo,
		EnrollSvc:       enrollSvc,
		WorkerConfigSvc: configSvc,
		AdminTokenSvc:   tokenSvc,
	}
}

func postEnroll(t *testing.T, deps HandlerDeps, body any) (int, map[string]any) {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := WithDeps(deps)(srv.Handler())
	httpsrv := httptest.NewServer(h)
	defer httpsrv.Close()
	buf, _ := json.Marshal(body)
	resp, err := http.Post(httpsrv.URL+"/admin/workforce/worker/enroll", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestWorkerEnroll_ReturnsLongTermToken(t *testing.T) {
	deps := newWorkerEnrollTestDeps(t)
	status, body := postEnroll(t, deps, map[string]any{
		"worker_id":    "w-1",
		"capabilities": []string{"fakeagent"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if _, ok := body["admin_token"]; !ok {
		t.Errorf("response missing admin_token: %v", body)
	}
	tok, _ := body["admin_token"].(string)
	if tok == "" {
		t.Errorf("admin_token empty")
	}
	id, _ := body["admin_token_id"].(string)
	if id == "" {
		t.Errorf("admin_token_id empty")
	}
	// Cross-check: token can be looked up by id and has the expected
	// owner + scope set.
	found, err := deps.AdminTokenSvc.FindByID(context.Background(), admintoken.TokenID(id))
	if err != nil {
		t.Fatal(err)
	}
	if string(found.Owner()) != "worker:w-1" {
		t.Errorf("owner = %q", found.Owner())
	}
	gotScopes := map[string]bool{}
	for _, s := range found.Scopes() {
		gotScopes[string(s)] = true
	}
	for _, want := range []string{"workforce:enroll", "dispatch:pull", "task:*", "secret:resolve", "blob:put"} {
		if !gotScopes[want] {
			t.Errorf("token missing scope %q (have %v)", want, gotScopes)
		}
	}
}

func TestWorkerEnroll_NoTokenSvc_SurfacesEnrollResultUnchanged(t *testing.T) {
	deps := newWorkerEnrollTestDeps(t)
	deps.AdminTokenSvc = nil
	status, body := postEnroll(t, deps, map[string]any{
		"worker_id":    "w-2",
		"capabilities": []string{},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if _, ok := body["admin_token"]; ok {
		t.Errorf("admin_token should be absent when AdminTokenSvc nil: %v", body)
	}
	// Worker was still enrolled.
	w, err := deps.WorkerRepo.FindByID(context.Background(), "w-2")
	if err != nil {
		t.Fatal(err)
	}
	if w.ID() != workforce.WorkerID("w-2") {
		t.Errorf("wrong worker_id")
	}
}
