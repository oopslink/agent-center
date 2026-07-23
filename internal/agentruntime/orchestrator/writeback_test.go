package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/clock"
)

var wbNow = time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

// fakeCenter records the writeback's center calls.
type fakeCenter struct {
	mu         sync.Mutex
	completes  [][3]string // agentID, taskID, summary
	blocks     [][4]string // agentID, taskID, reason, reasonType
	resets     [][2]string // agentID, taskID
	posts      [][3]string // agentID, conversationID, content
	taskPosts  [][3]string // agentID, taskID, content (P0-A ch2 delivery line on the task)
	injections []string    // option b: judgment prompts injected to the supervisor
	err        error       // returned by every center call when set
	injErr     error       // returned by the supervisor injector when set
}

func (f *fakeCenter) CompleteTask(_ context.Context, a, t, s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completes = append(f.completes, [3]string{a, t, s})
	return f.err
}
func (f *fakeCenter) ResetTask(_ context.Context, a, t string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets = append(f.resets, [2]string{a, t})
	return f.err
}
func (f *fakeCenter) BlockTask(_ context.Context, a, t, r, rt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocks = append(f.blocks, [4]string{a, t, r, rt})
	return f.err
}
func (f *fakeCenter) PostMessage(_ context.Context, a, c, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, [3]string{a, c, content})
	return f.err
}
func (f *fakeCenter) PostToTask(_ context.Context, a, tid, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.taskPosts = append(f.taskPosts, [3]string{a, tid, content})
	return f.err
}

// newWB builds a CenterWriteback over a real FileExchange in a temp agent root,
// pre-provisioning the executor dir + input.json so Report can ReadInput.
func newWB(t *testing.T, fc *fakeCenter, in executor.Input) (*CenterWriteback, *executor.FileExchange) {
	t.Helper()
	layout, err := executor.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fx, err := executor.NewFileExchange(layout, clock.NewFakeClock(wbNow))
	if err != nil {
		t.Fatal(err)
	}
	if in.ExecutorID != "" {
		if _, err := fx.Provision(in.ExecutorID); err != nil {
			t.Fatal(err)
		}
		if err := fx.WriteInput(in); err != nil {
			t.Fatal(err)
		}
	}
	wb, err := NewCenterWriteback(fc, fx, "agent-x")
	if err != nil {
		t.Fatal(err)
	}
	// option b: wire a capturing supervisor injector so Report delivers a judgment
	// prompt instead of auto-completing.
	wb.WithSupervisorInjector(func(_ context.Context, _, text string) error {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		fc.injections = append(fc.injections, text)
		return fc.injErr
	})
	return wb, fx
}

func baseInput(id string) executor.Input {
	return executor.Input{
		ExecutorID: id,
		Goal:       executor.Goal{Title: "do thing"},
		Model:      "claude-haiku-4-5",
		CreatedAt:  wbNow,
		Source:     executor.SourceRefs{TaskRef: "task-1"},
	}
}

func TestNewCenterWriteback_Validation(t *testing.T) {
	layout, _ := executor.NewLayout(t.TempDir())
	fx, _ := executor.NewFileExchange(layout, clock.NewFakeClock(wbNow))
	if _, err := NewCenterWriteback(nil, fx, "a"); err == nil {
		t.Error("nil client should error")
	}
	if _, err := NewCenterWriteback(&fakeCenter{}, nil, "a"); err == nil {
		t.Error("nil fx should error")
	}
	if _, err := NewCenterWriteback(&fakeCenter{}, fx, "  "); err == nil {
		t.Error("empty agentID should error")
	}
}

