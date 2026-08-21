package mcphost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callOK runs a tool, fails on a protocol error or an unexpected IsError, and
// returns the result.
func callOK(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s unexpected IsError; content=%v", name, res.Content)
	}
	return res
}

// TestJSONToolsForwarding drives a representative sample of the new JSON tools
// and asserts the fakeAdmin received the right tool name + a body carrying the
// process-fixed agent_id plus the args under the EXACT admin field names.
func TestJSONToolsForwarding(t *testing.T) {
	cases := []struct {
		toolName string
		args     map[string]any
		wantTool string // admin route segment
		wantBody map[string]any
	}{
		{
			toolName: "get_task",
			args:     map[string]any{"task_id": "t-1"},
			wantTool: "get_task",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1"},
		},
		{
			toolName: "get_task_audit",
			args:     map[string]any{"task_id": "t-1", "page_size": 10, "offset": 20},
			wantTool: "get_task_audit",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "page_size": float64(10), "offset": float64(20)},
		},
		{
			toolName: "get_task_execution",
			args:     map[string]any{"task_id": "t-1", "execution_id": "exec-1"},
			wantTool: "get_task_execution",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "execution_id": "exec-1"},
		},
		{
			toolName: "get_agent_runtime_effective_config",
			args:     map[string]any{},
			wantTool: "get_agent_runtime_effective_config",
			wantBody: map[string]any{"agent_id": "agent-X"},
		},
		{
			toolName: "get_team_rule_index",
			args:     map[string]any{"phase": "execute", "execution_id": "exec-1"},
			wantTool: "get_team_rule_index",
			wantBody: map[string]any{"agent_id": "agent-X", "phase": "execute", "execution_id": "exec-1"},
		},
		{
			toolName: "get_team_rule",
			args:     map[string]any{"slug": "prefer-tests", "commit": "0123456789012345678901234567890123456789", "phase": "execute", "execution_id": "exec-1"},
			wantTool: "get_team_rule",
			wantBody: map[string]any{"agent_id": "agent-X", "slug": "prefer-tests", "commit": "0123456789012345678901234567890123456789", "phase": "execute", "execution_id": "exec-1"},
		},
		{
			toolName: "fork_executor",
			args:     map[string]any{"task_id": "t-7", "model": "claude-sonnet", "context": "use the fast path"},
			wantTool: "fork_executor",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-7", "model": "claude-sonnet", "context": "use the fast path"},
		},
		{
			toolName: "get_issue",
			args:     map[string]any{"issue_id": "i-9"},
			wantTool: "get_issue",
			wantBody: map[string]any{"agent_id": "agent-X", "issue_id": "i-9"},
		},
		{
			toolName: "create_task",
			args:     map[string]any{"project_id": "p-1", "title": "Do it", "description": "d", "derived_from_issue": "i-2"},
			wantTool: "create_task",
			wantBody: map[string]any{"agent_id": "agent-X", "project_id": "p-1", "title": "Do it", "description": "d", "derived_from_issue": "i-2"},
		},
		{
			toolName: "assign_task",
			args:     map[string]any{"task_id": "t-1", "assignee": "agent:bob"},
			wantTool: "assign_task",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "assignee": "agent:bob"},
		},
		{
			toolName: "reassign_task",
			args:     map[string]any{"task_id": "t-1", "assignee": "user:carol"},
			wantTool: "reassign_task",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "assignee": "user:carol"},
		},
		{
			toolName: "subscribe",
			args:     map[string]any{"task_id": "t-1", "identity": "agent:bob"},
			wantTool: "subscribe",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "identity": "agent:bob"},
		},
		{
			toolName: "unsubscribe",
			args:     map[string]any{"task_id": "t-1"},
			wantTool: "unsubscribe",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "identity": ""},
		},
		{
			toolName: "heartbeat",
			args:     map[string]any{"task_id": "t-1"},
			wantTool: "heartbeat",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1"},
		},
		{
			toolName: "block_task",
			args:     map[string]any{"task_id": "t-1", "reason": "stuck", "reason_type": "obstacle"},
			wantTool: "block_task",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "reason": "stuck", "reason_type": "obstacle"},
		},
		{
			toolName: "complete_task",
			args:     map[string]any{"task_id": "t-1", "summary": "done"},
			wantTool: "complete_task",
			wantBody: map[string]any{"agent_id": "agent-X", "task_id": "t-1", "summary": "done"},
		},
		{
			toolName: "report_manual_recovery_delivery",
			args: map[string]any{
				"task_id":     "t-1",
				"executor_id": "exec-dead",
				"worktree":    "/tmp/recovered",
				"reason":      "executor exhausted after task_non_delivery",
				"evidence":    "go test ./...: pass",
				"git": map[string]any{
					"branch":        "ac-exec/t-1/exec-dead",
					"head_sha":      "deadbeef",
					"pushed":        true,
					"probed":        true,
					"base_ref":      "origin/main",
					"base_known":    true,
					"ahead_of_base": 1,
				},
			},
			wantTool: "report_manual_recovery_delivery",
			wantBody: map[string]any{
				"agent_id":    "agent-X",
				"task_id":     "t-1",
				"executor_id": "exec-dead",
				"worktree":    "/tmp/recovered",
				"reason":      "executor exhausted after task_non_delivery",
				"evidence":    "go test ./...: pass",
			},
		},
		{
			toolName: "record_finding",
			args:     map[string]any{"plan_id": "pl-1", "task_id": "t-1", "kind": "fact", "content": "the bug is on the tuple path"},
			wantTool: "record_finding",
			wantBody: map[string]any{"agent_id": "agent-X", "plan_id": "pl-1", "task_id": "t-1", "kind": "fact", "content": "the bug is on the tuple path"},
		},
		{
			toolName: "list_findings",
			args:     map[string]any{"plan_id": "pl-1"},
			wantTool: "list_findings",
			wantBody: map[string]any{"agent_id": "agent-X", "plan_id": "pl-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.toolName, func(t *testing.T) {
			fake := &fakeAdmin{canned: json.RawMessage(`{"ok":true}`)}
			cs := connect(t, Config{AgentID: "agent-X", Admin: fake})

			callOK(t, cs, tc.toolName, tc.args)

			if fake.gotTool != tc.wantTool {
				t.Errorf("forwarded tool = %q, want %q", fake.gotTool, tc.wantTool)
			}
			for k, want := range tc.wantBody {
				if got := fake.gotBody[k]; got != want {
					t.Errorf("body[%q] = %v, want %v (full body %v)", k, got, want, fake.gotBody)
				}
			}
		})
	}
}

