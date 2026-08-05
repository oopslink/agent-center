package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

func asstToolID(id, name, inputJSON string) string {
	return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + inputJSON + `}]}}`
}

func toolResultID(id, content string, isError bool) string {
	b, _ := json.Marshal(content)
	return fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q,"content":%s,"is_error":%t}]}}`, id, b, isError)
}

func TestRunExecutor_CommandEventsCaptureClaudeExitStatuses(t *testing.T) {
	lines := []string{
		asstToolID("toolu-read", "Read", `{"file_path":"README.md"}`),
		toolResultID("toolu-read", "contents", false),
		asstToolID("toolu-ok", "Bash", `{"command":"go test ./..."}`),
		toolResultID("toolu-ok", "ok\n", false),
		asstToolID("toolu-red", "Bash", `{"command":"go test -race ./..."}`),
		toolResultID("toolu-red", "tests failed\nExit code: 1\n", true),
	}

	fx, id, root := newProgressFixture(t)
	err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: id,
		Runner:     &scriptedRunner{lines: lines},
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err != nil {
		t.Fatalf("RunExecutor: %v", err)
	}

	events, _, err := fx.ReadCommandEvents(id)
	if err != nil {
		t.Fatalf("ReadCommandEvents: %v", err)
	}
	commands, ok, reason := evidenceCommandsFromEvents(events)
	if !ok {
		t.Fatalf("commands unavailable: %s events=%+v", reason, events)
	}
	if len(commands) != 2 {
		t.Fatalf("commands=%+v, want exactly 2 shell commands (non-shell result ignored)", commands)
	}
	if commands[0].Command != "go test ./..." || commands[0].ExitStatus != 0 {
		t.Fatalf("first command = %+v, want go test exit 0", commands[0])
	}
	if commands[1].Command != "go test -race ./..." || commands[1].ExitStatus != 1 {
		t.Fatalf("second command = %+v, want race test exit 1", commands[1])
	}
}

type codexScriptedRunner struct{ lines []string }

func (s *codexScriptedRunner) Run(ctx context.Context, rc RunContext) (RunResult, error) {
	cr := &CommandRunner{
		cmd: []string{"codex", "exec", "--json", "prompt"},
		run: func(_ context.Context, _ string, _ []string, onLine func(string)) (string, error) {
			for _, l := range s.lines {
				onLine(l)
			}
			return `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`, nil
		},
	}
	return cr.Run(ctx, rc)
}

func TestRunExecutor_CommandEventsCaptureCodexExitStatus(t *testing.T) {
	lines := []string{
		`{"type":"item.started","item":{"id":"cmd-1","type":"command_execution","command":"go test ./...","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"cmd-1","type":"command_execution","command":"go test ./...","aggregated_output":"ok\n","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`,
	}

	fx, id, root := newProgressFixture(t)
	err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: id,
		Runner:     &codexScriptedRunner{lines: lines},
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err != nil {
		t.Fatalf("RunExecutor: %v", err)
	}

	events, _, err := fx.ReadCommandEvents(id)
	if err != nil {
		t.Fatalf("ReadCommandEvents: %v", err)
	}
	commands, ok, reason := evidenceCommandsFromEvents(events)
	if !ok {
		t.Fatalf("commands unavailable: %s events=%+v", reason, events)
	}
	if len(commands) != 1 || commands[0].Command != "go test ./..." || commands[0].ExitStatus != 0 {
		t.Fatalf("codex commands=%+v, want go test exit 0", commands)
	}
}