// oneInjection returns the single judgment prompt injected to the supervisor,
// asserting exactly one injection and NO auto-complete/block/post (option b: the
// writeback delivers a judgment, the SUPERVISOR writes status).
func oneInjection(t *testing.T, fc *fakeCenter) string {
	t.Helper()
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.injections) != 1 {
		t.Fatalf("want 1 judgment injection, got %d: %v", len(fc.injections), fc.injections)
	}
	if len(fc.completes) != 0 || len(fc.blocks) != 0 || len(fc.posts) != 0 {
		t.Fatalf("option b: must NOT auto-complete/block/post; completes=%v blocks=%v posts=%v", fc.completes, fc.blocks, fc.posts)
	}
	return fc.injections[0]
}

func TestReport_Succeeded_InjectsJudgment(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-1")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{
		ExecutorID: "exec-1",
		Kind:       executor.OutcomeSucceeded,
		Status:     &executor.Status{ExecutorID: "exec-1", State: executor.StateDone, Summary: "built and tested", StartedAt: wbNow},
		Output:     &executor.Output{ExecutorID: "exec-1", Success: true, Result: "full result text", FinishedAt: wbNow},
	}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatalf("Report: %v", err)
	}
	j := oneInjection(t, fc)
	for _, want := range []string{"task-1", "succeeded", "built and tested", "complete_task", "block_task"} {
		if !strings.Contains(j, want) {
			t.Errorf("judgment missing %q: %q", want, j)
		}
	}
	for _, want := range []string{
		"Your Agent's executor",
		"Supervisor control plane",
		"same Agent's isolated execution unit",
		"final delivery remains YOUR judged responsibility",
		"if this Agent TRULY delivered",
	} {
		if !strings.Contains(j, want) {
			t.Errorf("judgment missing same-agent identity contract %q: %q", want, j)
		}
	}
	if strings.Contains(j, "Your forked executor") {
		t.Errorf("judgment kept old externalizing wording: %q", j)
	}
}

// TestReport_JudgmentSurfacesDeliveryBranch is the issue-f30b7e7b P0-A lock: the judgment
// prompt must carry the structured delivery evidence (branch + SHA + pushed) so the judging
// supervisor knows EXACTLY which branch/commit to inspect — closing the "pushed but nobody
// knows where to look = nominal delivery" gap.
func TestReport_JudgmentSurfacesDeliveryBranch(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-9")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{
		ExecutorID: "exec-9",
		Kind:       executor.OutcomeSucceeded,
		Status:     &executor.Status{ExecutorID: "exec-9", State: executor.StateDone, Summary: "did work", StartedAt: wbNow},
		Output:     &executor.Output{ExecutorID: "exec-9", Success: true, Result: "r", FinishedAt: wbNow},
		Git:        &executor.FinalizedGitStatus{Probed: true, Branch: "ac-exec/task-1/exec-9", HeadSHA: "abc1234", Pushed: true, BaseRef: "main", BaseKnown: true, AheadOfBase: 2},
	}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatalf("Report: %v", err)
	}
	j := oneInjection(t, fc)
	// Branch + SHA + pushed + the BASE lineage (so the judge can verify base via merge-base,
	// not just recency — the WebUI wrong-base incident) + the base-check instruction.
	for _, want := range []string{"ac-exec/task-1/exec-9", "abc1234", "pushed=true", "on origin", "Based on main", "merge-base"} {
		if !strings.Contains(j, want) {
			t.Errorf("judgment must surface delivery branch+SHA+base (%q missing): %q", want, j)
		}
	}
	// P0-A ch2: the delivery evidence is ALSO posted onto the task conversation, so
	// review/PD/integration (not just the judging supervisor) can find the branch.
	if len(fc.taskPosts) != 1 {
		t.Fatalf("expected exactly 1 delivery post to the task, got %d", len(fc.taskPosts))
	}
	tp := fc.taskPosts[0]
	if tp[1] != "task-1" || !strings.Contains(tp[2], "ac-exec/task-1/exec-9") || !strings.Contains(tp[2], "abc1234") {
		t.Errorf("task delivery post must carry the branch+SHA, got taskID=%q content=%q", tp[1], tp[2])
	}
}

