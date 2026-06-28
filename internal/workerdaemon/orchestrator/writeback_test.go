package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
)

var wbNow = time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

// fakeCenter records the writeback's center calls.
type fakeCenter struct {
	mu        sync.Mutex
	completes [][3]string // agentID, taskID, summary
	blocks    [][4]string // agentID, taskID, reason, reasonType
	posts     [][3]string // agentID, conversationID, content
	err       error       // returned by every call when set
}

func (f *fakeCenter) CompleteTask(_ context.Context, a, t, s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completes = append(f.completes, [3]string{a, t, s})
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

func TestReport_Succeeded_CompletesTask(t *testing.T) {
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
	if len(fc.completes) != 1 {
		t.Fatalf("want 1 complete, got %v", fc.completes)
	}
	got := fc.completes[0]
	if got[0] != "agent-x" || got[1] != "task-1" || got[2] != "built and tested" {
		t.Errorf("complete args = %v", got)
	}
	if len(fc.blocks) != 0 || len(fc.posts) != 0 {
		t.Errorf("unexpected block/post calls")
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
	if fc.completes[0][2] != "the result" {
		t.Errorf("summary = %q want result fallback", fc.completes[0][2])
	}

	// Neither summary nor result → falls back to goal title text.
	fc2 := &fakeCenter{}
	in2 := baseInput("exec-3")
	wb2, _ := newWB(t, fc2, in2)
	c2 := executor.Completion{ExecutorID: "exec-3", Kind: executor.OutcomeSucceeded}
	if err := wb2.Report(context.Background(), c2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fc2.completes[0][2], "do thing") {
		t.Errorf("summary = %q want goal title fallback", fc2.completes[0][2])
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
	if len(fc.blocks) != 1 {
		t.Fatalf("want 1 block, got %v", fc.blocks)
	}
	b := fc.blocks[0]
	if b[1] != "task-1" || b[3] != "obstacle" {
		t.Errorf("block args = %v", b)
	}
	if !strings.Contains(b[2], "runner_failed") || !strings.Contains(b[2], "boom") || !strings.Contains(b[2], "failed") {
		t.Errorf("block reason = %q", b[2])
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
	if len(fc.blocks) != 1 || !strings.Contains(fc.blocks[0][2], "crashed (retryable)") {
		t.Errorf("crash block = %v", fc.blocks)
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
	if !strings.Contains(fc.blocks[0][2], "no error detail") {
		t.Errorf("reason = %q", fc.blocks[0][2])
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

func TestReport_CenterError_Propagates(t *testing.T) {
	fc := &fakeCenter{err: errors.New("center down")}
	wb, _ := newWB(t, fc, baseInput("exec-11"))
	err := wb.Report(context.Background(), executor.Completion{
		ExecutorID: "exec-11", Kind: executor.OutcomeSucceeded,
		Status: &executor.Status{ExecutorID: "exec-11", State: executor.StateDone, Summary: "s", StartedAt: wbNow},
	})
	if err == nil || !strings.Contains(err.Error(), "complete_task") {
		t.Fatalf("want complete_task error, got %v", err)
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
	if len(fc.completes) != n {
		t.Errorf("want %d completes, got %d", n, len(fc.completes))
	}
}