func TestPlanToolsFreezePlanRulesPerMCPSession(t *testing.T) {
	fake := &fakeAdmin{cannedByTool: map[string]json.RawMessage{
		"get_team_rule_index":    json.RawMessage(`{"team_id":"team-1","phase":"plan","commit":"c1","rules":[{"slug":"plan-dag","description":"plan shape","body_bytes":10,"applies_to":["plan"],"source_path":"rules/plan-dag.md"}]}`),
		"create_plan":            json.RawMessage(`{"plan_id":"plan-1"}`),
		"edit_plan_topology":     json.RawMessage(`{"ok":true,"version":2,"dispatched":[]}`),
		"evolve_plan_generation": json.RawMessage(`{"ok":true,"generation":{"id":"generation-1"},"dispatched":["task-c"]}`),
	}}
	cs := connect(t, Config{AgentID: "agent-1", Admin: fake, Generation: 7})

	callOK(t, cs, "create_plan", map[string]any{"project_id": "proj-1", "name": "Plan"})
	callOK(t, cs, "edit_plan_topology", map[string]any{"plan_id": "plan-1", "base_version": 1, "ops": []any{}})
	callOK(t, cs, "evolve_plan_generation", map[string]any{
		"plan_id": "plan-1", "parent_generation_id": "generation-g0", "base_version": 2,
		"idempotency_key": "evo-1", "reason": "scope changed", "evidence": "review",
		"diff": map[string]any{
			"node_decisions": []any{map[string]any{"task_id": "task-a", "action": "preserve", "reason": "in flight"}},
			"tasks":          []any{map[string]any{"ref": "c", "title": "C", "assignee_ref": "agent:c", "detached": true}},
			"edges":          []any{},
		},
	})

	if got := len(fake.callsFor("get_team_rule_index")); got != 1 {
		t.Fatalf("get_team_rule_index calls = %d, want 1 frozen load per MCP session; calls=%+v", got, fake.calls)
	}
	createCalls := fake.callsFor("create_plan")
	editCalls := fake.callsFor("edit_plan_topology")
	evolveCalls := fake.callsFor("evolve_plan_generation")
	if len(createCalls) != 1 || len(editCalls) != 1 || len(evolveCalls) != 1 {
		t.Fatalf("plan tool calls create=%d edit=%d evolve=%d; calls=%+v", len(createCalls), len(editCalls), len(evolveCalls), fake.calls)
	}
	createRules, _ := createCalls[0].body["planning_rules"].(map[string]any)
	if createRules["commit"] != "c1" || createRules["phase"] != "plan" || createRules["source"] != "mcp_plan_tool" {
		t.Fatalf("create planning_rules metadata = %v", createRules)
	}
	if createRules["planning_generation"] != float64(7) ||
		createRules["planning_session_id"] != "agent:agent-1/generation:7" ||
		!strings.Contains(createRules["refresh_semantics"].(string), "once per MCP planning session") {
		t.Fatalf("create planning_rules freeze boundary = %v", createRules)
	}
	rules, _ := createRules["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("create planning_rules rules = %v", createRules["rules"])
	}
	rule, _ := rules[0].(map[string]any)
	if rule["slug"] != "plan-dag" || rule["body"] != nil || rule["body_bytes"] != float64(10) {
		t.Fatalf("create planning_rules rule = %v", rule)
	}
	editRules, _ := editCalls[0].body["planning_rules"].(map[string]any)
	if editRules["commit"] != "c1" || editRules["planning_generation"] != float64(7) {
		t.Fatalf("edit planning_rules should reuse frozen c1 generation 7 snapshot, got %v", editRules)
	}
	evolveRules, _ := evolveCalls[0].body["planning_rules"].(map[string]any)
	if evolveRules["commit"] != "c1" || evolveRules["planning_generation"] != float64(7) {
		t.Fatalf("evolve planning_rules should reuse frozen c1 generation 7 snapshot, got %v", evolveRules)
	}
	if evolveCalls[0].body["agent_id"] != "agent-1" || evolveCalls[0].body["idempotency_key"] != "evo-1" {
		t.Fatalf("evolve body missing fixed agent/idempotency: %v", evolveCalls[0].body)
	}

	fake2 := &fakeAdmin{cannedByTool: map[string]json.RawMessage{
		"get_team_rule_index": json.RawMessage(`{"team_id":"team-1","phase":"plan","commit":"c2","rules":[]}`),
		"create_plan":         json.RawMessage(`{"plan_id":"plan-2"}`),
	}}
	cs2 := connect(t, Config{AgentID: "agent-1", Admin: fake2, Generation: 8})
	callOK(t, cs2, "create_plan", map[string]any{"project_id": "proj-1", "name": "Plan 2"})
	if got := len(fake2.callsFor("get_team_rule_index")); got != 1 {
		t.Fatalf("new generation get_team_rule_index calls = %d, want 1", got)
	}
	createRules2, _ := fake2.callsFor("create_plan")[0].body["planning_rules"].(map[string]any)
	if createRules2["commit"] != "c2" || createRules2["planning_generation"] != float64(8) {
		t.Fatalf("new MCP session should reload c2 generation 8 snapshot, got %v", createRules2)
	}
}