// TestReport_JudgmentSurfacesPushFailure locks the failure-surface half: when the eager-push
// failed, the judgment prompt states the branch is committed but NOT on origin + why.
func TestReport_JudgmentSurfacesPushFailure(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-10")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{
		ExecutorID: "exec-10",
		Kind:       executor.OutcomeCrashed,
		Error:      &executor.ErrorDetail{Kind: "non_delivery", Message: "no durable delivery — eager-push failed: auth denied"},
		Status:     &executor.Status{ExecutorID: "exec-10", State: executor.StateDone, StartedAt: wbNow},
		Git:        &executor.FinalizedGitStatus{Probed: true, Branch: "ac-exec/task-1/exec-10", HeadSHA: "def5678", Pushed: false, PushError: "auth denied"},
	}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatalf("Report: %v", err)
	}
	j := oneInjection(t, fc)
	for _, want := range []string{"ac-exec/task-1/exec-10", "pushed=false", "eager-push FAILED", "auth denied"} {
		if !strings.Contains(j, want) {
			t.Errorf("judgment must surface the push failure (%q missing): %q", want, j)
		}
	}
}

func TestReport_Succeeded_SummaryFallback(t *testing.T) {
	// No status summary → falls back to output.Result.
	fc := &fakeCenter{}
	in := baseInput("exec-2")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{
		ExecutorID: "exec-2",
		Kind:       executor.OutcomeSucceeded,
		Output:     &executor.Output{ExecutorID: "exec-2", Success: true, Result: "the result", FinishedAt: wbNow},
	}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if j := oneInjection(t, fc); !strings.Contains(j, "the result") {
		t.Errorf("judgment = %q want result fallback", j)
	}

	// Neither summary nor result → falls back to goal title text.
	fc2 := &fakeCenter{}
	in2 := baseInput("exec-3")
	wb2, _ := newWB(t, fc2, in2)
	c2 := executor.Completion{ExecutorID: "exec-3", Kind: executor.OutcomeSucceeded}
	if err := wb2.Report(context.Background(), c2); err != nil {
		t.Fatal(err)
	}
	if j := oneInjection(t, fc2); !strings.Contains(j, "do thing") {
		t.Errorf("judgment = %q want goal title fallback", j)
	}
}

func TestReport_Failed_BlocksTask(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-4")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{
		ExecutorID: "exec-4",
		Kind:       executor.OutcomeFailed,
		Error:      &executor.ErrorDetail{Kind: "runner_failed", Message: "boom"},
	}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	j := oneInjection(t, fc)
	for _, want := range []string{"task-1", "failed", "runner_failed", "boom", "block_task"} {
		if !strings.Contains(j, want) {
			t.Errorf("judgment missing %q: %q", want, j)
		}
	}
}

func TestReport_Crashed_BlocksRetryable(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-5")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{
		ExecutorID: "exec-5",
		Kind:       executor.OutcomeCrashed,
		Retryable:  true,
		Error:      &executor.ErrorDetail{Kind: "clean_exit_no_output", Message: "no output"},
	}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if j := oneInjection(t, fc); !strings.Contains(j, "crashed (retryable)") {
		t.Errorf("crash judgment = %q", j)
	}
}

func TestReport_Failed_NoErrorDetail(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-6")
	wb, _ := newWB(t, fc, in)
	c := executor.Completion{ExecutorID: "exec-6", Kind: executor.OutcomeFailed}
	if err := wb.Report(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if j := oneInjection(t, fc); !strings.Contains(j, "no error detail") {
		t.Errorf("judgment = %q", j)
	}
}

func TestReport_Running_NoOp(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, baseInput("exec-7"))
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "exec-7", Kind: executor.OutcomeRunning}); err != nil {
		t.Fatal(err)
	}
	if len(fc.completes)+len(fc.blocks)+len(fc.posts) != 0 {
		t.Error("running should be a no-op")
	}
}

