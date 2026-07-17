package agentruntime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
	"github.com/oopslink/agent-center/internal/claudestream"
)

// recReporter records the lifecycle + converse-error + activity calls the branch
// tests assert on.
type recReporter struct {
	nopReporter
	mu        sync.Mutex
	lifecycle []string // "state|errMsg"
	converse  []string // "conv|summary"
	activity  []string // eventType
	usage     []UsageReport
}

func (r *recReporter) ReportUsage(_ context.Context, u UsageReport) error {
	r.mu.Lock()
	r.usage = append(r.usage, u)
	r.mu.Unlock()
	return nil
}

func (r *recReporter) usages() []UsageReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]UsageReport(nil), r.usage...)
}

func (r *recReporter) ReportAgentLifecycle(_ context.Context, _, state, errMsg string, _ time.Time) error {
	r.mu.Lock()
	r.lifecycle = append(r.lifecycle, state+"|"+errMsg)
	r.mu.Unlock()
	return nil
}
func (r *recReporter) ReportConverseError(_ context.Context, _, conv, summary string, _ time.Time) error {
	r.mu.Lock()
	r.converse = append(r.converse, conv+"|"+summary)
	r.mu.Unlock()
	return nil
}
func (r *recReporter) ReportAgentActivity(_ context.Context, _, eventType, _, _, _ string, _ time.Time) error {
	r.mu.Lock()
	r.activity = append(r.activity, eventType)
	r.mu.Unlock()
	return nil
}

func (r *recReporter) lifecycles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lifecycle...)
}

// fullRuntime wires the deps onExit needs (reporter, clock, OnFatal). fatal reports
// whether OnFatal fired — the controller-model crash signal (→ process exit → launcher
// rebuild) that replaced the retired in-process self-heal.
func fullRuntime(t *testing.T) (rt *LocalRuntime, rep *recReporter, fatal *bool) {
	t.Helper()
	rep = &recReporter{}
	st := &SessionState{}
	f := false
	fatal = &f
	cfg := LocalRuntimeConfig{
		AgentID:  "agent-x",
		Reporter: rep,
		Log:      func(string, ...any) {},
		Now:      func() time.Time { return time.Unix(1_000_000, 0) },
		OnFatal:  func(string) { f = true },
	}
	return NewLocalRuntime(cfg, st), rep, fatal
}

// --- NotifyWork branches ---

func TestNotifyWork_InjectErrorPropagates(t *testing.T) {
	rt, _ := newTestRuntime(t)
	fs := &fakeSession{}
	fs.closed = true // Inject → error
	rt.withState(func(s *SessionState) { s.Session = fs })
	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-x", TaskID: "wi-1", Brief: "x"}); err == nil {
		t.Fatal("inject error must propagate")
	}
	rt.withState(func(s *SessionState) {
		if s.HadWork || s.CurrentTaskID != "" {
			t.Errorf("failed inject must NOT set work state: hadWork=%v task=%q", s.HadWork, s.CurrentTaskID)
		}
	})
}

func TestNotifyWork_CreatesTaskDir(t *testing.T) {
	home := t.TempDir()
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-x", Reporter: &nopReporter{}, Log: func(string, ...any) {},
		Now: func() time.Time { return time.Unix(1, 0) }, WorkerID: "w-1", AgentHomeBase: home,
		TaskDirManager: taskexec.NewDirManager(),
	}, &SessionState{})
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
	if err := rt.NotifyWork(context.Background(), WorkRequest{AgentID: "agent-x", TaskID: "task-7", Brief: "go"}); err != nil {
		t.Fatalf("NotifyWork: %v", err)
	}
	if msgs := fs.msgs(); len(msgs) != 1 || msgs[0] != "go" {
		t.Fatalf("brief not injected: %v", msgs)
	}
	// The task dir must now exist under <home>/agents/agent-x/tasks/task-7.
	if _, _, _, err := rt.agentPaths("agent-x"); err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
}

// --- NotifyWake / NotifyConverse error branches ---

func TestNotifyWake_NoSessionAndInjectError(t *testing.T) {
	rt, _ := newTestRuntime(t)
	if err := rt.NotifyWake(context.Background(), WakeRequest{AgentID: "agent-x", MessageID: "m"}); err == nil {
		t.Fatal("no session must error")
	}
	fs := &fakeSession{}
	fs.closed = true
	rt.withState(func(s *SessionState) { s.Session = fs })
	if err := rt.NotifyWake(context.Background(), WakeRequest{AgentID: "agent-x", MessageID: "m", MessageText: "hi"}); err == nil {
		t.Fatal("inject error must propagate")
	}
}