func TestPlanRuleFreezeSurvivesSearchToolsReregister(t *testing.T) {
	fake := &fakeAdmin{cannedByTool: map[string]json.RawMessage{
		"get_team_rule_index": json.RawMessage(`{"team_id":"team-1","phase":"plan","commit":"c1","rules":[]}`),
		"create_plan":         json.RawMessage(`{"plan_id":"plan-1"}`),
		"edit_plan_topology":  json.RawMessage(`{"ok":true,"version":2,"dispatched":[]}`),
	}}
	cs := connect(t, Config{AgentID: "agent-1", Admin: fake, Generation: 3, TierTools: true})

	callOK(t, cs, "search_tools", map[string]any{"query": "plan"})
	callOK(t, cs, "create_plan", map[string]any{"project_id": "proj-1", "name": "Plan"})
	callOK(t, cs, "search_tools", map[string]any{"query": "plan"})
	callOK(t, cs, "edit_plan_topology", map[string]any{"plan_id": "plan-1", "base_version": 1, "ops": []any{}})

	if got := len(fake.callsFor("get_team_rule_index")); got != 1 {
		t.Fatalf("get_team_rule_index calls after search_tools re-register = %d, want 1; calls=%+v", got, fake.calls)
	}
	editRules, _ := fake.callsFor("edit_plan_topology")[0].body["planning_rules"].(map[string]any)
	if editRules["commit"] != "c1" || editRules["planning_generation"] != float64(3) {
		t.Fatalf("edit planning_rules = %v", editRules)
	}
}

