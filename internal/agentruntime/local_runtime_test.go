package agentruntime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/runtimefs"
)

// fakeSession is a test Session: it records injects and drives OnExit/OnEvent.
type fakeSession struct {
	mu        sync.Mutex
	injected  []string
	closed    bool
	injectErr error
}

func (f *fakeSession) Inject(_ context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return context.Canceled
	}
	if f.injectErr != nil {
		return f.injectErr
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

// newTestRuntime returns the runtime WITHOUT its *SessionState: reach the state via
// rt.withState so the shared-mutex contract holds by construction (docs/rules/testing.md § 6.3).
func newTestRuntime(t *testing.T) (*LocalRuntime, *nopReporter) {
	t.Helper()
	rep := &nopReporter{}
	st := &SessionState{}
	cfg := LocalRuntimeConfig{
		AgentID:  "agent-x",
		Reporter: rep,
		Log:      func(string, ...any) {},
	}
	return NewLocalRuntime(cfg, st), rep
}

// TestNotifyWork_InjectsAndSetsState pins the wired NotifyWork inject path: with a
// live session it injects the brief and records the in-flight work.
func TestNotifyWork_InjectsAndSetsState(t *testing.T) {
	rt, _ := newTestRuntime(t)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })

	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-x", TaskID: "wi-1", Brief: "do it"}); err != nil {
		t.Fatalf("NotifyWork: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] != "do it" {
		t.Fatalf("brief not injected: %v", msgs)
	}
	rt.withState(func(s *SessionState) {
		if !s.HadWork || s.CurrentTaskID != "wi-1" {
			t.Errorf("work state not set: hadWork=%v task=%q", s.HadWork, s.CurrentTaskID)
		}
	})
}

// TestNotifyWork_NoSessionRetries pins the delivery-race policy: no live session → error.
func TestNotifyWork_NoSessionRetries(t *testing.T) {
	rt, _ := newTestRuntime(t)
	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-x", TaskID: "wi-1"}); err == nil {
		t.Fatal("expected an error when no running session")
	}
}

func TestNotifyConverse_SessionClosedSignalsFatal(t *testing.T) {
	rep := &nopReporter{}
	st := &SessionState{Session: &fakeSession{injectErr: ErrSessionClosed}}
	fatalCh := make(chan string, 1)
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID:  "agent-x",
		Reporter: rep,
		Log:      func(string, ...any) {},
		OnFatal:  func(reason string) { fatalCh <- reason },
	}, st)

	err := rt.NotifyConverse(context.Background(), ConverseRequest{
		AgentID:        "agent-x",
		ConversationID: "c1",
		MessageID:      "m1",
		MessageText:    "hi",
	})
	if err == nil {
		t.Fatal("expected inject error")
	}
	select {
	case got := <-fatalCh:
		if !strings.Contains(got, ErrSessionClosed.Error()) {
			t.Fatalf("fatal reason = %q, want session closed", got)
		}
	default:
		t.Fatal("expected fatal signal for closed session")
	}
}

// TestNotifyWake_DedupAndMarkSeen pins wake inject + dedup + mark-seen.
func TestNotifyWake_DedupAndMarkSeen(t *testing.T) {
	rt, rep := newTestRuntime(t)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
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
	rt, _ := newTestRuntime(t)
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) {
		s.Session = fs
		s.CurrentTaskID = "wi-stale"
	})
	err := rt.NotifyConverse(context.Background(), ConverseRequest{
		AgentID: "agent-x", ConversationID: "c1", MessageID: "m1", SenderDisplay: "Ada", MessageText: "hello",
	})
	if err != nil {
		t.Fatalf("NotifyConverse: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] == "" {
		t.Fatalf("converse brief not injected: %v", msgs)
	}
	rt.withState(func(s *SessionState) {
		if s.CurrentConversationID != "c1" || s.CurrentTaskID != "" {
			t.Errorf("converse context not set / work not cleared: conv=%q task=%q", s.CurrentConversationID, s.CurrentTaskID)
		}
	})
}