func TestNotifyConverse_NoSessionAndInjectErrorAndDedup(t *testing.T) {
	rt, _ := newTestRuntime(t)
	if err := rt.NotifyConverse(context.Background(), ConverseRequest{AgentID: "agent-x", MessageID: "m"}); err == nil {
		t.Fatal("no session must error")
	}
	// Inject-error path.
	fsBad := &fakeSession{}
	fsBad.closed = true
	rt.withState(func(s *SessionState) { s.Session = fsBad })
	if err := rt.NotifyConverse(context.Background(), ConverseRequest{AgentID: "agent-x", ConversationID: "c", MessageID: "m1", MessageText: "hi"}); err == nil {
		t.Fatal("inject error must propagate")
	}
	// Dedup replay: a healthy session, inject once, then replay the SAME id → no-op.
	fs := &fakeSession{}
	rt.withState(func(s *SessionState) { s.Session = fs })
	req := ConverseRequest{AgentID: "agent-x", ConversationID: "c", MessageID: "m2", MessageText: "yo"}
	if err := rt.NotifyConverse(context.Background(), req); err != nil {
		t.Fatalf("converse: %v", err)
	}
	if err := rt.NotifyConverse(context.Background(), req); err != nil {
		t.Fatalf("converse replay: %v", err)
	}
	if got := len(fs.msgs()); got != 1 {
		t.Fatalf("dedup failed: %d injects", got)
	}
}

// --- recordWake FIFO eviction ---

func TestRecordWake_FIFOEviction(t *testing.T) {
	rt, _ := newTestRuntime(t)
	rt.recordWake("first")
	for i := 0; i < WakeDedupCap; i++ {
		rt.recordWake("m" + string(rune('a'+i%26)) + strings.Repeat("x", i))
	}
	// The set is capped and the oldest ("first") was evicted.
	var before int
	rt.withState(func(s *SessionState) {
		if len(s.WakeOrder) != WakeDedupCap {
			t.Errorf("WakeOrder len = %d, want cap %d", len(s.WakeOrder), WakeDedupCap)
		}
		if _, ok := s.WakeSeen["first"]; ok {
			t.Error("oldest entry must be evicted past the cap")
		}
		before = len(s.WakeOrder)
	})
	// Empty id is ignored.
	rt.recordWake("")
	rt.withState(func(s *SessionState) {
		if len(s.WakeOrder) != before {
			t.Error("empty message id must be ignored")
		}
	})
}

// --- onExit three-state coordination ---

func TestOnExit_DetachingReportsNothing(t *testing.T) {
	rt, rep, fatal := fullRuntime(t)
	rt.withState(func(s *SessionState) { s.Detaching = true })
	rt.onExit(nil)
	if len(rep.lifecycles()) != 0 {
		t.Fatalf("detaching exit must report NOTHING (agent stays desired-running), got %v", rep.lifecycles())
	}
	if *fatal {
		t.Fatal("detaching (survival) must NOT fire OnFatal")
	}
}

func TestOnExit_ExpectedStopReportsNothing(t *testing.T) {
	rt, rep, fatal := fullRuntime(t)
	rt.withState(func(s *SessionState) { s.ExpectedStop = true })
	rt.onExit(nil)
	if len(rep.lifecycles()) != 0 {
		t.Fatalf("expected-stop exit must report NOTHING (stop flow owns it), got %v", rep.lifecycles())
	}
	if *fatal {
		t.Fatal("expected-stop must NOT fire OnFatal")
	}
}

func TestOnExit_CodexCrashReportsErrorOnce(t *testing.T) {
	rt, rep, fatal := fullRuntime(t)
	rt.withState(func(s *SessionState) { s.CLI = CLICodex })
	rt.onExit(context.DeadlineExceeded)
	got := rep.lifecycles()
	if len(got) != 1 || !strings.HasPrefix(got[0], "error|") {
		t.Fatalf("codex crash must report error once, got %v", got)
	}
	// codex has no --resume / no restart → must NOT fire OnFatal (no process-exit rebuild).
	if *fatal {
		t.Fatal("codex crash must NOT fire OnFatal")
	}
}