func TestGetTeamRuleDefaultsPlanningSessionAuditContext(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"slug":"plan-dag","commit":"c1","body":"read me"}`)}
	cs := connect(t, Config{AgentID: "agent-1", Admin: fake, Generation: 7})

	callOK(t, cs, "get_team_rule", map[string]any{"slug": "plan-dag", "commit": "c1"})

	if fake.gotTool != "get_team_rule" {
		t.Fatalf("tool = %q, want get_team_rule", fake.gotTool)
	}
	if fake.gotBody["agent_id"] != "agent-1" ||
		fake.gotBody["slug"] != "plan-dag" ||
		fake.gotBody["commit"] != "c1" ||
		fake.gotBody["planning_session_id"] != "agent:agent-1/generation:7" {
		t.Fatalf("forwarded body = %v", fake.gotBody)
	}
	if _, leaked := fake.gotBody["execution_id"]; leaked {
		t.Fatalf("planning get_team_rule must not invent execution_id: %v", fake.gotBody)
	}
}

// TestCreateTaskAgentIDNotSpoofable proves the process-fixed agent_id wins on a
// NEW tool too: a smuggled agent_id arg is either rejected by the schema or
// ignored — the forwarded body always carries cfg.AgentID.
func TestCreateTaskAgentIDNotSpoofable(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"task_id":"t-9"}`)}
	cs := connect(t, Config{AgentID: "agent-real", Admin: fake})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"project_id": "p-1",
			"title":      "x",
			"agent_id":   "agent-EVIL", // attempt to impersonate
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		// Schema rejected the unknown property before the handler ran.
		if fake.gotTool != "" {
			t.Fatalf("admin seam called despite schema rejection: tool=%q body=%v", fake.gotTool, fake.gotBody)
		}
		return
	}
	if got := fake.gotBody["agent_id"]; got != "agent-real" {
		t.Fatalf("agent_id spoofable! forwarded = %v, want agent-real", got)
	}
}

// =============================================================================
// File tools
// =============================================================================

// fakeFileMover records the last Upload/Download invocation and returns canned
// results (or a canned error). Stands in for the daemon FileTransferClient.
type fakeFileMover struct {
	// recorded upload inputs
	upRoot, upAgentID, upPath, upScope, upScopeID string
	upURI                                         string
	upErr                                         error
	// recorded download inputs
	dlRoot, dlAgentID, dlFile, dlDest string
	dlErr                             error
}

