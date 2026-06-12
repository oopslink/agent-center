package mcphost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeAdmin records the last CallAgentTool invocation and returns canned
// JSON (or a canned error). It stands in for the real AdminClient transport
// so the tests exercise the real mcp.Server end-to-end over an in-memory
// transport without a center.
type fakeAdmin struct {
	gotTool string
	gotBody map[string]any
	canned  json.RawMessage
	err     error
}

func (f *fakeAdmin) CallAgentTool(_ context.Context, tool string, body any, out *json.RawMessage) error {
	f.gotTool = tool
	// Normalise body to a map[string]any for assertions regardless of the
	// concrete type the handler passed.
	raw, _ := json.Marshal(body)
	f.gotBody = map[string]any{}
	_ = json.Unmarshal(raw, &f.gotBody)
	if f.err != nil {
		return f.err
	}
	if out != nil {
		*out = append((*out)[:0], f.canned...)
	}
	return nil
}

// connect wires a real mcp.Server (built by NewServer) to an in-process MCP
// client over the SDK's in-memory transport pair, and returns the connected
// client session. The server MUST connect before the client (the client
// initializes the session on connect).
func connect(t *testing.T, cfg Config) *mcp.ClientSession {
	t.Helper()
	srv := NewServer(cfg)
	serverT, clientT := mcp.NewInMemoryTransports()

	ctx := context.Background()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// textContent extracts the single TextContent text from a CallToolResult,
// failing the test if the shape is unexpected.
func textContent(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("want exactly 1 content block, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("want *TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

// wantTools is the FULL b3-ii tool set: the 2 locked b3-i tools + the
// remaining OQ4 JSON tools + the 3 file tools. Each MCP tool name equals its
// admin /admin/agent-tools/<tool> route segment (download_file is the only
// one whose admin route differs — GET /admin/files/{ulid} — because it moves
// bytes through the FileMover, not callAdmin).
var wantTools = []string{
	// b3-i (locked)
	"get_my_work", "post_task_message",
	// v2.8.1 #278 D pull model: agent drives its own work-item queue
	"start_work", "fail_work",
	// v2.8.1 #278 PR4 scheduling autonomy
	"pause_work", "resume_paused_work",
	// v2.8.1 #278 PR4 read tools (loop-boundary active + paused candidates)
	"get_my_active_work", "list_my_paused_work",
	// v2.8.1 #278 PR4b dual-stream: the agent's unread messages (DM + @mention) + mark-seen
	"get_my_unread", "mark_seen",
	// v2.7 #185: DM/channel reply
	"post_message",
	// v2.7.1 #239: self / org discovery
	"get_my_profile", "find_org_agent",
	// v2.7.1 #246: channel name → id discovery
	"find_org_channel",
	// reads
	"get_task", "get_issue",
	// pm writes / passthrough
	"create_task", "assign_task", "reassign_task",
	"subscribe", "unsubscribe", "request_input",
	"block_task", "complete_task", "verify_task",
	// v2.9 P3 Stage C (#285): plan orchestration tools (see planTools)
	"create_plan", "add_task_to_plan", "remove_task_from_plan",
	"add_plan_dependency", "remove_plan_dependency",
	"start_plan", "stop_plan", "get_plan", "list_plans",
	"delete_plan", "archive_plan",
	// files
	"upload_file", "download_file", "attach_file",
}

// planTools is the v2.9 P3 Stage C (#285) plan agent-tool catalog: every plan
// admin handler in internal/admin/api/agent_tools_plans.go MUST have a matching
// per-agent MCP tool registered in NewServer, or the agent LLM can't see/call
// it. TestPlanToolsRegistered (the #266-class integration-seam guard) asserts
// all of these are present.
var planTools = []string{
	"create_plan", "add_task_to_plan", "remove_task_from_plan",
	"add_plan_dependency", "remove_plan_dependency",
	"start_plan", "stop_plan", "get_plan", "list_plans",
	"delete_plan", "archive_plan",
}

func TestInitializeAndListTools(t *testing.T) {
	cs := connect(t, Config{AgentID: "agent-1", Admin: &fakeAdmin{}, Files: &fakeFileMover{}})

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	if len(byName) != len(wantTools) {
		t.Fatalf("want exactly %d tools, got %d: %v", len(wantTools), len(byName), keys(byName))
	}
	for _, want := range wantTools {
		tool, ok := byName[want]
		if !ok {
			t.Fatalf("missing tool %q (have %v)", want, keys(byName))
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", want)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", want)
		}
	}

	// post_task_message's input schema must expose task_id + text properties.
	schemaProps := inputSchemaProperties(t, byName["post_task_message"])
	for _, prop := range []string{"task_id", "text"} {
		if _, ok := schemaProps[prop]; !ok {
			t.Errorf("post_task_message schema missing property %q (have %v)", prop, keys2(schemaProps))
		}
	}
}

// TestPlanToolsRegistered is the #266-class integration-seam guard for the
// v2.9 P3 Stage C plan tools (#285). The seam it closes: the plan tools existed
// as admin HTTP handlers (internal/admin/api/agent_tools_plans.go) but were
// NEVER registered in the per-agent MCP tool catalog NewServer builds — so the
// agent LLM could not see or call them, breaking "PM-agent programmatically
// builds plans". This test enumerates the tools the live MCP server (via
// NewServer → ListTools, the SAME mechanism the existing tests use) actually
// exposes and asserts EVERY plan tool is present (plus the existing task tools
// stay present), so a future plan admin handler added without a matching
// mcphost registration fails CI here.
func TestPlanToolsRegistered(t *testing.T) {
	cs := connect(t, Config{AgentID: "agent-1", Admin: &fakeAdmin{}, Files: &fakeFileMover{}})

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	registered := map[string]bool{}
	for _, tool := range res.Tools {
		registered[tool.Name] = true
	}

	// Every plan tool must be in the agent-facing catalog.
	for _, name := range planTools {
		if !registered[name] {
			t.Errorf("plan tool %q is NOT registered in the per-agent MCP catalog (admin handler exists but agent LLM can't see it)", name)
		}
	}

	// Sanity: the existing task tools must still be present (the plan
	// additions did not displace them).
	for _, name := range []string{"create_task", "assign_task", "get_task", "complete_task"} {
		if !registered[name] {
			t.Errorf("existing tool %q went missing after the plan-tool additions", name)
		}
	}
}

// TestAgentFacingToolParity is the (3) full-parity guard (PD-approved, #266-class):
// it asserts the live per-agent MCP catalog (ListTools) EQUALS the source-of-truth
// AgentFacingToolNames set — BOTH directions. Forward: every canonical name is
// registered (a set member dropped from NewServer → FAIL). Reverse: every registered
// tool is in the canonical set (a tool added to NewServer without a deliberate
// AgentFacingToolNames entry → FAIL). This catches the WHOLE class of
// registration↔manifest drift (the #285/#299 seam where a handler exists but isn't
// agent-facing, or a tool is exposed without a deliberate decision) in EITHER
// direction — not just the specific families TestPlanToolsRegistered guards.
func TestAgentFacingToolParity(t *testing.T) {
	cs := connect(t, Config{AgentID: "agent-1", Admin: &fakeAdmin{}, Files: &fakeFileMover{}})

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	registered := map[string]bool{}
	for _, tool := range res.Tools {
		registered[tool.Name] = true
	}
	canonical := map[string]bool{}
	for _, name := range AgentFacingToolNames {
		canonical[name] = true
	}

	// Forward: every canonical agent-facing tool is actually registered.
	for _, name := range AgentFacingToolNames {
		if !registered[name] {
			t.Errorf("canonical agent-facing tool %q is NOT registered in NewServer (dropped from the catalog, or stale AgentFacingToolNames)", name)
		}
	}
	// Reverse: every registered tool is in the canonical set — no tool reaches the
	// agent LLM without a deliberate AgentFacingToolNames entry.
	for name := range registered {
		if !canonical[name] {
			t.Errorf("tool %q is registered in NewServer but NOT in AgentFacingToolNames — add it deliberately (is it meant to be agent-facing?)", name)
		}
	}
	// Size parity = fast signal the two sets diverged.
	if len(registered) != len(AgentFacingToolNames) {
		t.Errorf("registered tool count = %d, AgentFacingToolNames = %d (sets diverged)", len(registered), len(AgentFacingToolNames))
	}
	// FilesSeamTools (the reverse-lockstep exception markers) must be real
	// agent-facing tools.
	for _, name := range FilesSeamTools {
		if !canonical[name] {
			t.Errorf("FilesSeamTools entry %q is not in AgentFacingToolNames", name)
		}
	}
}

func TestCallGetMyWork(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"work_items":[{"id":"wi-1"}]}`)}
	cs := connect(t, Config{AgentID: "agent-42", Admin: fake})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_my_work"})
	if err != nil {
		t.Fatalf("call get_my_work: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError; content=%v", res.Content)
	}
	if fake.gotTool != "get_my_work" {
		t.Errorf("forwarded tool = %q, want get_my_work", fake.gotTool)
	}
	if got := fake.gotBody["agent_id"]; got != "agent-42" {
		t.Errorf("forwarded agent_id = %v, want agent-42", got)
	}
	if got := textContent(t, res); got != `{"work_items":[{"id":"wi-1"}]}` {
		t.Errorf("text content = %q, want canned JSON", got)
	}
}

func TestCallPostTaskMessage(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"posted":true}`)}
	cs := connect(t, Config{AgentID: "agent-7", Admin: fake})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "post_task_message",
		Arguments: map[string]any{"task_id": "task-9", "text": "hello"},
	})
	if err != nil {
		t.Fatalf("call post_task_message: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError; content=%v", res.Content)
	}
	if fake.gotTool != "post_task_message" {
		t.Errorf("forwarded tool = %q, want post_task_message", fake.gotTool)
	}
	if got := fake.gotBody["agent_id"]; got != "agent-7" {
		t.Errorf("forwarded agent_id = %v, want agent-7 (from cfg)", got)
	}
	if got := fake.gotBody["task_id"]; got != "task-9" {
		t.Errorf("forwarded task_id = %v, want task-9", got)
	}
	// The model-facing arg is "text" but the admin endpoint reads "content".
	if got := fake.gotBody["content"]; got != "hello" {
		t.Errorf("forwarded content = %v, want hello", got)
	}
	if _, ok := fake.gotBody["text"]; ok {
		t.Errorf("forwarded body must not carry raw \"text\" key (admin reads \"content\")")
	}
	if got := textContent(t, res); got != `{"posted":true}` {
		t.Errorf("text content = %q, want canned JSON", got)
	}
}

