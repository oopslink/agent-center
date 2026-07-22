package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
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

// TestRunExecutor_FailurePersistErrorNotSilent pins the v2.34.0 dogfood cleanup: when the
// failure path cannot PERSIST output.json, the persist error is no longer swallowed — it
// is surfaced in the returned (exit) error, while the ROOT-CAUSE runner error stays
// recoverable via errors.Is (the exit-code signal is unchanged).
func TestRunExecutor_FailurePersistErrorNotSilent(t *testing.T) {
	_, root := runHarness(t, "exec-persistfail")
	// Force WriteOutput to fail deterministically (no chmod/root fragility): plant a
	// DIRECTORY where output.json goes, so the atomic write's final rename cannot land.
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	outPath, err := layout.OutputPath("exec-persistfail")
	if err != nil {
		t.Fatalf("OutputPath: %v", err)
	}
	if err := os.MkdirAll(outPath, 0o700); err != nil {
		t.Fatalf("plant output.json dir: %v", err)
	}

	runnerErr := errors.New("compute exploded")
	err = RunExecutor(context.Background(), RunConfig{
		AgentRoot:  root,
		ExecutorID: "exec-persistfail",
		Runner:     &fakeComputeRunner{err: runnerErr},
		Clock:      clock.NewFakeClock(time.Unix(1700000000, 0)),
	})
	if err == nil {
		t.Fatal("failure path must return a non-nil error (exit non-zero)")
	}
	if !errors.Is(err, runnerErr) {
		t.Errorf("returned error must still wrap the root-cause runner error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "write failure output") {
		t.Errorf("a WriteOutput persist failure must be surfaced (not silent), got: %v", err)
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
		run: func(_ context.Context, dir string, cmd []string, _ func(string)) (string, error) {
			gotDir = dir
			if len(cmd) > 0 {
				gotName, gotArgs = cmd[0], cmd[1:]
			}
			return "hello world\nsecond line", nil
		},
	}
	var progressPhases []string
	res, err := cr.Run(context.Background(), RunContext{
		WorkspaceDir: "/ws/exec-1",
		Progress:     func(phase, _ string, _ ...string) { progressPhases = append(progressPhases, phase) },
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

// TestCommandRunner_CodexBranch_CapturesThreadID covers the T969 run.go cli-branch: a
// `codex exec` runner's captured --json output is parsed by ParseCodexRunnerStream, so
// RunResult carries the clean result + the captured thread_id (→ Record.SessionID).
func TestCommandRunner_CodexBranch_CapturesThreadID(t *testing.T) {
	cr := &CommandRunner{
		cmd: []string{"codex", "exec", "--json", "-m", "gpt-5.5", "do it"},
		run: func(_ context.Context, _ string, _ []string, _ func(string)) (string, error) {
			return strings.Join([]string{
				`{"type":"thread.started","thread_id":"th_run1"}`,
				`{"type":"item.completed","item":{"type":"agent_message","text":"codex result"}}`,
				`{"type":"turn.completed","usage":{"input_tokens":9,"output_tokens":2}}`,
			}, "\n"), nil
		},
	}
	res, err := cr.Run(context.Background(), RunContext{WorkspaceDir: "/ws/e1", Progress: func(string, string, ...string) {}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ThreadID != "th_run1" {
		t.Errorf("ThreadID = %q, want th_run1 (captured for resume)", res.ThreadID)
	}
	if res.Result != "codex result" {
		t.Errorf("Result = %q, want clean 'codex result' (not raw JSONL)", res.Result)
	}
	if res.Usage.OutputTokens != 2 {
		t.Errorf("usage out = %d, want 2", res.Usage.OutputTokens)
	}
}

// TestCommandRunner_ClaudeUnchanged is the T969 CLAUDE ZERO-REGRESSION lock: a non-codex
// runner still parses via ParseRunnerStream and carries an EMPTY ThreadID — the codex
// branch must never touch the claude path.
func TestCommandRunner_ClaudeUnchanged(t *testing.T) {
	cr := &CommandRunner{
		cmd: []string{"claude", "-p", "do it", "--output-format", "stream-json"},
		run: func(_ context.Context, _ string, _ []string, _ func(string)) (string, error) {
			return `{"type":"result","subtype":"success","result":"claude answer","usage":{"input_tokens":5,"output_tokens":1}}`, nil
		},
	}
	res, err := cr.Run(context.Background(), RunContext{WorkspaceDir: "/ws/e2", Progress: func(string, string, ...string) {}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ThreadID != "" {
		t.Errorf("claude ThreadID = %q, want empty (codex branch must not touch claude)", res.ThreadID)
	}
	if res.Result != "claude answer" {
		t.Errorf("claude Result = %q, want 'claude answer' via ParseRunnerStream", res.Result)
	}
}

// TestIsCodexRunnerCmd pins the cli detection that gates the parser branch.
func TestIsCodexRunnerCmd(t *testing.T) {
	cases := []struct {
		cmd  []string
		want bool
	}{
		{[]string{"codex", "exec", "--json"}, true},
		{[]string{"/opt/codex", "exec", "resume", "t"}, true},
		{[]string{"claude", "-p", "x"}, false},
		{[]string{"codex"}, false},            // no exec subcommand
		{[]string{"codexish", "exec"}, false}, // basename must be exactly "codex"
		{nil, false},
	}
	for _, c := range cases {
		if got := isCodexRunnerCmd(c.cmd); got != c.want {
			t.Errorf("isCodexRunnerCmd(%v) = %v, want %v", c.cmd, got, c.want)
		}
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
		run: func(_ context.Context, _ string, _ []string, _ func(string)) (string, error) {
			return streamOut, nil
		},
	}
	res, err := cr.Run(context.Background(), RunContext{Progress: func(string, string, ...string) {}})
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
	_, err := cr.Run(context.Background(), RunContext{Progress: func(string, string, ...string) {}})
	if err == nil {
		t.Error("empty runner command must error, not invent a command")
	}
}

func TestCommandRunner_CommandFailurePropagates(t *testing.T) {
	cr := &CommandRunner{
		cmd: []string{"false"},
		run: func(_ context.Context, _ string, _ []string, _ func(string)) (string, error) {
			return "boom output", errors.New("exit status 1")
		},
	}
	_, err := cr.Run(context.Background(), RunContext{Progress: func(string, string, ...string) {}})
	if err == nil || !strings.Contains(err.Error(), "boom output") {
		t.Errorf("expected command failure with output, got %v", err)
	}
}

// TestCommandRunner_HeartbeatsDuringStream_T877 locks the stall-fix: the runner must
// refresh progress ("running" heartbeat) WHILE the command streams output, not only at
// start/done. Before, CombinedOutput blocked to completion so last_progress_at was
// frozen the whole run and a legit heavy claude run was falsely stall-killed.
func TestCommandRunner_HeartbeatsDuringStream_T877(t *testing.T) {
	cr := &CommandRunner{
		cmd: []string{"claude", "--stream"},
		run: func(_ context.Context, _ string, _ []string, onLine func(string)) (string, error) {
			for i := 0; i < 5; i++ { // simulate a long stream of stream-json lines
				onLine(`{"type":"assistant","message":{"content":[]}}`)
			}
			return "final answer", nil
		},
	}
	var phases []string
	if _, err := cr.Run(context.Background(), RunContext{
		Progress: func(phase, _ string, _ ...string) { phases = append(phases, phase) },
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	running := 0
	for _, p := range phases {
		if p == "running" {
			running++
		}
	}
	if running < 1 {
		t.Errorf("no 'running' heartbeat during stream; phases=%v — a heavy run would falsely stall", phases)
	}
	if len(phases) < 2 || phases[0] != "start" || phases[len(phases)-1] != "done" {
		t.Errorf("phases = %v, want start…running…done", phases)
	}
}

func TestCommandRunner_StaleCodexResumeRetriesFreshOnce(t *testing.T) {
	var calls [][]string
	cr := &CommandRunner{
		cmd: []string{"codex", "exec", "resume", "stale-thread", "--json", "prompt"},
		run: func(_ context.Context, _ string, cmd []string, _ func(string)) (string, error) {
			calls = append(calls, append([]string(nil), cmd...))
			if len(calls) == 1 {
				return "thread/resume failed: no rollout found for thread id stale-thread", errors.New("exit 1")
			}
			return `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`, nil
		},
	}
	got, err := cr.Run(context.Background(), RunContext{Progress: func(string, string, ...string) {}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Result != "done" || len(calls) != 2 {
		t.Fatalf("result=%q calls=%v", got.Result, calls)
	}
	want := []string{"codex", "exec", "--json", "prompt"}
	if !reflect.DeepEqual(calls[1], want) {
		t.Fatalf("fresh retry cmd=%v want=%v", calls[1], want)
	}
}

// TestExecRun_StreamsStdoutAndPreservesStderr_T877 locks the production exec seam: it
// invokes onLine per STDOUT line (the heartbeat source) AND keeps STDERR in the
// returned output (pd Gate: a runner_failed diagnostic like claude's "Session ID … in
// use" is on stderr and must not be dropped when we switch off CombinedOutput).
func TestExecRun_StreamsStdoutAndPreservesStderr_T877(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh unavailable: %v", err)
	}
	var lines []string
	out, rerr := execRun(context.Background(), t.TempDir(),
		[]string{sh, "-c", "echo L1; echo L2; echo ERRTEXT 1>&2; echo L3"},
		func(l string) { lines = append(lines, l) })
	if rerr != nil {
		t.Fatalf("execRun: %v", rerr)
	}
	if len(lines) != 3 {
		t.Errorf("onLine calls = %d (%v), want 3 stdout lines", len(lines), lines)
	}
	if !strings.Contains(out, "L1") || !strings.Contains(out, "L3") {
		t.Errorf("stdout content lost: %q", out)
	}
	if !strings.Contains(out, "ERRTEXT") {
		t.Errorf("stderr DROPPED — pd Gate requires preserving it: %q", out)
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
