package agentruntime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/runtimefs"
)

// fakeSession is a test Session: it records injects and drives OnExit/OnEvent.
type fakeSession struct {
	mu       sync.Mutex
	injected []string
	closed   bool
}

func (f *fakeSession) Inject(_ context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return context.Canceled
	}
	f.injected = append(f.injected, msg)
	return nil
}
func (f *fakeSession) Stop(context.Context) error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}
func (f *fakeSession) Detach() { f.mu.Lock(); f.closed = true; f.mu.Unlock() }
func (f *fakeSession) msgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.injected...)
}

// nopReporter satisfies Reporter with recording where the tests assert.
type nopReporter struct {
	mu       sync.Mutex
	markSeen int
}

func (r *nopReporter) ReportAgentActivity(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}
func (r *nopReporter) ReportAgentLifecycle(context.Context, string, string, string, time.Time) error {
	return nil
}
func (r *nopReporter) ReportMarkSeen(context.Context, string, string, string, time.Time) error {
	r.mu.Lock()
	r.markSeen++
	r.mu.Unlock()
	return nil
}
func (r *nopReporter) ReportConverseError(context.Context, string, string, string, time.Time) error {
	return nil
}
func (r *nopReporter) FetchReplyNudges(context.Context, string) ([]string, error) { return nil, nil }
func (r *nopReporter) ReportUsage(context.Context, UsageReport) error             { return nil }
func (r *nopReporter) RenewTaskLease(context.Context, string, string, time.Time) error {
	return nil
}
func (r *nopReporter) ReportRuntimeFsResponse(context.Context, runtimefs.Response) error { return nil }

var _ Reporter = (*nopReporter)(nil)

func newTestRuntime(t *testing.T) (*LocalRuntime, *SessionState, *nopReporter) {
	t.Helper()
	rep := &nopReporter{}
	st := &SessionState{}
	cfg := LocalRuntimeConfig{
		AgentID:  "agent-x",
		Reporter: rep,
		Log:      func(string, ...any) {},
	}
	return NewLocalRuntime(cfg, st), st, rep
}

// TestNotifyWork_InjectsAndSetsState pins the wired NotifyWork inject path: with a
// live session it injects the brief and records the in-flight work.
func TestNotifyWork_InjectsAndSetsState(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	fs := &fakeSession{}
	st.Session = fs

	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-x", TaskID: "wi-1", Brief: "do it"}); err != nil {
		t.Fatalf("NotifyWork: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] != "do it" {
		t.Fatalf("brief not injected: %v", msgs)
	}
	if !st.HadWork || st.CurrentTaskID != "wi-1" {
		t.Fatalf("work state not set: hadWork=%v task=%q", st.HadWork, st.CurrentTaskID)
	}
}

// TestNotifyWork_NoSessionRetries pins the delivery-race policy: no live session → error.
func TestNotifyWork_NoSessionRetries(t *testing.T) {
	rt, _, _ := newTestRuntime(t)
	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-x", TaskID: "wi-1"}); err == nil {
		t.Fatal("expected an error when no running session")
	}
}

// TestNotifyWake_DedupAndMarkSeen pins wake inject + dedup + mark-seen.
func TestNotifyWake_DedupAndMarkSeen(t *testing.T) {
	rt, st, rep := newTestRuntime(t)
	fs := &fakeSession{}
	st.Session = fs
	req := WakeRequest{AgentID: "agent-x", TaskID: "wi-1", ConversationID: "c1", MessageID: "m1", MessageText: "hi"}
	if err := rt.NotifyWake(context.Background(), req); err != nil {
		t.Fatalf("NotifyWake: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] != "hi" {
		t.Fatalf("wake not injected: %v", msgs)
	}
	// Replay of the same message id → dedup no-op (no second inject).
	if err := rt.NotifyWake(context.Background(), req); err != nil {
		t.Fatalf("NotifyWake replay: %v", err)
	}
	if got := len(fs.msgs()); got != 1 {
		t.Fatalf("dedup failed: %d injects", got)
	}
	rep.mu.Lock()
	seen := rep.markSeen
	rep.mu.Unlock()
	if seen != 1 {
		t.Fatalf("mark-seen calls = %d, want 1", seen)
	}
}

// TestNotifyConverse_InjectsBrief pins converse builds + injects the brief and sets
// the conversation context (clearing any work context).
func TestNotifyConverse_InjectsBrief(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	fs := &fakeSession{}
	st.Session = fs
	st.CurrentTaskID = "wi-stale"
	err := rt.NotifyConverse(context.Background(), ConverseRequest{
		AgentID: "agent-x", ConversationID: "c1", MessageID: "m1", SenderDisplay: "Ada", MessageText: "hello",
	})
	if err != nil {
		t.Fatalf("NotifyConverse: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] == "" {
		t.Fatalf("converse brief not injected: %v", msgs)
	}
	if st.CurrentConversationID != "c1" || st.CurrentTaskID != "" {
		t.Fatalf("converse context not set / work not cleared: conv=%q task=%q", st.CurrentConversationID, st.CurrentTaskID)
	}
}