// TestOnExit_ClaudeCrashReportsCrashedAndFiresFatal is the T860 gap4 guard: a claude
// unexpected crash reports "crashed" once and fires OnFatal (→ the agent-runtime process
// exits → the worker launcher rebuilds it with bounded backoff). No in-process self-heal.
func TestOnExit_ClaudeCrashReportsCrashedAndFiresFatal(t *testing.T) {
	rt, rep, fatal := fullRuntime(t)
	rt.withState(func(s *SessionState) {
		s.HadWork = true
		s.CurrentTaskID = "wi-9"
		s.Model = "m-1"
	})
	rt.onExit(context.DeadlineExceeded)
	got := rep.lifecycles()
	if len(got) != 1 || !strings.HasPrefix(got[0], "crashed|") {
		t.Fatalf("claude crash must report 'crashed' once, got %v", got)
	}
	if !*fatal {
		t.Fatal("claude crash must fire OnFatal (→ process exit → launcher rebuild)")
	}
}

// --- surfaceTurnFailure branches ---

func TestSurfaceTurnFailure_Branches(t *testing.T) {
	// (a) in-flight WorkItem → cleared.
	rt, _, _ := fullRuntime(t)
	rt.withState(func(s *SessionState) { s.CurrentTaskID = "wi-1" })
	rt.surfaceTurnFailure("agent-x", claudestream.StreamEvent{Type: "result", IsError: true, Subtype: "boom"})
	if got := rt.CurrentTaskID(); got != "" {
		t.Errorf("failed turn must clear currentTaskID, got %q", got)
	}
	// (b) converse context → surfaceConverseFailure posts a system message + clears.
	rt2, rep2, _ := fullRuntime(t)
	rt2.withState(func(s *SessionState) { s.CurrentConversationID = "c-1" })
	rt2.surfaceTurnFailure("agent-x", claudestream.StreamEvent{Type: "result", IsError: true, Subtype: "bad", Result: "model 404"})
	rt2.withState(func(s *SessionState) {
		if s.CurrentConversationID != "" {
			t.Error("failed converse turn must clear currentConversationID")
		}
	})
	if len(rep2.converse) != 1 || !strings.Contains(rep2.converse[0], "c-1|bad") {
		t.Fatalf("must post a converse-error system message, got %v", rep2.converse)
	}
	// (c) idle (no work, no conv) → just logged, no report.
	rt3, rep3, _ := fullRuntime(t)
	rt3.surfaceTurnFailure("agent-x", claudestream.StreamEvent{Type: "result", IsError: true})
	if len(rep3.converse) != 0 {
		t.Fatal("idle is_error must not post a converse message")
	}
}

// --- reportRecovered / reportLifecycleOnce ---

func TestReportRecoveredAndLifecycleOnce(t *testing.T) {
	rt, rep, _ := fullRuntime(t)
	rt.reportRecovered()
	if got := rep.lifecycles(); len(got) != 1 || got[0] != "running|" {
		t.Fatalf("reportRecovered must report running, got %v", got)
	}
	// ReportLifecycleOnce (daemon-facing entry used by reconcile/reset settle) fires
	// EXACTLY once per instance.
	rt.ReportLifecycleOnce(context.Background(), "stopped", "")
	rt.ReportLifecycleOnce(context.Background(), "stopped", "")
	if got := rep.lifecycles(); len(got) != 2 {
		t.Fatalf("lifecycle once must fire exactly one 'stopped' (total 2 incl running), got %v", got)
	}
}

// --- maybeReportUsage branches ---

func TestMaybeReportUsage_Branches(t *testing.T) {
	rt, rep, _ := fullRuntime(t)

	// Empty turn (no tokens) → skipped: NOT reported.
	rt.withState(func(s *SessionState) { s.Model = "m-1" })
	rt.maybeReportUsage("agent-x", claudestream.StreamEvent{Type: "result"}, "t-1")
	if got := len(rep.usages()); got != 0 {
		t.Fatalf("empty turn must not report usage, got %d", got)
	}

	// Tokens present but no model → skipped: NOT reported.
	rt.withState(func(s *SessionState) { s.Model = "" })
	rt.maybeReportUsage("agent-x", claudestream.StreamEvent{Type: "result", TokensIn: 5}, "t-1")
	if got := len(rep.usages()); got != 0 {
		t.Fatalf("no-model turn must not report usage, got %d", got)
	}

	// Model + tokens → reported exactly once with the turn's tokens attributed to the task.
	rt.withState(func(s *SessionState) { s.Model = "m-2" })
	rt.maybeReportUsage("agent-x", claudestream.StreamEvent{Type: "result", TokensIn: 10, TokensOut: 3, CacheReadTokens: 2}, "t-2")
	us := rep.usages()
	if len(us) != 1 {
		t.Fatalf("model+tokens must report once, got %d", len(us))
	}
	u := us[0]
	if u.AgentID != "agent-x" || u.Model != "m-2" || u.TaskID != "t-2" ||
		u.InputTokens != 10 || u.OutputTokens != 3 || u.CacheReadTokens != 2 {
		t.Fatalf("usage misattributed: %+v", u)
	}
}

