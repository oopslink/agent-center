package claudecode

import (
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

func TestAdapter_NameAndSession(t *testing.T) {
	a := New("")
	if a.Name() != AdapterName {
		t.Fatalf("name: %s", a.Name())
	}
	if !a.SupportsSession() {
		t.Fatal("expected supports session")
	}
}

func TestAdapter_BuildCommand_Order(t *testing.T) {
	a := New("/usr/local/bin/claude")
	spec, err := a.BuildCommand(agentadapter.SpawnRequest{
		ExecutionID: "E-1",
		Prompt:      "hello",
		SkillFiles:  []string{"/skills/x.md", "/skills/y.md"},
		Env:         map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Binary != "/usr/local/bin/claude" {
		t.Fatalf("binary: %s", spec.Binary)
	}
	wantArgs := []string{
		"--output-format", "stream-json",
		"--session-id", "E-1",
		"--skill", "/skills/x.md",
		"--skill", "/skills/y.md",
		"-p", "hello",
	}
	if len(spec.Args) != len(wantArgs) {
		t.Fatalf("args len: %d want %d (%+v)", len(spec.Args), len(wantArgs), spec.Args)
	}
	for i := range wantArgs {
		if spec.Args[i] != wantArgs[i] {
			t.Fatalf("args[%d]: got %q want %q", i, spec.Args[i], wantArgs[i])
		}
	}
	found := false
	for _, e := range spec.Env {
		if e == "FOO=bar" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected FOO=bar in env")
	}
}

func TestAdapter_BuildCommand_Validation(t *testing.T) {
	a := New("")
	if _, err := a.BuildCommand(agentadapter.SpawnRequest{Prompt: "x"}); err == nil {
		t.Fatal("expected execution_id error")
	}
	if _, err := a.BuildCommand(agentadapter.SpawnRequest{ExecutionID: "E"}); err == nil {
		t.Fatal("expected prompt error")
	}
}

func TestAdapter_ParseEvent_5KnownPlusUnknown(t *testing.T) {
	a := New("")
	cases := []struct {
		name string
		line string
		want agentadapter.EventType
		ck   func(*testing.T, agentadapter.AgentTraceEvent)
	}{
		{"thinking", `{"type":"thinking","text":"let me see"}`, agentadapter.EventThinking,
			func(t *testing.T, e agentadapter.AgentTraceEvent) {
				if e.Thinking != "let me see" {
					t.Fatalf("thinking: %s", e.Thinking)
				}
			}},
		{"tool_use", `{"type":"tool_use","name":"Bash","input":{"cmd":"ls"}}`, agentadapter.EventToolCall,
			func(t *testing.T, e agentadapter.AgentTraceEvent) {
				if e.ToolName != "Bash" {
					t.Fatalf("tool_name: %s", e.ToolName)
				}
				if string(e.ToolInput) != `{"cmd":"ls"}` {
					t.Fatalf("tool_input: %s", e.ToolInput)
				}
			}},
		{"tool_result", `{"type":"tool_result","tool_use_id":"x","content":"hello"}`, agentadapter.EventToolResult,
			func(t *testing.T, e agentadapter.AgentTraceEvent) {
				if string(e.ToolOutput) != `"hello"` {
					t.Fatalf("tool_output: %s", e.ToolOutput)
				}
			}},
		{"usage", `{"type":"usage","input_tokens":100,"output_tokens":50}`, agentadapter.EventTokensReport,
			func(t *testing.T, e agentadapter.AgentTraceEvent) {
				if e.TokensIn != 100 || e.TokensOut != 50 {
					t.Fatalf("tokens: %d/%d", e.TokensIn, e.TokensOut)
				}
			}},
		{"end_turn", `{"type":"end_turn"}`, agentadapter.EventTurnEnd, nil},
		{"error", `{"type":"error","message":"x"}`, agentadapter.EventError, nil},
		{"unknown", `{"type":"future_thing","x":1}`, agentadapter.EventUnknown,
			func(t *testing.T, e agentadapter.AgentTraceEvent) {
				if e.CliType != "future_thing" {
					t.Fatalf("cli_type: %s", e.CliType)
				}
				if len(e.Raw) == 0 {
					t.Fatal("raw should be preserved")
				}
			}},
		{"tool_use_missing_name_degraded", `{"type":"tool_use","input":{}}`, agentadapter.EventUnknown, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, err := a.ParseEvent([]byte(c.line))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if ev.Type != c.want {
				t.Fatalf("type: %s want %s", ev.Type, c.want)
			}
			if c.ck != nil {
				c.ck(t, ev)
			}
		})
	}
}

func TestAdapter_ParseEvent_MalformedJSON(t *testing.T) {
	a := New("")
	if _, err := a.ParseEvent([]byte("")); !errors.Is(err, agentadapter.ErrInvalidEventJSON) {
		t.Fatal("expected invalid")
	}
	if _, err := a.ParseEvent([]byte("not-json")); !errors.Is(err, agentadapter.ErrInvalidEventJSON) {
		t.Fatal("expected invalid")
	}
	if _, err := a.ParseEvent([]byte(`{"type":"thinking","text":12345}`)); !errors.Is(err, agentadapter.ErrInvalidEventJSON) {
		t.Fatal("expected unmarshal sub-shape error")
	}
}

func TestAdapter_AutoRegistered(t *testing.T) {
	a, ok := agentadapter.Get(AdapterName)
	if !ok {
		t.Fatal("expected auto-register")
	}
	if a.Name() != AdapterName {
		t.Fatalf("name: %s", a.Name())
	}
}