func (f *fakeFileMover) UploadFile(_ context.Context, agentRoot, agentID, localPath, scope, scopeID string) (string, error) {
	f.upRoot, f.upAgentID, f.upPath, f.upScope, f.upScopeID = agentRoot, agentID, localPath, scope, scopeID
	if f.upErr != nil {
		return "", f.upErr
	}
	return f.upURI, nil
}

func (f *fakeFileMover) DownloadFile(_ context.Context, agentRoot, agentID, ulidOrURI, destPath string) error {
	f.dlRoot, f.dlAgentID, f.dlFile, f.dlDest = agentRoot, agentID, ulidOrURI, destPath
	return f.dlErr
}

func TestUploadFile(t *testing.T) {
	fm := &fakeFileMover{upURI: "ac://files/01H"}
	cs := connect(t, Config{AgentID: "agent-7", AgentRoot: "/ws/root", Admin: &fakeAdmin{}, Files: fm})

	res := callOK(t, cs, "upload_file", map[string]any{
		"path":     "out/report.txt",
		"scope":    "task",
		"scope_id": "t-1",
	})

	// agentRoot + agentID come from cfg, never from args.
	if fm.upRoot != "/ws/root" || fm.upAgentID != "agent-7" {
		t.Errorf("upload root/agent = %q/%q, want /ws/root/agent-7", fm.upRoot, fm.upAgentID)
	}
	if fm.upPath != "out/report.txt" || fm.upScope != "task" || fm.upScopeID != "t-1" {
		t.Errorf("upload args = %q,%q,%q", fm.upPath, fm.upScope, fm.upScopeID)
	}
	var out struct {
		FileURI string `json:"file_uri"`
	}
	if err := json.Unmarshal([]byte(textContent(t, res)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.FileURI != "ac://files/01H" {
		t.Errorf("file_uri = %q, want ac://files/01H", out.FileURI)
	}
}

func TestDownloadFile(t *testing.T) {
	fm := &fakeFileMover{}
	cs := connect(t, Config{AgentID: "agent-9", AgentRoot: "/ws/r", Admin: &fakeAdmin{}, Files: fm})

	res := callOK(t, cs, "download_file", map[string]any{
		"file":      "ac://files/01H",
		"dest_path": "in/data.bin",
	})

	if fm.dlRoot != "/ws/r" || fm.dlAgentID != "agent-9" {
		t.Errorf("download root/agent = %q/%q, want /ws/r/agent-9", fm.dlRoot, fm.dlAgentID)
	}
	if fm.dlFile != "ac://files/01H" || fm.dlDest != "in/data.bin" {
		t.Errorf("download args = %q,%q", fm.dlFile, fm.dlDest)
	}
	var out struct {
		OK   bool   `json:"ok"`
		Dest string `json:"dest"`
	}
	if err := json.Unmarshal([]byte(textContent(t, res)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !out.OK || out.Dest != "in/data.bin" {
		t.Errorf("result = %+v, want ok + dest=in/data.bin", out)
	}
}

// TestFileMoverErrorBecomesIsError proves a FileMover error (e.g. a simulated
// path escape) surfaces as an IsError result carrying the message, not a
// protocol error.
func TestFileMoverErrorBecomesIsError(t *testing.T) {
	fm := &fakeFileMover{upErr: errors.New("file_transfer: path escapes workspace root: \"/etc/passwd\"")}
	cs := connect(t, Config{AgentID: "agent-1", AgentRoot: "/ws", Admin: &fakeAdmin{}, Files: fm})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "upload_file",
		Arguments: map[string]any{"path": "../../etc/passwd"},
	})
	if err != nil {
		t.Fatalf("call returned protocol error, want IsError result: %v", err)
	}
	if !res.IsError {
		t.Fatalf("want IsError=true on file-mover error")
	}
	if got := textContent(t, res); got == "" {
		t.Errorf("IsError result missing the error message")
	}
}

// TestAttachFileForwarding proves attach_file forwards {agent_id, file_uri,
// scope, scope_id} to the admin attach_file endpoint, agent_id from cfg.
func TestAttachFileForwarding(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"reference_id":"r-1"}`)}
	cs := connect(t, Config{AgentID: "agent-2", Admin: fake})

	callOK(t, cs, "attach_file", map[string]any{
		"file_uri": "ac://files/01H",
		"scope":    "conversation",
		"scope_id": "c-7",
	})

	if fake.gotTool != "attach_file" {
		t.Errorf("forwarded tool = %q, want attach_file", fake.gotTool)
	}
	want := map[string]any{"agent_id": "agent-2", "file_uri": "ac://files/01H", "scope": "conversation", "scope_id": "c-7"}
	for k, v := range want {
		if got := fake.gotBody[k]; got != v {
			t.Errorf("body[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestListFilesForwarding(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"files":[]}`)}
	cs := connect(t, Config{AgentID: "agent-2", Admin: fake})
	callOK(t, cs, "list_files", map[string]any{"scope": "task", "scope_id": "t-7"})
	if fake.gotTool != "list_files" {
		t.Fatalf("forwarded tool=%q want list_files", fake.gotTool)
	}
	want := map[string]any{"agent_id": "agent-2", "scope": "task", "scope_id": "t-7"}
	for k, v := range want {
		if fake.gotBody[k] != v {
			t.Errorf("body[%q]=%v want %v", k, fake.gotBody[k], v)
		}
	}
}