// --- pure policy edge cases ---

func TestDecideRateLimitResume_Clamps(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	// retry_after wins, floored to min.
	if got := DecideRateLimitResume(1, 0, now, 60*time.Second, 5*time.Second, time.Hour); got != 5*time.Second {
		t.Fatalf("tiny retry_after must floor to min, got %s", got)
	}
	// resets_at relative.
	if got := DecideRateLimitResume(0, now.Unix()+45, now, 60*time.Second, 5*time.Second, time.Hour); got != 45*time.Second {
		t.Fatalf("resets_at relative, got %s", got)
	}
	// no window → default; huge → capped.
	if got := DecideRateLimitResume(0, 0, now, 60*time.Second, 5*time.Second, time.Hour); got != 60*time.Second {
		t.Fatalf("no window default, got %s", got)
	}
	if got := DecideRateLimitResume(100000, 0, now, 60*time.Second, 5*time.Second, time.Hour); got != time.Hour {
		t.Fatalf("huge must cap, got %s", got)
	}
	// zero durations fall back to the package defaults (min floor applies).
	if got := DecideRateLimitResume(1, 0, now, 0, 0, 0); got != DefaultRateLimitMinBackoff {
		t.Fatalf("zero params must use default min floor, got %s", got)
	}
}

func TestDecideAPIErrorBackoff_Edges(t *testing.T) {
	if got := DecideAPIErrorBackoff(0, 2*time.Second, 60*time.Second); got != 2*time.Second {
		t.Fatalf("attempt<1 floored to first, got %s", got)
	}
	if got := DecideAPIErrorBackoff(99, 2*time.Second, 60*time.Second); got != 60*time.Second {
		t.Fatalf("large attempt must cap, got %s", got)
	}
	// zero params → package defaults.
	if got := DecideAPIErrorBackoff(1, 0, 0); got != DefaultAPIErrorBackoffBase {
		t.Fatalf("zero base must default, got %s", got)
	}
}

func TestTaskIDFromStartTool_Variants(t *testing.T) {
	if got := TaskIDFromStartTool("mcp__agent-center__start_task", []byte(`{"task_id":"task-1"}`)); got != "task-1" {
		t.Fatalf("start_task task_id, got %q", got)
	}
	if got := TaskIDFromStartTool("claim_task", []byte(`{"task_id":"task-2"}`)); got != "task-2" {
		t.Fatalf("claim_task task_id, got %q", got)
	}
	if got := TaskIDFromStartTool("complete_task", []byte(`{"task_id":"task-3"}`)); got != "" {
		t.Fatalf("non-start tool must yield empty, got %q", got)
	}
	if got := TaskIDFromStartTool("start_task", []byte(`not json`)); got != "" {
		t.Fatalf("bad json must yield empty, got %q", got)
	}
	if got := TaskIDFromStartTool("start_task", nil); got != "" {
		t.Fatalf("empty input must yield empty, got %q", got)
	}
	if !IsTaskTerminalTool("mcp__agent-center__discard_task") || IsTaskTerminalTool("start_task") {
		t.Fatal("IsTaskTerminalTool classification wrong")
	}
}

func TestConverseErrorSummary(t *testing.T) {
	if got := converseErrorSummary(claudestream.StreamEvent{}); got != "error" {
		t.Fatalf("empty subtype → error, got %q", got)
	}
	long := strings.Repeat("z", 300)
	got := converseErrorSummary(claudestream.StreamEvent{Subtype: "sub", Result: long})
	if !strings.HasPrefix(got, "sub: ") || !strings.HasSuffix(got, "…") || len(got) > 220 {
		t.Fatalf("long result must be truncated with ellipsis, got len=%d %q", len(got), got[:20])
	}
}