func TestReport_NoTaskRef_PostsToChat(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-8")
	in.Source = executor.SourceRefs{ChatIDs: []string{"", "chan-9"}} // skips blank, uses chan-9
	wb, _ := newWB(t, fc, in)
	// success
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-8", Kind: executor.OutcomeSucceeded,
		Status: &executor.Status{ExecutorID: "exec-8", State: executor.StateDone, Summary: "ok", StartedAt: wbNow},
	}); err != nil {
		t.Fatal(err)
	}
	if len(fc.posts) != 1 || fc.posts[0][1] != "chan-9" || fc.posts[0][2] != "ok" {
		t.Fatalf("post = %v", fc.posts)
	}
	if len(fc.completes) != 0 {
		t.Error("no task → no complete_task")
	}
	// failure path also posts
	fc.posts = nil
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-8", Kind: executor.OutcomeFailed, Error: &executor.ErrorDetail{Kind: "k", Message: "m"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(fc.posts) != 1 || !strings.Contains(fc.posts[0][2], "m") {
		t.Errorf("failure post = %v", fc.posts)
	}
}

func TestReport_NoSourceAtAll_Errors(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("exec-9")
	in.Source = executor.SourceRefs{} // no task, no chat
	wb, _ := newWB(t, fc, in)
	err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-9", Kind: executor.OutcomeSucceeded,
		Status: &executor.Status{ExecutorID: "exec-9", State: executor.StateDone, Summary: "x", StartedAt: wbNow},
	})
	if err == nil || !strings.Contains(err.Error(), "no task or chat source") {
		t.Fatalf("want no-source error, got %v", err)
	}
}

func TestReport_InputMissing_Errors(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, executor.Input{}) // nothing provisioned
	err := wb.Report(context.Background(), executor.Completion{ExecutorID: "exec-absent", Kind: executor.OutcomeSucceeded})
	if err == nil || !strings.Contains(err.Error(), "read input") {
		t.Fatalf("want read-input error, got %v", err)
	}
}

func TestReport_UnknownKind_Errors(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, baseInput("exec-10"))
	err := wb.Report(context.Background(), executor.Completion{ExecutorID: "exec-10", Kind: "weird"})
	if err == nil || !strings.Contains(err.Error(), "unknown completion kind") {
		t.Fatalf("want unknown-kind error, got %v", err)
	}
}

func TestReport_InjectError_Propagates(t *testing.T) {
	fc := &fakeCenter{injErr: errors.New("session gone")}
	wb, _ := newWB(t, fc, baseInput("exec-11"))
	err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-11", Kind: executor.OutcomeSucceeded,
		Status: &executor.Status{ExecutorID: "exec-11", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	})
	if err == nil || !strings.Contains(err.Error(), "inject judgment") {
		t.Fatalf("want inject-judgment error, got %v", err)
	}
}

// fakeMem records WriteCompletion calls and can fail.
type fakeMem struct {
	calls int
	err   error
}

func (m *fakeMem) WriteCompletion(_ context.Context, _ executor.Input, _ executor.Completion) error {
	m.calls++
	return m.err
}

func TestReport_MemoryWriter(t *testing.T) {
	fc := &fakeCenter{}
	mem := &fakeMem{}
	wb, _ := newWB(t, fc, baseInput("exec-12"))
	wb.WithMemoryWriter(mem)
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-12", Kind: executor.OutcomeSucceeded,
		Status: &executor.Status{ExecutorID: "exec-12", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatal(err)
	}
	if mem.calls != 1 {
		t.Errorf("memory writer calls = %d want 1", mem.calls)
	}
	// A memory writer failure is surfaced (not swallowed).
	mem.err = errors.New("disk full")
	err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-12", Kind: executor.OutcomeSucceeded,
		Status: &executor.Status{ExecutorID: "exec-12", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	})
	if err == nil || !strings.Contains(err.Error(), "memory") {
		t.Fatalf("want memory error, got %v", err)
	}
}

// fakeUsage records the usage samples the writeback relays and can fail.
type fakeUsage struct {
	samples []UsageSample
	err     error
}

