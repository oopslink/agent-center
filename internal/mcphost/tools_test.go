package mcphost

import (
	"context"
	"encoding/json"
	"errors"
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
