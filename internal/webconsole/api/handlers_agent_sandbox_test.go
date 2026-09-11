package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"reflect"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workforce"
	wfsqlite "github.com/oopslink/agent-center/internal/workforce/sqlite"
)

func TestAPI_AgentSandboxAction_EnqueuesRuntimeOwnedAction(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.WorkerRepo = wfsqlite.NewWorkerRepo(db)
	deps.EnvControl = envservice.New(envservice.Deps{
		DB:      db,
		Workers: envsqlite.NewWorkerRepo(db),
		Events:  envsqlite.NewControlEventRepo(db),
		IDGen:   idgen.NewGenerator(clock.SystemClock{}),
		Clock:   clock.SystemClock{},
	})
	sess := setupTestSession(t, db, deps)
	saveWorkerWithStatus(t, db, sess.OrgID, "w-1", workforce.WorkerOnline)
	if _, err := deps.EnvControl.ConnectWorker(context.Background(), environment.WorkerID("w-1")); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"coder","model":"claude","cli":"codex","worker_id":"w-1","sandbox_enabled":true,"sandbox_provider":"tart_macos_vm"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatalf("missing agent id: %v", created)
	}

	resp = orgScopedPost(t, s.URL+"/api/agents/"+id+"/sandbox/open_console", `{}`, sess)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("sandbox action: got %d, want 202", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["action"] != "open_console" || body["command_type"] != cmdTypeAgentSandboxAction {
		t.Fatalf("response = %+v, want open_console %s", body, cmdTypeAgentSandboxAction)
	}

	cmds, err := deps.EnvControl.CommandsAfter(context.Background(), environment.WorkerID("w-1"), 0)
	if err != nil {
		t.Fatalf("commands after: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("commands = %d, want 1", len(cmds))
	}
	if cmds[0].CommandType() != cmdTypeAgentSandboxAction {
		t.Fatalf("command type = %q, want %q", cmds[0].CommandType(), cmdTypeAgentSandboxAction)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(cmds[0].Payload()), &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["action"] != "open_console" || payload["agent_id"] == "" || payload["agent_id"] == id {
		t.Fatalf("payload = %+v, want action and internal execution agent id", payload)
	}
}

func TestAPI_AgentSandboxDesktopSession_DistinguishesConfiguredAndReachable(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"viewer","model":"claude","cli":"codex","worker_id":"w-1","sandbox_enabled":true,"sandbox_provider":"tart_macos_vm"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: got %d", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	id, _ := created["identity_id"].(string)
	if id == "" {
		t.Fatalf("missing agent id: %v", created)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	t.Setenv("AC_SANDBOX_VNC_ENDPOINT", ln.Addr().String())

	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/sandbox/desktop/session", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("desktop session ready: got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ok"] != true || body["status"] != "ready" || body["endpoint_state"] != "configured" {
		t.Fatalf("ready session = %+v", body)
	}

	_ = ln.Close()
	resp = orgScopedGet(t, s.URL+"/api/agents/"+id+"/sandbox/desktop/session", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("desktop session unreachable: got %d", resp.StatusCode)
	}
	body = map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ok"] != false || body["status"] != "unreachable" || body["endpoint_state"] != "unreachable" {
		t.Fatalf("unreachable session = %+v", body)
	}
}

func TestVNCSecurityTypesForWeb_PrefersPasswordAuth(t *testing.T) {
	got := vncSecurityTypesForWeb([]byte{30, 33, 36, 2, 35})
	if !reflect.DeepEqual(got, []byte{2}) {
		t.Fatalf("security types = %v, want password auth only", got)
	}
	original := []byte{30, 33, 36}
	got = vncSecurityTypesForWeb(original)
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("security types = %v, want unchanged", got)
	}
	got[0] = 99
	if original[0] == 99 {
		t.Fatalf("security types must return a copy for unchanged passthrough")
	}
}