func (u *fakeUsage) ReportUsage(_ context.Context, s UsageSample) error {
	u.samples = append(u.samples, s)
	return u.err
}

// usageOutput is a successful output.json carrying token usage at wbNow.
func usageOutput(id string, in, out, cr, cw int) *executor.Output {
	return &executor.Output{
		ExecutorID: id, Success: true, Result: "r", FinishedAt: wbNow,
		Usage: &executor.TokenUsage{InputTokens: in, OutputTokens: out, CacheReadTokens: cr, CacheWriteTokens: cw},
	}
}

func TestReport_Usage_ReportedWithBoundTaskID(t *testing.T) {
	fc := &fakeCenter{}
	fu := &fakeUsage{}
	in := baseInput("exec-u1") // TaskRef "task-1", Model "claude-haiku-4-5"
	wb, _ := newWB(t, fc, in)
	wb.WithUsageReporter(fu)
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u1", Kind: executor.OutcomeSucceeded,
		Output: usageOutput("exec-u1", 100, 40, 5, 2),
		Status: &executor.Status{ExecutorID: "exec-u1", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(fu.samples) != 1 {
		t.Fatalf("usage samples = %d, want 1", len(fu.samples))
	}
	s := fu.samples[0]
	if s.AgentID != "agent-x" || s.TaskID != "task-1" || s.Model != "claude-haiku-4-5" {
		t.Errorf("sample meta = %+v", s)
	}
	if s.Usage != (executor.TokenUsage{InputTokens: 100, OutputTokens: 40, CacheReadTokens: 5, CacheWriteTokens: 2}) {
		t.Errorf("sample usage = %+v", s.Usage)
	}
	if !s.At.Equal(wbNow) {
		t.Errorf("sample at = %v, want %v (output.FinishedAt)", s.At, wbNow)
	}
	// Task judgment still delivered (usage is orthogonal to result routing).
	if len(fc.injections) != 1 {
		t.Errorf("want task judgment injected, got %v", fc.injections)
	}
}

func TestReport_Usage_EmptyTaskRefStaysEmpty(t *testing.T) {
	// Acceptance ②: a task-less run must NOT fabricate a task_id (kept empty so the
	// center never mis-attributes).
	fc := &fakeCenter{}
	fu := &fakeUsage{}
	in := baseInput("exec-u2")
	in.Source = executor.SourceRefs{ChatIDs: []string{"conv-1"}} // chat, no task
	wb, _ := newWB(t, fc, in)
	wb.WithUsageReporter(fu)
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u2", Kind: executor.OutcomeSucceeded,
		Output: usageOutput("exec-u2", 7, 3, 0, 0),
		Status: &executor.Status{ExecutorID: "exec-u2", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(fu.samples) != 1 || fu.samples[0].TaskID != "" {
		t.Fatalf("want one sample with empty task_id, got %+v", fu.samples)
	}
}

func TestReport_Usage_ReportedOnFailure(t *testing.T) {
	// Tokens spent on a failed run are still accounted (usage is independent of
	// the success/failure routing).
	fc := &fakeCenter{}
	fu := &fakeUsage{}
	wb, _ := newWB(t, fc, baseInput("exec-u3"))
	wb.WithUsageReporter(fu)
	out := usageOutput("exec-u3", 9, 9, 0, 0)
	out.Success = false
	out.Result = ""
	out.Error = &executor.ErrorDetail{Kind: "runner_failed", Message: "boom"}
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u3", Kind: executor.OutcomeFailed,
		Output: out,
		Error:  out.Error,
	}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(fu.samples) != 1 {
		t.Errorf("want usage reported on failure, got %v", fu.samples)
	}
	if len(fc.injections) != 1 {
		t.Errorf("want task judgment injected on failure, got %v", fc.injections)
	}
}

func TestReport_Usage_BestEffort(t *testing.T) {
	// A usage-report error must NOT fail the writeback (no dir-retain / re-report).
	fc := &fakeCenter{}
	fu := &fakeUsage{err: errors.New("usage endpoint down")}
	wb, _ := newWB(t, fc, baseInput("exec-u4"))
	wb.WithUsageReporter(fu)
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u4", Kind: executor.OutcomeSucceeded,
		Output: usageOutput("exec-u4", 1, 1, 0, 0),
		Status: &executor.Status{ExecutorID: "exec-u4", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatalf("usage error must be swallowed, got %v", err)
	}
	if len(fc.injections) != 1 {
		t.Errorf("task judgment must still be injected despite usage error")
	}
}

func TestReport_Usage_SkippedWhenAbsent(t *testing.T) {
	fc := &fakeCenter{}
	fu := &fakeUsage{}
	wb, _ := newWB(t, fc, baseInput("exec-u5"))
	wb.WithUsageReporter(fu)
	// (a) no output usage → no report.
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u5", Kind: executor.OutcomeSucceeded,
		Output: &executor.Output{ExecutorID: "exec-u5", Success: true, Result: "r", FinishedAt: wbNow},
		Status: &executor.Status{ExecutorID: "exec-u5", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatal(err)
	}
	// (b) zero usage → no report.
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u5", Kind: executor.OutcomeSucceeded,
		Output: usageOutput("exec-u5", 0, 0, 0, 0),
		Status: &executor.Status{ExecutorID: "exec-u5", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatal(err)
	}
	if len(fu.samples) != 0 {
		t.Errorf("want no usage samples, got %+v", fu.samples)
	}
}

func TestReport_Usage_NilReporterNoPanic(t *testing.T) {
	// No reporter wired: a usage-bearing completion is a safe no-op.
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, baseInput("exec-u6"))
	if err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-u6", Kind: executor.OutcomeSucceeded,
		Output: usageOutput("exec-u6", 5, 5, 0, 0),
		Status: &executor.Status{ExecutorID: "exec-u6", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	}); err != nil {
		t.Fatalf("nil reporter must be a no-op, got %v", err)
	}
}