func TestMessageDeliveredPayload_Truncates(t *testing.T) {
	long := strings.Repeat("q", 250)
	p := messageDeliveredPayload(ConverseRequest{ConversationID: "c", MessageID: "m", MessageText: long, AttachmentCount: 2})
	if !strings.Contains(p, `"conversation_id":"c"`) || !strings.Contains(p, `"attachments_count":2`) {
		t.Fatalf("payload missing fields: %s", p)
	}
	// content_preview must be truncated to 200 runes.
	if strings.Count(p, "q") > 200 {
		t.Fatalf("content_preview must truncate to 200 runes, got %d", strings.Count(p, "q"))
	}
}

func TestBuildConverseBrief_Branches(t *testing.T) {
	// DM.
	dm := BuildConverseBrief(ConverseRequest{ConversationID: "c", SenderDisplay: "Ada", MessageText: "hi"})
	if !strings.Contains(dm, "[Direct message from Ada]") || !strings.Contains(dm, "conversation, not a task") {
		t.Fatalf("dm brief wrong: %s", dm)
	}
	// Channel.
	ch := BuildConverseBrief(ConverseRequest{ConversationID: "c", ConvKind: "channel", ConvName: "general", SenderRef: "u:1", MessageText: "hi"})
	if !strings.Contains(ch, "[Channel #general]") {
		t.Fatalf("channel brief wrong: %s", ch)
	}
	// Owner-anchored PLAN keeps the conversation-note; non-plan (task) drops it.
	plan := BuildConverseBrief(ConverseRequest{OwnerRef: "pm://plans/plan-abc", ConvName: "P", SenderDisplay: "x", ConversationID: "c", MessageText: "hi"})
	if !strings.Contains(plan, "belongs to") || !strings.Contains(plan, "conversation, not a task") {
		t.Fatalf("plan-anchored brief wrong: %s", plan)
	}
	task := BuildConverseBrief(ConverseRequest{OwnerRef: "pm://tasks/task-9", ConvName: "T", SenderDisplay: "x", ConversationID: "c", MessageText: "hi"})
	if !strings.Contains(task, "belongs to") || strings.Contains(task, "conversation, not a task") {
		t.Fatalf("task-anchored brief must drop the conv-note: %s", task)
	}
	// Thread reply-hint + attachments.
	thr := BuildConverseBrief(ConverseRequest{ConversationID: "c", SenderDisplay: "x", MessageText: "hi", RootMessageID: "root-1", AttachmentCount: 2})
	if !strings.Contains(thr, "parent_message_id=\"root-1\"") || !strings.Contains(thr, "2 file attachments") {
		t.Fatalf("thread+attachments brief wrong: %s", thr)
	}
	one := BuildConverseBrief(ConverseRequest{ConversationID: "c", SenderDisplay: "x", MessageText: "hi", AttachmentCount: 1})
	if !strings.Contains(one, "1 file attachment") || strings.Contains(one, "1 file attachments") {
		t.Fatalf("single attachment noun wrong: %s", one)
	}
}

// --- WriteMCPConfig / agentPaths error branches ---

func TestWriteMCPConfig_Branches(t *testing.T) {
	if p, err := WriteMCPConfig("", nil); p != "" || err != nil {
		t.Fatalf("empty bytes → (\"\", nil), got (%q, %v)", p, err)
	}
	if _, err := WriteMCPConfig("", []byte("x")); err == nil {
		t.Fatal("empty home with bytes must error")
	}
	home := t.TempDir()
	p, err := WriteMCPConfig(home, []byte(`{"mcpServers":{}}`))
	if err != nil || p == "" {
		t.Fatalf("valid write failed: p=%q err=%v", p, err)
	}
}

func TestAgentPaths_Errors(t *testing.T) {
	base := NewLocalRuntime(LocalRuntimeConfig{Reporter: &nopReporter{}}, &SessionState{})
	if _, _, _, err := base.agentPaths("a"); err == nil {
		t.Fatal("missing AgentHomeBase must error")
	}
	r2 := NewLocalRuntime(LocalRuntimeConfig{Reporter: &nopReporter{}, AgentHomeBase: "/tmp"}, &SessionState{})
	if _, _, _, err := r2.agentPaths("a"); err == nil {
		t.Fatal("missing WorkerID must error")
	}
	r3 := NewLocalRuntime(LocalRuntimeConfig{Reporter: &nopReporter{}, AgentHomeBase: "/tmp", WorkerID: "w"}, &SessionState{})
	if _, _, _, err := r3.agentPaths(""); err == nil {
		t.Fatal("missing agentID must error")
	}
	if home, _, _, err := r3.agentPaths("a"); err != nil || home == "" {
		t.Fatalf("valid agentPaths failed: %v", err)
	}
}