// TestAgentIDNotSpoofable proves the process-fixed agent_id wins: even when
// the model smuggles an agent_id field into the tool args, the forwarded
// body carries the configured agent_id, not the arg.
func TestAgentIDNotSpoofable(t *testing.T) {
	fake := &fakeAdmin{canned: json.RawMessage(`{"posted":true}`)}
	cs := connect(t, Config{AgentID: "agent-real", Admin: fake})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "post_task_message",
		Arguments: map[string]any{
			"task_id":  "task-1",
			"text":     "x",
			"agent_id": "agent-EVIL", // attempt to impersonate
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// Two acceptable outcomes, both prove non-spoofability:
	//   (a) the SDK rejects the unknown agent_id property (schema has no
	//       agent_id field) → IsError, the handler never runs, and the
	//       smuggled value never reaches the admin call;
	//   (b) the property is ignored and the handler runs, in which case the
	//       forwarded body MUST carry the configured agent_id, not the arg.
	if res.IsError {
		// (a): the spoofed arg was rejected before reaching the admin seam.
		if fake.gotTool != "" {
			t.Fatalf("admin seam was called despite schema rejection: tool=%q body=%v",
				fake.gotTool, fake.gotBody)
		}
		return
	}
	// (b): handler ran; the forwarded agent_id must be the configured one.
	if got := fake.gotBody["agent_id"]; got != "agent-real" {
		t.Fatalf("agent_id spoofable! forwarded = %v, want agent-real", got)
	}
}

// TestAdminErrorBecomesIsError proves a non-2xx admin response (typed
// *AdminToolError) surfaces to the model as a CallToolResult with IsError
// set and the body carried in the content, rather than a protocol error.
func TestAdminErrorBecomesIsError(t *testing.T) {
	fake := &fakeAdmin{err: &AdminToolError{Status: 403, Body: `{"error":"forbidden"}`}}
	cs := connect(t, Config{AgentID: "agent-1", Admin: fake})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_my_work"})
	if err != nil {
		t.Fatalf("call returned protocol error, want IsError result: %v", err)
	}
	if !res.IsError {
		t.Fatalf("want IsError=true on admin 403")
	}
	if got := textContent(t, res); got != `{"error":"forbidden"}` {
		t.Errorf("error body = %q, want forbidden body", got)
	}
}

// inputSchemaProperties decodes a tool's InputSchema (as delivered to the
// client — a JSON-marshalable value) and returns its "properties" map.
func inputSchemaProperties(t *testing.T, tool *mcp.Tool) map[string]any {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	return schema.Properties
}

func keys(m map[string]*mcp.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keys2(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