func TestGenerateMCPConfig(t *testing.T) {
	raw, err := GenerateMCPConfig(MCPConfigParams{
		ServerName:        "agent-center",
		Command:           "/usr/bin/agent-center",
		Args:              []string{"worker", "mcp-host"},
		AgentID:           "agent-42",
		AdminURL:          "tcp://127.0.0.1:9000",
		WorkerToken:       "tok-abc",
		ServerFingerprint: "SHA256:deadbeef",
		AgentRoot:         "/ws/agent-42",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Must be valid JSON in the {mcpServers:{<name>:{command,args,env}}} shape.
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("produced invalid JSON: %v\n%s", err, raw)
	}
	srv, ok := doc.MCPServers["agent-center"]
	if !ok {
		t.Fatalf("mcpServers missing entry %q (have %v)", "agent-center", doc.MCPServers)
	}
	if srv.Command != "/usr/bin/agent-center" {
		t.Errorf("command = %q", srv.Command)
	}
	if len(srv.Args) != 2 || srv.Args[0] != "worker" || srv.Args[1] != "mcp-host" {
		t.Errorf("args = %v, want [worker mcp-host]", srv.Args)
	}
	wantEnv := map[string]string{
		"AC_MCP_AGENT_ID":           "agent-42",
		"AC_MCP_ADMIN_URL":          "tcp://127.0.0.1:9000",
		"AC_MCP_WORKER_TOKEN":       "tok-abc",
		"AC_MCP_SERVER_FINGERPRINT": "SHA256:deadbeef",
		"AC_MCP_AGENT_ROOT":         "/ws/agent-42",
	}
	for k, v := range wantEnv {
		if got := srv.Env[k]; got != v {
			t.Errorf("env[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestGenerateMCPConfig_DisableToolTiering(t *testing.T) {
	raw, err := GenerateMCPConfig(MCPConfigParams{
		ServerName:         "agent-center",
		Command:            "/usr/bin/agent-center",
		Args:               []string{"worker", "mcp-host"},
		AgentID:            "agent-42",
		DisableToolTiering: true,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var doc MCPConfig
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("produced invalid JSON: %v\n%s", err, raw)
	}
	if got := doc.MCPServers["agent-center"].Env["AC_MCP_TIER_TOOLS"]; got != "false" {
		t.Fatalf("AC_MCP_TIER_TOOLS = %q, want false", got)
	}
}

func TestRequireTools_TieredCatalogIncludesSupervisorCore(t *testing.T) {
	if err := RequireTools(context.Background(), Config{AgentID: "agent-x", TierTools: true}, "post_message", "list_my_tasks", "search_tools"); err != nil {
		t.Fatalf("RequireTools tiered supervisor core: %v", err)
	}
}