func TestClip(t *testing.T) {
	if got := clip("  hi  "); got != "hi" {
		t.Errorf("clip trim = %q", got)
	}
	long := strings.Repeat("a", maxRelayChars+50)
	got := clip(long)
	if len(got) != maxRelayChars+len("…") || !strings.HasSuffix(got, "…") {
		t.Errorf("clip truncate len=%d", len(got))
	}
}

func TestReport_SoleWriterSerializes(t *testing.T) {
	// Concurrent Reports must not race (the mutex makes the orchestrator sole writer).
	fc := &fakeCenter{}
	layout, _ := executor.NewLayout(t.TempDir())
	fx, _ := executor.NewFileExchange(layout, clock.NewFakeClock(wbNow))
	wb, _ := NewCenterWriteback(fc, fx, "agent-x")
	wb.WithSupervisorInjector(func(_ context.Context, _, text string) error {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		fc.injections = append(fc.injections, text)
		return fc.injErr
	})
	const n = 8
	for i := 0; i < n; i++ {
		id := "exec-c" + string(rune('a'+i))
		in := baseInput(id)
		in.Source.TaskRef = "task-" + id
		if _, err := fx.Provision(id); err != nil {
			t.Fatal(err)
		}
		if err := fx.WriteInput(in); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		id := "exec-c" + string(rune('a'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = wb.Report(context.Background(), executor.Completion{
				ExecutorID: id, Kind: executor.OutcomeSucceeded,
				Status: &executor.Status{ExecutorID: id, State: executor.StateDone, Summary: "s", StartedAt: wbNow},
			})
		}()
	}
	wg.Wait()
	if len(fc.injections) != n {
		t.Errorf("want %d judgment injections, got %d", n, len(fc.injections))
	}
}

// fakeDelivery captures delivery samples relayed by the writeback (issue-f30b7e7b).
type fakeDelivery struct {
	mu      sync.Mutex
	samples []DeliverySample
	err     error
}

func (f *fakeDelivery) ReportDelivery(_ context.Context, s DeliverySample) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.samples = append(f.samples, s)
	return nil
}

