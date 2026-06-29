package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// runHarness wires a temp agent root + an executor whose input.json is staged,
// ready for RunExecutor to consume. Returns the FileExchange for assertions.
func runHarness(t *testing.T, id string) (*FileExchange, string) {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := fx.WriteInput(validPoolInput(id)); err != nil {
		t.Fatalf("WriteInput: %v", err)
	}
	return fx, root
}

// fakeComputeRunner records the RunContext it saw and returns a canned result/err.
type fakeComputeRunner struct {
	res      RunResult
	err      error
	gotWS    string
	gotModel string
	progress int
}

func (r *fakeComputeRunner) Run(_ context.Context, rc RunContext) (RunResult, error) {
	r.gotWS = rc.WorkspaceDir
	r.gotModel = rc.Input.Model
	rc.Progress("phase1", "working")
	r.progress++
	if r.err != nil {
		return RunResult{}, r.err
	}
	return r.res, nil
}

func TestRunExecutor_SuccessWritesOutputAndDoneStatus(t *testing.T) {
	fx, root := runHarness(t, "exec-ok")
	fr := &fakeComputeRunner{res: RunResult{Result: "all good", Summary: "ok"}}
	err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: "exec-ok",
		Runner:     fr,
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err != nil {
		t.Fatalf("RunExecutor success path returned err: %v", err)
	}
	out, err := fx.ReadOutput("exec-ok")
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	if !out.Success || out.Result != "all good" {
		t.Errorf("output = %+v, want success result 'all good'", out)
	}
	st, err := fx.ReadStatus("exec-ok")
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if st.State != StateDone || st.Summary != "ok" {
		t.Errorf("status = %+v, want done/ok", st)
	}
	// Runner saw the workspace dir + the resolved model from input.json.
	if !strings.HasSuffix(fr.gotWS, "/workspace") {
		t.Errorf("runner workspace = %q, want .../workspace", fr.gotWS)
	}
	if fr.gotModel != "claude-haiku" {
		t.Errorf("runner model = %q, want claude-haiku", fr.gotModel)
	}
	prog, err := fx.ReadProgress("exec-ok")
	if err != nil || len(prog) == 0 {
		t.Errorf("expected progress entries, got %v err=%v", prog, err)
	}
}

func TestRunExecutor_FailureWritesErrorAndFailedStatus(t *testing.T) {
	fx, root := runHarness(t, "exec-bad")
	fr := &fakeComputeRunner{err: errors.New("compute exploded")}
	err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: "exec-bad",
		Runner:     fr,
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err == nil {
		t.Fatal("RunExecutor failure path must return a non-nil error (exit non-zero)")
	}
	out, err := fx.ReadOutput("exec-bad")
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	if out.Success || out.Error == nil || out.Error.Kind != "runner_failed" {
		t.Errorf("output = %+v, want failure with runner_failed", out)
	}
	st, err := fx.ReadStatus("exec-bad")
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if st.State != StateFailed || st.Error == nil {
		t.Errorf("status = %+v, want failed with error", st)
	}
}

func TestRunExecutor_MissingInputIsCrash(t *testing.T) {
	root := t.TempDir()
	// No Provision / WriteInput → ReadInput returns ErrNotExist.
	err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: "ghost",
		Runner:     &fakeComputeRunner{},
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err == nil {
		t.Fatal("missing input must surface as an error (orchestrator treats as crash)")
	}
}

