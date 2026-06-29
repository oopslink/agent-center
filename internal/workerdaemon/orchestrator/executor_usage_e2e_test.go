package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

// ensureWorkspace creates the executor's workspace dir (the production Spawner
// makes it at fork time; the file-protocol Provision alone does not, and the
// CommandRunner chdir's into it).
func ensureWorkspace(t *testing.T, layout *executor.Layout, execID string) {
	t.Helper()
	ws, err := layout.WorkspaceDir(execID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_ForkedExecutorUsageReportedWithTaskID is the end-to-end seam test for
// v2.20.0 F2 (issue-47fe2a78): an executor that actually FORKS a runner subprocess
// must report that run's token usage to the center tagged with the fork-time
// Source.TaskRef.
//
// Unlike the per-component unit tests (ParseRunnerUsage on a string, Report on a
// hand-built Output), this drives the REAL production chain end to end:
//
//	RunExecutor (the forked `worker executor` entrypoint)
//	  → CommandRunner runs a REAL subprocess (the fake runner script below,
//	    emitting a claude stream-json `result` line carrying usage)
//	  → ParseRunnerUsage extracts the token counts from the captured stdout
//	  → recordSuccess writes output.json with the usage
//	  → CenterWriteback.Report reads input.json + output.json and relays the usage
//	    via report_usage, tagged with input.json's Source.TaskRef.
//
// The only stub is the final center hop (the UsageReporter), which is exactly the
// boundary we assert against. This closes the I47-class gap where a feature passes
// its unit tests but its real entrypoint was never exercised: here a genuine
// subprocess is forked and the bound task_id is asserted on what would be POSTed.
func TestE2E_ForkedExecutorUsageReportedWithTaskID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runner uses a POSIX shell")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not available: %v", err)
	}

	const (
		taskRef = "task-e2e-7"
		execID  = "exec-e2e"
	)
	// The runner's captured stdout: a banner + interleaved stderr-ish noise + a
	// real claude stream-json result line carrying the run's usage. ParseRunnerUsage
	// must pick the usage out of exactly this (the production claude --output-format
	// stream-json shape, same as executor/usage_test.go's claudeResult helper).
	const resultLine = `{"type":"result","subtype":"success","is_error":false,"result":"done",` +
		`"usage":{"input_tokens":123,"output_tokens":45,` +
		`"cache_read_input_tokens":6,"cache_creation_input_tokens":7}}`
	wantUsage := executor.TokenUsage{InputTokens: 123, OutputTokens: 45, CacheReadTokens: 6, CacheWriteTokens: 7}

	// 1. Provision a real agent home + write input.json carrying the bound TaskRef.
	root := t.TempDir()
	layout, err := executor.NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	fx, err := executor.NewFileExchange(layout, clock.NewFakeClock(wbNow))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.Provision(execID); err != nil {
		t.Fatal(err)
	}
	ensureWorkspace(t, layout, execID)
	in := executor.Input{
		ExecutorID: execID,
		Goal:       executor.Goal{Title: "e2e usage"},
		Model:      "claude-opus-4-8",
		CreatedAt:  wbNow,
		Source:     executor.SourceRefs{TaskRef: taskRef},
	}
	if err := fx.WriteInput(in); err != nil {
		t.Fatal(err)
	}

	// 2. Run the REAL executor entrypoint with a forked fake-runner subprocess.
	runnerCmd := []string{sh, "-c", "echo 'claude starting…'; printf '%s\\n' '" + resultLine + "'"}
	if err := executor.RunExecutor(context.Background(), executor.RunConfig{
		AgentRoot:  root,
		ExecutorID: execID,
		RunnerCmd:  runnerCmd,
		Clock:      clock.NewFakeClock(wbNow),
	}); err != nil {
		t.Fatalf("RunExecutor (forked runner): %v", err)
	}

	// 3. Harvest what the orchestrator would read back from the finished executor.
	out, err := fx.ReadOutput(execID)
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	st, err := fx.ReadStatus(execID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if out.Usage == nil {
		t.Fatal("output.json carries no usage — the fork→parse→record chain is broken")
	}
	if *out.Usage != wantUsage {
		t.Fatalf("output.json usage = %+v, want %+v", *out.Usage, wantUsage)
	}

	// 4. Relay through the REAL writeback usage path; assert the reported task_id.
	fc := &fakeCenter{}
	fu := &fakeUsage{}
	wb, err := NewCenterWriteback(fc, fx, "agent-e2e")
	if err != nil {
		t.Fatal(err)
	}
	wb.WithUsageReporter(fu)
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: execID,
		Kind:       executor.OutcomeSucceeded,
		Output:     &out,
		Status:     &st,
	}); err != nil {
		t.Fatalf("Report: %v", err)
	}

	// 5. Exactly one usage sample, bound to the task and carrying the run's tokens.
	if len(fu.samples) != 1 {
		t.Fatalf("usage samples = %d, want 1", len(fu.samples))
	}
	s := fu.samples[0]
	if s.TaskID != taskRef {
		t.Errorf("reported task_id = %q, want %q (the fork-time Source.TaskRef)", s.TaskID, taskRef)
	}
	if s.AgentID != "agent-e2e" {
		t.Errorf("reported agent_id = %q, want agent-e2e", s.AgentID)
	}
	if s.Model != "claude-opus-4-8" {
		t.Errorf("reported model = %q, want claude-opus-4-8", s.Model)
	}
	if s.Usage != wantUsage {
		t.Errorf("reported usage = %+v, want %+v", s.Usage, wantUsage)
	}
	// The task is completed too (usage relay is orthogonal to completion).
	if len(fc.completes) != 1 || fc.completes[0][1] != taskRef {
		t.Errorf("want task %q completed, got %v", taskRef, fc.completes)
	}
}

