package codex

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

func TestCodex_Identity(t *testing.T) {
	a := New("")
	if a.Name() != AdapterName {
		t.Fatalf("name: %s", a.Name())
	}
	// SupportsSession stays false until the runtime resume path (Stage 2/3) is
	// wired; the codex CLI *can* resume, but the runtime cannot drive it yet.
	if a.SupportsSession() {
		t.Fatal("expected SupportsSession=false (not runtime-wired)")
	}
}

func TestCodex_RegisteredInDefaultRegistry(t *testing.T) {
	// v2 per ADR-0030 § 3: codex self-registers on import so DispatchService
	// can target it; the v1 "must not auto-register" assertion is flipped.
	a, ok := agentadapter.Get(AdapterName)
	if !ok {
		t.Fatal("codex should be auto-registered (v2)")
	}
	if a.Name() != AdapterName {
		t.Fatalf("registered adapter name: %s", a.Name())
	}
}

func TestCodex_BuildCommand_Fresh(t *testing.T) {
	a := New("")
	spec, err := a.BuildCommand(agentadapter.SpawnRequest{
		ExecutionID: "exec-1",
		Prompt:      "do the thing",
		WorkingDir:  "/work/dir",
		Env:         map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Binary != "codex" {
		t.Fatalf("binary: %s", spec.Binary)
	}
	// Core non-interactive flags.
	for _, want := range []string{"exec", "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"} {
		if !slices.Contains(spec.Args, want) {
			t.Fatalf("args missing %q: %v", want, spec.Args)
		}
	}
	// Working dir threaded via -C.
	if i := slices.Index(spec.Args, "-C"); i < 0 || i+1 >= len(spec.Args) || spec.Args[i+1] != "/work/dir" {
		t.Fatalf("expected -C /work/dir: %v", spec.Args)
	}
	// Prompt is the final positional arg.
	if spec.Args[len(spec.Args)-1] != "do the thing" {
		t.Fatalf("prompt should be last positional: %v", spec.Args)
	}
	// Env merged.
	if !slices.Contains(spec.Env, "FOO=bar") {
		t.Fatalf("env missing FOO=bar")
	}
}

func TestCodex_BuildCommand_OmitsCWhenNoWorkingDir(t *testing.T) {
	a := New("")
	spec, err := a.BuildCommand(agentadapter.SpawnRequest{ExecutionID: "e", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(spec.Args, "-C") {
		t.Fatalf("did not expect -C without WorkingDir: %v", spec.Args)
	}
}

func TestCodex_BuildCommand_SystemPromptPrepended(t *testing.T) {
	a := New("")
	spec, err := a.BuildCommand(agentadapter.SpawnRequest{
		ExecutionID:  "e",
		Prompt:       "user turn",
		SystemPrompt: "be terse",
	})
	if err != nil {
		t.Fatal(err)
	}
	last := spec.Args[len(spec.Args)-1]
	if !strings.HasPrefix(last, "be terse") || !strings.Contains(last, "user turn") {
		t.Fatalf("system prompt not prepended to final prompt arg: %q", last)
	}
}

func TestCodex_BuildCommand_Validation(t *testing.T) {
	a := New("")
	if _, err := a.BuildCommand(agentadapter.SpawnRequest{Prompt: "p"}); err == nil {
		t.Fatal("expected error for empty execution_id")
	}
	if _, err := a.BuildCommand(agentadapter.SpawnRequest{ExecutionID: "e"}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestCodex_ParseEvent_TurnCompleted_TokensAndEnd(t *testing.T) {
	line := []byte(`{"type":"turn.completed","usage":{"input_tokens":17717,"cached_input_tokens":4992,"output_tokens":6,"reasoning_output_tokens":0}}`)
	ev, err := New("").ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != agentadapter.EventTurnEnd {
		t.Fatalf("type: %s", ev.Type)
	}
	if ev.TokensIn != 17717 || ev.TokensOut != 6 {
		t.Fatalf("tokens in=%d out=%d", ev.TokensIn, ev.TokensOut)
	}
	if ev.CliType != "turn.completed" {
		t.Fatalf("cli_type: %s", ev.CliType)
	}
}

func TestCodex_ParseEvent_CommandExecution_Started_ToolCall(t *testing.T) {
	line := []byte(`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/usr/bin/zsh -lc ls","aggregated_output":"","exit_code":null,"status":"in_progress"}}`)
	ev, err := New("").ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != agentadapter.EventToolCall {
		t.Fatalf("type: %s", ev.Type)
	}
	if ev.ToolName != "shell" {
		t.Fatalf("tool name: %s", ev.ToolName)
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(ev.ToolInput, &in); err != nil {
		t.Fatalf("tool input not valid JSON: %v", err)
	}
	if in.Command != "/usr/bin/zsh -lc ls" {
		t.Fatalf("command: %s", in.Command)
	}
}

func TestCodex_ParseEvent_CommandExecution_Completed_ToolResult(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/usr/bin/zsh -lc ls","aggregated_output":"a.txt\n","exit_code":0,"status":"completed"}}`)
	ev, err := New("").ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != agentadapter.EventToolResult {
		t.Fatalf("type: %s", ev.Type)
	}
	var out string
	if err := json.Unmarshal(ev.ToolOutput, &out); err != nil {
		t.Fatalf("tool output not valid JSON string: %v", err)
	}
	if out != "a.txt\n" {
		t.Fatalf("output: %q", out)
	}
}

func TestCodex_ParseEvent_Reasoning_Thinking(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"id":"item_1","type":"reasoning","text":"let me think"}}`)
	ev, err := New("").ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != agentadapter.EventThinking {
		t.Fatalf("type: %s", ev.Type)
	}
	if ev.Thinking != "let me think" {
		t.Fatalf("thinking: %s", ev.Thinking)
	}
}

func TestCodex_ParseEvent_AgentMessage_Unknown(t *testing.T) {
	// AgentTraceEvent has no assistant-text slot (same gap as the claude adapter);
	// surface as Unknown with Raw preserved.
	line := []byte(`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"PONG"}}`)
	ev, err := New("").ParseEvent(line)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != agentadapter.EventUnknown {
		t.Fatalf("type: %s", ev.Type)
	}
	if len(ev.Raw) == 0 {
		t.Fatal("raw should be preserved")
	}
}

func TestCodex_ParseEvent_ThreadAndTurnStarted_Unknown(t *testing.T) {
	for _, line := range [][]byte{
		[]byte(`{"type":"thread.started","thread_id":"019ecc1b-81f2-7fd1-8797-aa71a6c85085"}`),
		[]byte(`{"type":"turn.started"}`),
	} {
		ev, err := New("").ParseEvent(line)
		if err != nil {
			t.Fatalf("line %s: %v", line, err)
		}
		if ev.Type != agentadapter.EventUnknown {
			t.Fatalf("line %s: type %s", line, ev.Type)
		}
	}
}

func TestCodex_ParseEvent_Error(t *testing.T) {
	for _, line := range [][]byte{
		[]byte(`{"type":"turn.failed","error":{"message":"boom"}}`),
		[]byte(`{"type":"error","message":"boom"}`),
	} {
		ev, err := New("").ParseEvent(line)
		if err != nil {
			t.Fatalf("line %s: %v", line, err)
		}
		if ev.Type != agentadapter.EventError {
			t.Fatalf("line %s: type %s", line, ev.Type)
		}
	}
}

func TestCodex_ParseEvent_Malformed(t *testing.T) {
	if _, err := New("").ParseEvent(nil); !errors.Is(err, agentadapter.ErrInvalidEventJSON) {
		t.Fatalf("empty: want ErrInvalidEventJSON, got %v", err)
	}
	if _, err := New("").ParseEvent([]byte(`{not json`)); !errors.Is(err, agentadapter.ErrInvalidEventJSON) {
		t.Fatalf("bad json: want ErrInvalidEventJSON, got %v", err)
	}
}