func TestCommandRunner_RunsCommandInWorkspace(t *testing.T) {
	var gotDir, gotName string
	var gotArgs []string
	cr := &CommandRunner{
		cmd: []string{"echo", "hello"},
		run: func(_ context.Context, dir, name string, args ...string) (string, error) {
			gotDir, gotName, gotArgs = dir, name, args
			return "hello world\nsecond line", nil
		},
	}
	var progressPhases []string
	res, err := cr.Run(context.Background(), RunContext{
		WorkspaceDir: "/ws/exec-1",
		Progress:     func(phase, _ string) { progressPhases = append(progressPhases, phase) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotDir != "/ws/exec-1" || gotName != "echo" || len(gotArgs) != 1 || gotArgs[0] != "hello" {
		t.Errorf("command exec = dir %q name %q args %v", gotDir, gotName, gotArgs)
	}
	if res.Summary != "hello world" {
		t.Errorf("summary = %q, want first line 'hello world'", res.Summary)
	}
	if len(progressPhases) != 2 || progressPhases[0] != "start" || progressPhases[1] != "done" {
		t.Errorf("progress phases = %v, want [start done]", progressPhases)
	}
}

func TestRunExecutor_RecordsUsageInOutput(t *testing.T) {
	fx, root := runHarness(t, "exec-usage")
	fr := &fakeComputeRunner{res: RunResult{
		Result:  "all good",
		Summary: "ok",
		Usage:   TokenUsage{InputTokens: 100, OutputTokens: 40, CacheReadTokens: 5, CacheWriteTokens: 2},
	}}
	if err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: "exec-usage",
		Runner:     fr,
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	}); err != nil {
		t.Fatalf("RunExecutor: %v", err)
	}
	out, err := fx.ReadOutput("exec-usage")
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	if out.Usage == nil {
		t.Fatal("output.usage = nil, want recorded usage")
	}
	if *out.Usage != (TokenUsage{InputTokens: 100, OutputTokens: 40, CacheReadTokens: 5, CacheWriteTokens: 2}) {
		t.Errorf("output.usage = %+v", *out.Usage)
	}
}

func TestRunExecutor_ZeroUsageOmittedFromOutput(t *testing.T) {
	fx, root := runHarness(t, "exec-nousage")
	fr := &fakeComputeRunner{res: RunResult{Result: "x", Summary: "ok"}} // zero usage
	if err := RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: "exec-nousage",
		Runner:     fr,
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	}); err != nil {
		t.Fatalf("RunExecutor: %v", err)
	}
	out, err := fx.ReadOutput("exec-nousage")
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	if out.Usage != nil {
		t.Errorf("output.usage = %+v, want nil for zero usage", out.Usage)
	}
}

func TestCommandRunner_ParsesUsageFromStream(t *testing.T) {
	streamOut := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"input_tokens":80,"output_tokens":20,"cache_read_input_tokens":4,"cache_creation_input_tokens":1}}`,
	}, "\n")
	cr := &CommandRunner{
		cmd: []string{"claude", "--stream"},
		run: func(_ context.Context, _, _ string, _ ...string) (string, error) {
			return streamOut, nil
		},
	}
	res, err := cr.Run(context.Background(), RunContext{Progress: func(string, string) {}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := TokenUsage{InputTokens: 80, OutputTokens: 20, CacheReadTokens: 4, CacheWriteTokens: 1}
	if res.Usage != want {
		t.Errorf("res.Usage = %+v, want %+v", res.Usage, want)
	}
}

func TestCommandRunner_EmptyCommandErrors(t *testing.T) {
	cr := NewCommandRunner(nil)
	_, err := cr.Run(context.Background(), RunContext{Progress: func(string, string) {}})
	if err == nil {
		t.Error("empty runner command must error, not invent a command")
	}
}

func TestCommandRunner_CommandFailurePropagates(t *testing.T) {
	cr := &CommandRunner{
		cmd: []string{"false"},
		run: func(_ context.Context, _, _ string, _ ...string) (string, error) {
			return "boom output", errors.New("exit status 1")
		},
	}
	_, err := cr.Run(context.Background(), RunContext{Progress: func(string, string) {}})
	if err == nil || !strings.Contains(err.Error(), "boom output") {
		t.Errorf("expected command failure with output, got %v", err)
	}
}

func TestSummarize(t *testing.T) {
	if got := summarize("\n  \n  first\nsecond"); got != "first" {
		t.Errorf("summarize = %q, want 'first'", got)
	}
	if got := summarize("   \n  "); got != "" {
		t.Errorf("summarize blank = %q, want ''", got)
	}
	long := strings.Repeat("x", 250)
	if got := summarize(long); len(got) != 200 {
		t.Errorf("summarize long len = %d, want 200", len(got))
	}
}