// TestE2E_ForkedExecutorEmptyTaskRefNotFabricated is the negative seam: a forked
// run with NO bound task (a chat/converse source) must report its usage with an
// EMPTY task_id — never fabricated — so the center cannot mis-attribute it
// (acceptance ②). Same real fork→parse→writeback chain as above.
func TestE2E_ForkedExecutorEmptyTaskRefNotFabricated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runner uses a POSIX shell")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not available: %v", err)
	}
	const execID = "exec-e2e-notask"
	const resultLine = `{"type":"result","subtype":"success","is_error":false,"result":"done",` +
		`"usage":{"input_tokens":9,"output_tokens":4,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`

	root := t.TempDir()
	layout, err := executor.NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	fx, err := executor.NewFileExchange(layout, clock.NewFakeClock(wbNow))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.Provision(execID); err != nil {
		t.Fatal(err)
	}
	ensureWorkspace(t, layout, execID)
	// Source has chat ids but NO task ref.
	in := executor.Input{
		ExecutorID: execID,
		Goal:       executor.Goal{Title: "e2e usage no task"},
		Model:      "claude-opus-4-8",
		CreatedAt:  wbNow,
		Source:     executor.SourceRefs{ChatIDs: []string{"conv-1"}},
	}
	if err := fx.WriteInput(in); err != nil {
		t.Fatal(err)
	}
	runnerCmd := []string{sh, "-c", "printf '%s\\n' '" + resultLine + "'"}
	if err := executor.RunExecutor(context.Background(), executor.RunConfig{
		AgentRoot: root, ExecutorID: execID, RunnerCmd: runnerCmd, Clock: clock.NewFakeClock(wbNow),
	}); err != nil {
		t.Fatalf("RunExecutor: %v", err)
	}
	out, err := fx.ReadOutput(execID)
	if err != nil {
		t.Fatalf("ReadOutput: %v", err)
	}
	st, err := fx.ReadStatus(execID)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	fc := &fakeCenter{}
	fu := &fakeUsage{}
	wb, err := NewCenterWriteback(fc, fx, "agent-e2e")
	if err != nil {
		t.Fatal(err)
	}
	wb.WithUsageReporter(fu)
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: execID, Kind: executor.OutcomeSucceeded, Output: &out, Status: &st,
	}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(fu.samples) != 1 {
		t.Fatalf("usage samples = %d, want 1", len(fu.samples))
	}
	if got := fu.samples[0].TaskID; got != "" {
		t.Errorf("reported task_id = %q, want empty (no fabrication for a task-less run)", got)
	}
}