// TestReport_RelaysDeliveryStatus locks that a terminal writeback relays the executor's
// git delivery status (agent/task/git) — the never-pushed shape the center reconcile
// must catch — to the DeliveryReporter (issue-f30b7e7b).
func TestReport_RelaysDeliveryStatus(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, baseInput("e1"))
	fd := &fakeDelivery{}
	wb.WithDeliveryReporter(fd)

	git := &executor.FinalizedGitStatus{Probed: true, Branch: "feat/review-only", HeadSHA: "abc", Pushed: false, BaseKnown: true, AheadOfBase: 2}
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeSucceeded, Git: git}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	fd.mu.Lock()
	defer fd.mu.Unlock()
	if len(fd.samples) != 1 {
		t.Fatalf("want 1 delivery sample, got %d", len(fd.samples))
	}
	s := fd.samples[0]
	if s.AgentID != "agent-x" || s.TaskID != "task-1" {
		t.Errorf("sample id = %q/%q, want agent-x/task-1", s.AgentID, s.TaskID)
	}
	if s.Git == nil || s.Git.Pushed || s.Git.Branch != "feat/review-only" {
		t.Errorf("sample git = %+v, want non-nil Pushed=false Branch=feat/review-only", s.Git)
	}
}

// TestReport_DeliveryNoopWhenNoGitOrNoReporter locks the best-effort no-ops: no
// reporter wired, or a nil c.Git (non-git workspace), reports nothing and never panics.
func TestReport_DeliveryNoopWhenNoGitOrNoReporter(t *testing.T) {
	fc := &fakeCenter{}
	// (a) no reporter wired → Report still succeeds, no panic.
	wb, _ := newWB(t, fc, baseInput("e1"))
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeSucceeded, Git: &executor.FinalizedGitStatus{Probed: true}}); err != nil {
		t.Fatalf("Report with no reporter: %v", err)
	}
	// (b) reporter wired but c.Git nil (non-git workspace) → no sample.
	fd := &fakeDelivery{}
	wb2, _ := newWB(t, fc, baseInput("e2"))
	wb2.WithDeliveryReporter(fd)
	if err := wb2.Report(context.Background(), executor.Completion{ExecutorID: "e2", Kind: executor.OutcomeSucceeded, Git: nil}); err != nil {
		t.Fatalf("Report with nil git: %v", err)
	}
	fd.mu.Lock()
	defer fd.mu.Unlock()
	if len(fd.samples) != 0 {
		t.Fatalf("nil c.Git must not report delivery, got %d", len(fd.samples))
	}
}

// TestReport_DeliveryErrorNeverWedges locks that a delivery-report failure is swallowed
// — it never fails the writeback or blocks the judgment path (best-effort side-channel).
func TestReport_DeliveryErrorNeverWedges(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, baseInput("e1"))
	wb.WithDeliveryReporter(&fakeDelivery{err: errors.New("center down")})
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeSucceeded, Git: &executor.FinalizedGitStatus{Probed: true}}); err != nil {
		t.Fatalf("delivery-report error must not wedge Report: %v", err)
	}
	// The judgment is still delivered — the delivery failure did not short-circuit.
	oneInjection(t, fc)
}

// inlineInput returns a task-bound Input tagged supervisor_inline.
func inlineInput(id string) executor.Input {
	in := baseInput(id)
	in.DispatchMode = executor.DispatchModeSupervisorInline
	return in
}

