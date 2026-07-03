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

// TestDecideSelfHeal_Curve pins the exported pure crash→action policy.
func TestDecideSelfHeal_Curve(t *testing.T) {
	p := SelfHealParams{MaxAttempts: 5, BackoffBase: time.Second, BackoffCap: 30 * time.Second, ResetWindow: 60 * time.Second}
	base := time.Unix(1_000_000, 0)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i := 0; i < 5; i++ {
		failed, count, backoff := DecideSelfHeal(i, base, base.Add(time.Second), p)
		if failed || count != i+1 || backoff != want[i] {
			t.Fatalf("crash #%d: failed=%v count=%d backoff=%s (want %s)", i+1, failed, count, backoff, want[i])
		}
	}
	if failed, count, _ := DecideSelfHeal(5, base, base.Add(time.Second), p); !failed || count != 6 {
		t.Fatalf("6th crash must circuit-break: failed=%v count=%d", failed, count)
	}
}

// TestSelfHealStore_RecordAndDrain pins record → due-drain → circuit-break through the
// shared store (the daemon's OnTick drives this).
func TestSelfHealStore_RecordAndDrain(t *testing.T) {
	var mu sync.Mutex
	now := time.Unix(1_000_000, 0)
	store := NewSelfHealStore(&mu, SelfHealParams{MaxAttempts: 2, BackoffBase: time.Second}, nil)

	if got := store.RecordCrashAndSchedule(RelaunchSpec{AgentID: "a", Version: 1}, now, "boom"); got != "error" {
		t.Fatalf("first crash state = %q, want error", got)
	}
	// Not yet due.
	if dues := store.DrainDue(now, func(string) bool { return false }); len(dues) != 0 {
		t.Fatalf("relaunch fired before backoff: %d", len(dues))
	}
	// Due after backoff → one relaunch spec.
	dues := store.DrainDue(now.Add(2*time.Second), func(string) bool { return false })
	if len(dues) != 1 || dues[0].AgentID != "a" {
		t.Fatalf("expected one due relaunch, got %+v", dues)
	}
	// A live session drops the pending relaunch.
	store.RecordCrashAndSchedule(RelaunchSpec{AgentID: "a", Version: 1}, now.Add(3*time.Second), "boom2")
	if dues := store.DrainDue(now.Add(30*time.Second), func(string) bool { return true }); len(dues) != 0 {
		t.Fatalf("live session must drop the relaunch, got %+v", dues)
	}
	// crashCount is 2 now (boom + boom2; the live-drop did not advance it). MaxAttempts
	// 2 → the 3rd crash circuit-breaks to terminal "failed".
	if got := store.RecordCrashAndSchedule(RelaunchSpec{AgentID: "a"}, now.Add(40*time.Second), "boom3"); got != "failed" {
		t.Fatalf("cap must circuit-break, got %q", got)
	}
	if _, failed, present := store.EntryForTest("a"); !present || !failed {
		t.Fatalf("entry must be terminal-failed")
	}
	store.Clear("a")
	if _, _, present := store.EntryForTest("a"); present {
		t.Fatal("Clear must drop the entry")
	}
}