// oneBlock asserts exactly one auto-block (obstacle) and NO judgment/complete/post — the N4
// defensive net writes the task state itself (safe auto-BLOCK), bypassing the judgment loop.
func oneBlock(t *testing.T, fc *fakeCenter) [4]string {
	t.Helper()
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.blocks) != 1 {
		t.Fatalf("want 1 auto-block, got %d: %v", len(fc.blocks), fc.blocks)
	}
	if len(fc.injections) != 0 || len(fc.completes) != 0 || len(fc.posts) != 0 {
		t.Fatalf("auto-block must NOT judge/complete/post; inj=%v completes=%v posts=%v", fc.injections, fc.completes, fc.posts)
	}
	b := fc.blocks[0]
	if b[3] != "obstacle" {
		t.Errorf("reasonType = %q, want obstacle", b[3])
	}
	if b[0] != "agent-x" || b[1] != "task-1" {
		t.Errorf("block agent/task = %q/%q, want agent-x/task-1", b[0], b[1])
	}
	return b
}

// TestReport_SupervisorInline_EmptyWorkspace_AutoBlocks is the KEY N4 defensive lock: a
// mis-forked inline node (deploy/verdict — empty, non-git workspace) whose probe is !Probed
// is TRUSTED by N3 as OutcomeSucceeded. The inline net must STILL auto-block it (gating on
// !(Probed&&Pushed), not c.Kind) — else it false-succeeds + spins.
func TestReport_SupervisorInline_EmptyWorkspace_AutoBlocks(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, inlineInput("e1"))
	// Empty / non-git workspace ⇒ c.Git nil (probe found no repo). N3 would keep this
	// OutcomeSucceeded, but the inline net gates on durable delivery, not c.Kind.
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeSucceeded, Git: nil}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	oneBlock(t, fc)
}

// TestReport_SupervisorInline_CommittedUnpushed_AutoBlocks: an inline node that landed in a
// git tree and committed but never pushed → no durable delivery → auto-block.
func TestReport_SupervisorInline_CommittedUnpushed_AutoBlocks(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, inlineInput("e1"))
	git := &executor.FinalizedGitStatus{Probed: true, Pushed: false, Branch: "feat/x", AheadOfBase: 1}
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeSucceeded, Git: git}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	oneBlock(t, fc)
}

// TestReport_SupervisorInline_DurablePush_NotBlocked is the escape valve: an inline-tagged
// node that ACTUALLY pushed durable work (an N2 misclassification of a real code task) is
// NOT blocked — it flows through the normal success/judgment path.
func TestReport_SupervisorInline_DurablePush_NotBlocked(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, inlineInput("e1"))
	git := &executor.FinalizedGitStatus{Probed: true, Pushed: true, Branch: "feat/x", AheadOfBase: 1}
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeSucceeded, Git: git}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	// Durable push escapes the block → normal success path delivers a judgment, no block.
	oneInjection(t, fc)
}

// TestReport_ExecutorFork_Failed_Unchanged: a normal executor_fork failure is UNCHANGED —
// judged (retryable), never auto-blocked (the center-side fruitless_reopens cap bounds it).
func TestReport_ExecutorFork_Failed_Unchanged(t *testing.T) {
	fc := &fakeCenter{}
	in := baseInput("e1")
	in.DispatchMode = executor.DispatchModeExecutorFork
	wb, _ := newWB(t, fc, in)
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeFailed, Error: &executor.ErrorDetail{Kind: "k", Message: "m"}}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	oneInjection(t, fc) // judged, not blocked
}

// TestReport_EmptyMode_Unchanged is the default-no-change lock: with DispatchMode unset
// (legacy / pre-N2), a failure is judged exactly as before — ZERO behavior change.
func TestReport_EmptyMode_Unchanged(t *testing.T) {
	fc := &fakeCenter{}
	wb, _ := newWB(t, fc, baseInput("e1")) // baseInput leaves DispatchMode ""
	if err := wb.Report(context.Background(), executor.Completion{ExecutorID: "e1", Kind: executor.OutcomeFailed, Error: &executor.ErrorDetail{Kind: "k", Message: "m"}}); err != nil {
		t.Fatalf("Report: %v", err)
	}
	oneInjection(t, fc)
}
