package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// recordingReporter is a fake feedbackReporter that records every call so
// assertions can inspect activity / lifecycle feedback. Safe for concurrent use
// (OnEvent/OnExit fire on the session reader goroutine).
type recordingReporter struct {
	mu sync.Mutex

	activities   []activityCall
	lifecycles   []lifecycleCall
	markSeens    []markSeenCall
	converseErrs []converseErrCall
}

type activityCall struct {
	agentID, eventType, payload, taskRef, interactionRef string
}
type lifecycleCall struct {
	agentID, state, errMsg string
}
type markSeenCall struct {
	agentID, conversationID, messageID string
}
type converseErrCall struct {
	agentID, conversationID, summary string
}

func (r *recordingReporter) ReportAgentActivity(_ context.Context, agentID, eventType, payloadJSON, taskRef, interactionRef string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities = append(r.activities, activityCall{agentID, eventType, payloadJSON, taskRef, interactionRef})
	return nil
}

func (r *recordingReporter) ReportAgentLifecycle(_ context.Context, agentID, state, errMsg string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lifecycles = append(r.lifecycles, lifecycleCall{agentID, state, errMsg})
	return nil
}

func (r *recordingReporter) ReportMarkSeen(_ context.Context, agentID, conversationID, messageID string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markSeens = append(r.markSeens, markSeenCall{agentID, conversationID, messageID})
	return nil
}

func (r *recordingReporter) ReportConverseError(_ context.Context, agentID, conversationID, summary string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.converseErrs = append(r.converseErrs, converseErrCall{agentID, conversationID, summary})
	return nil
}

func (r *recordingReporter) converseErrCalls() []converseErrCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]converseErrCall, len(r.converseErrs))
	copy(out, r.converseErrs)
	return out
}

func (r *recordingReporter) markSeenCalls() []markSeenCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]markSeenCall, len(r.markSeens))
	copy(out, r.markSeens)
	return out
}

func (r *recordingReporter) lifecycleCalls() []lifecycleCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]lifecycleCall, len(r.lifecycles))
	copy(out, r.lifecycles)
	return out
}

func (r *recordingReporter) activityCalls() []activityCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]activityCall, len(r.activities))
	copy(out, r.activities)
	return out
}

var _ feedbackReporter = (*recordingReporter)(nil)

// fakeSession is the TEST-ONLY agentSession (PM s3b-2b test seam). It records
// Inject/Stop/Detach and lets the test drive the OnEvent/OnExit callbacks the real
// SupervisorSession's event-pump would fire — WITHOUT spawning a supervisor or
// claude. It exists ONLY in _test.go and is NEVER wired in a production path (the
// production starter is startSupervisorSessionAdapter → real supervisor spawn);
// it is a test artifact, not a session, so it does not weaken grep-clean ownership.
type fakeSession struct {
	cfg SupervisorSessionConfig

	mu       sync.Mutex
	injected []string
	stopped  bool
	detached bool
	exited   bool // OnExit fired (once)
}

// Inject records the RAW message (the controller passes the brief/merged text;
// the stream-json wire encoding is the supervisor's job, not the controller's).
func (f *fakeSession) Inject(_ context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped || f.detached {
		return ErrSessionClosed
	}
	f.injected = append(f.injected, msg)
	return nil
}

// Stop is the explicit-terminate path: mark stopped + fire OnExit(nil) ONCE
// (mirrors the real Stop, which SIGTERMs the supervisor then joins the pump →
// OnExit). The controller's stopSession blocks on this, so firing synchronously
// keeps tests deterministic.
func (f *fakeSession) Stop(_ context.Context) error {
	f.fireExit(true /*viaStop*/, nil)
	return nil
}

// Detach is the survival path: mark detached + fire OnExit(nil) ONCE (mirrors the
// real Detach, which closes the socket without signalling, then joins the pump).
func (f *fakeSession) Detach() { f.fireExit(false /*viaStop*/, nil) }

func (f *fakeSession) fireExit(viaStop bool, err error) {
	f.mu.Lock()
	if f.exited {
		f.mu.Unlock()
		return
	}
	f.exited = true
	if viaStop {
		f.stopped = true
	} else {
		f.detached = true
	}
	cb := f.cfg.OnExit
	f.mu.Unlock()
	if cb != nil {
		cb(err)
	}
}

// emit drives a parsed StreamEvent through OnEvent (the controller maps it to a
// ReportAgentActivity call). The raw claude-line → StreamEvent parsing is covered
// in the claudestream + supervisor_session tests (and Tester's real-claude GATE),
// so the controller test asserts only the onEvent→activity mapping.
func (f *fakeSession) emit(ev claudestream.StreamEvent) {
	f.mu.Lock()
	cb := f.cfg.OnEvent
	f.mu.Unlock()
	if cb != nil {
		cb(ev)
	}
}

// crash drives an UNEXPECTED OnExit(err) (the supervisor/claude died while
// desired=running) — without Stop/Detach, so onExit takes the crash branch.
func (f *fakeSession) crash(err error) { f.fireExit(false /*viaStop*/, err) }

func (f *fakeSession) injectedMsgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.injected))
	copy(out, f.injected)
	return out
}

// recordingStarter is the TEST-ONLY sessionStarter: it hands out a fresh
// fakeSession per start (recording the config so epoch/workspace plumbing can be
// asserted) and counts starts so restart-vs-replay assertions work. nextErr makes
// the next start fail (spawn-failure path).
type recordingStarter struct {
	mu       sync.Mutex
	sessions []*fakeSession
	nextErr  error
}

func (s *recordingStarter) start(_ context.Context, cfg SupervisorSessionConfig) (agentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextErr != nil {
		err := s.nextErr
		s.nextErr = nil
		return nil, err
	}
	fs := &fakeSession{cfg: cfg}
	s.sessions = append(s.sessions, fs)
	return fs, nil
}

func (s *recordingStarter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func (s *recordingStarter) last() *fakeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) == 0 {
		return nil
	}
	return s.sessions[len(s.sessions)-1]
}

// all returns a snapshot of every started session (for asserting per-agent
// relaunch when boot reconcile starts more than one).
func (s *recordingStarter) all() []*fakeSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*fakeSession(nil), s.sessions...)
}

// newTestController builds a controller with the recording reporter + the
// TEST-ONLY fake session starter (no real supervisor spawn), rooted at a temp
// AgentHomeBase.
func newTestController(t *testing.T, base string) (*AgentController, *recordingReporter, *recordingStarter) {
	t.Helper()
	rep := &recordingReporter{}
	rs := &recordingStarter{}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter:      rep,
		WorkerID:      "w-1",
		AdminURL:      "unix:/tmp/admin.sock",
		WorkerToken:   "tok",
		BinaryPath:    "agent-center",
		AgentHomeBase: base,
		StopGrace:     50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Inject the fake starter via the unexported seam (same-package test only).
	c.cfg.starter = rs.start
	return c, rep, rs
}

func reconcileCmd(t *testing.T, agentID, desired string, version int, scope string, offset int64) ControlCommand {
	t.Helper()
	pl := reconcilePayload{AgentID: agentID, DesiredLifecycle: desired, Version: version, ResetScope: scope}
	return ControlCommand{
		ID:          "cmd-r",
		Offset:      offset,
		CommandType: cmdTypeAgentReconcile,
		Payload:     mustJSON(t, pl),
	}
}

func workCmd(t *testing.T, agentID, workItemID, brief string, offset int64) ControlCommand {
	t.Helper()
	pl := workPayload{AgentID: agentID, TaskID: workItemID, Brief: brief}
	return ControlCommand{
		ID:          "cmd-w",
		Offset:      offset,
		CommandType: cmdTypeAgentWork,
		Payload:     mustJSON(t, pl),
	}
}

func wakeCmd(t *testing.T, agentID, workItemID, messageID, messageText string, offset int64) ControlCommand {
	t.Helper()
	return wakeCmdConv(t, agentID, workItemID, "", messageID, messageText, offset)
}

// wakeCmdConv builds an agent.wake command carrying a conversation_id (D2-e-ii:
// the controller advances the read-state cursor via ReportMarkSeen after inject).
func wakeCmdConv(t *testing.T, agentID, workItemID, conversationID, messageID, messageText string, offset int64) ControlCommand {
	t.Helper()
	pl := wakePayload{
		AgentID: agentID, TaskID: workItemID, ConversationID: conversationID,
		MessageID: messageID, MessageText: messageText,
	}
	return ControlCommand{
		ID:          "cmd-wake",
		Offset:      offset,
		CommandType: cmdTypeAgentWake,
		Payload:     mustJSON(t, pl),
	}
}

// converseCmd builds an agent.converse command (DM/channel reply, no WorkItem).
func converseCmd(t *testing.T, agentID, conversationID, messageID, messageText string, offset int64) ControlCommand {
	t.Helper()
	pl := conversePayload{
		AgentID: agentID, ConversationID: conversationID, ConvKind: "dm",
		SenderDisplay: "alice", MessageID: messageID, MessageText: messageText,
	}
	return ControlCommand{
		ID:          "cmd-converse",
		Offset:      offset,
		CommandType: cmdTypeAgentConverse,
		Payload:     mustJSON(t, pl),
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAgentController_ReconcileRunning_StartsAndStreamsActivity(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	if rs.count() != 1 {
		t.Fatalf("want 1 session start, got %d", rs.count())
	}

	// Drive PARSED StreamEvents (what the supervisor's event-pump delivers to
	// OnEvent) and assert each maps to a ReportAgentActivity with event_type =
	// StreamEvent.Type and a meaningful payload. The raw claude-line → StreamEvent
	// parsing is covered by the claudestream + supervisor_session tests (and
	// Tester's real-claude GATE); here we test only onEvent→activity (stdout→
	// activity, NEVER Conversation).
	fs := rs.last()
	fs.emit(claudestream.StreamEvent{Type: "system", Subtype: "init"})
	fs.emit(claudestream.StreamEvent{Type: "thinking", Text: "PONG thinking"})
	fs.emit(claudestream.StreamEvent{Type: "assistant_text", Text: "PONG"})
	fs.emit(claudestream.StreamEvent{Type: "tool_use", ToolName: "Bash", ToolUseID: "tu-1"})
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "success", Result: "ok"})

	acts := rep.activityCalls()
	if len(acts) != 5 {
		t.Fatalf("want 5 activity calls (one per emitted event), got %d: %+v", len(acts), acts)
	}
	byType := map[string]activityCall{}
	for _, a := range acts {
		if a.agentID != "agent-1" {
			t.Fatalf("activity for wrong agent: %+v", a)
		}
		// stdout→activity must carry NO conversation/interaction ref (not a Conversation post).
		if a.interactionRef != "" {
			t.Fatalf("activity must not carry an interaction ref (stdout is activity, not Conversation): %+v", a)
		}
		byType[a.eventType] = a
	}

	if a, ok := byType["thinking"]; !ok {
		t.Fatalf("no thinking activity: %+v", acts)
	} else if !strings.Contains(a.payload, "PONG") {
		t.Fatalf("thinking payload missing text: %s", a.payload)
	}
	if a, ok := byType["assistant_text"]; !ok {
		t.Fatalf("no assistant_text activity: %+v", acts)
	} else if !strings.Contains(a.payload, "PONG") {
		t.Fatalf("assistant_text payload missing PONG: %s", a.payload)
	}
	if a, ok := byType["result"]; !ok {
		t.Fatalf("no result activity: %+v", acts)
	} else if !strings.Contains(a.payload, "success") {
		t.Fatalf("result payload missing subtype: %s", a.payload)
	}
	// v2.7.1 #216: the claude session-init system line is reported as "system_init".
	if _, ok := byType["system_init"]; !ok {
		t.Fatalf("no system_init activity: %+v", acts)
	}
	if a, ok := byType["tool_use"]; !ok {
		t.Fatalf("no tool_use activity: %+v", acts)
	} else if !strings.Contains(a.payload, "Bash") {
		t.Fatalf("tool_use payload missing tool name: %s", a.payload)
	}
}

// TestAgentController_ReconcileRunning_PassesModel pins the v2.7 Model-plumbing
// PRIMARY path (the gate1k fresh-start failure): a reconcile command carrying the
// agent's configured model spawns the session with that --model; an empty model
// passes none (backward-compat → claude default).
func TestAgentController_ReconcileRunning_PassesModel(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	pl := reconcilePayload{AgentID: "agent-1", DesiredLifecycle: "running", Model: "claude-test-model-x", Version: 1}
	cmd := ControlCommand{ID: "cmd-rm", Offset: 1, CommandType: cmdTypeAgentReconcile, Payload: mustJSON(t, pl)}
	if err := c.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("reconcile running w/ model: %v", err)
	}
	if got := rs.last().cfg.Model; got != "claude-test-model-x" {
		t.Fatalf("session must spawn with the agent's configured model, got %q", got)
	}

	// Backward-compat: no model → none passed (daemon omits --model → claude default).
	c2, _, rs2 := newTestController(t, t.TempDir())
	defer c2.Shutdown(context.Background())
	pl2 := reconcilePayload{AgentID: "agent-2", DesiredLifecycle: "running", Version: 1}
	cmd2 := ControlCommand{ID: "cmd-rn", Offset: 1, CommandType: cmdTypeAgentReconcile, Payload: mustJSON(t, pl2)}
	if err := c2.Handle(context.Background(), cmd2); err != nil {
		t.Fatalf("reconcile running w/o model: %v", err)
	}
	if got := rs2.last().cfg.Model; got != "" {
		t.Fatalf("no configured model must pass empty (claude default), got %q", got)
	}
}

func TestAgentController_Work_InjectsBrief(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "do the task", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}

	// The controller Injects the RAW brief; the stream-json wire encoding is the
	// supervisor's job (covered by claudestream/supervisor + Tester's GATE).
	in := rs.last().injectedMsgs()
	if len(in) != 1 || in[0] != "do the task" {
		t.Fatalf("want the brief injected once verbatim, got %+v", in)
	}
}

// TestAgentController_Converse_IsErrorTurnPostsConverseError is the converse
// analogue of the L2 WorkItem failure surface (#185 follow-up / UX Rule 9): a
// DM/channel turn (no WorkItem) that ends is_error — e.g. an invalid model →
// claude 404 — must post a VISIBLE converse-error system message into its
// conversation, not leave the human in a silent black hole. A success turn must
// not, and a stray second is_error must not double-post.
func TestAgentController_Converse_IsErrorTurnPostsConverseError(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), converseCmd(t, "agent-1", "conv-1", "m1", "hi agent", 2)); err != nil {
		t.Fatalf("converse: %v", err)
	}
	fs := rs.last()

	// A successful converse turn posts NO converse-error.
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "success", Result: "PONG", IsError: false})
	if got := len(rep.converseErrCalls()); got != 0 {
		t.Fatalf("success converse turn must not post converse-error, got %d", got)
	}

	// An is_error converse turn posts exactly one converse-error for conv-1.
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "error_during_execution", Result: "model x not found", IsError: true})
	calls := rep.converseErrCalls()
	if len(calls) != 1 || calls[0].conversationID != "conv-1" || calls[0].agentID != "agent-1" {
		t.Fatalf("is_error converse turn must post 1 converse-error for conv-1, got %+v", calls)
	}
	if !strings.Contains(calls[0].summary, "error_during_execution") {
		t.Fatalf("summary should carry the subtype, got %q", calls[0].summary)
	}
	// A stray second is_error must NOT re-post (in-flight conversation cleared).
	fs.emit(claudestream.StreamEvent{Type: "result", Subtype: "error_during_execution", IsError: true})
	if got := len(rep.converseErrCalls()); got != 1 {
		t.Fatalf("a second is_error must not re-post converse-error, got %d", got)
	}
}

func TestAgentController_Work_NoSessionRetries(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	// No reconcile(running) yet → work should return an error (retry).
	if err := c.Handle(context.Background(), workCmd(t, "agent-1", "wi-1", "brief", 1)); err == nil {
		t.Fatal("want error for work with no running session (retry signal)")
	}
}

func TestAgentController_ReconcileStop_ReportsStoppedOnce(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	fs := rs.last()

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "stopped", 2, "", 2)); err != nil {
		t.Fatalf("reconcile stop: %v", err)
	}

	// The stop flow SIGTERMed the supervisor via the session Stop (fake records it).
	fs.mu.Lock()
	stoppedSess := fs.stopped
	fs.mu.Unlock()
	if !stoppedSess {
		t.Fatal("expected the session to be Stopped (supervisor SIGTERM)")
	}

	lc := rep.lifecycleCalls()
	stopped := 0
	for _, l := range lc {
		if l.agentID == "agent-1" && l.state == "stopped" {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("want exactly 1 stopped lifecycle report, got %d (%+v)", stopped, lc)
	}
}

func TestAgentController_ReconcileReset_WipesWorkspaceWithContainment(t *testing.T) {
	base := t.TempDir()
	c, rep, rs := newTestController(t, base)

	// Start so the home dirs exist.
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	// First start reads the initial epoch 0.
	if got := rs.last().cfg.Epoch; got != 0 {
		t.Fatalf("first start must use epoch 0, got %d", got)
	}

	home := filepath.Join(base, "agents", "agent-1")
	workspace := filepath.Join(home, "workspace")
	// Plant a file inside the workspace (to be wiped).
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(workspace, "scratch.txt")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a sibling dir OUTSIDE the agent home (must be untouched).
	sibling := filepath.Join(base, "agents", "other-agent")
	if err := os.MkdirAll(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(sibling, "keep.txt")
	if err := os.WriteFile(siblingFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "resetting", 2, "workspace", 2)); err != nil {
		t.Fatalf("reconcile reset: %v", err)
	}

	// Workspace wiped.
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("workspace not wiped: stat err=%v", err)
	}
	// Sibling untouched (containment).
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("sibling outside agent home was touched: %v", err)
	}

	// Exactly one stopped lifecycle report.
	stopped := 0
	for _, l := range rep.lifecycleCalls() {
		if l.state == "stopped" {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("want 1 stopped lifecycle, got %d", stopped)
	}

	// CLEAN-SLATE chain: the reset bumped the durable epoch, and the NEXT
	// reconcile(running) must spawn with the bumped epoch (→ a fresh claude
	// session-id). This proves reset→BumpEpochForReset→next-start-reads-epoch
	// end-to-end at the controller layer.
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 3, "", 3)); err != nil {
		t.Fatalf("reconcile running after reset: %v", err)
	}
	if got := rs.last().cfg.Epoch; got != 1 {
		t.Fatalf("post-reset start must use bumped epoch 1 (clean slate), got %d", got)
	}
}

// v2.7 #179: the per-agent home must NOT re-append "workers/<wid>" (AgentHomeBase
// is already worker-scoped). It resolves to <base>/agents/<id> with no double
// nesting — and MUST equal the dir boot_reconcile's enumerateLocalAgents scans,
// or reattach breaks.
func TestAgentController_agentPaths_DedupedNoDoubleWorkers(t *testing.T) {
	base := t.TempDir()
	c, _, _ := newTestController(t, base)

	home, workspace, err := c.agentPaths("ag-x")
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	want := filepath.Join(base, "agents", "ag-x")
	if home != want {
		t.Fatalf("home = %q, want %q (no redundant workers/<wid>)", home, want)
	}
	if workspace != filepath.Join(want, "workspace") {
		t.Fatalf("workspace = %q", workspace)
	}
	if strings.Contains(home, filepath.Join("workers", "w-1")) {
		t.Fatalf("home still contains redundant workers/<wid>: %q", home)
	}
	// Must match boot_reconcile's scan root (parent of the per-agent homes).
	if got := filepath.Dir(home); got != filepath.Join(base, "agents") {
		t.Fatalf("home parent = %q, boot-reconcile scans %q — mismatch breaks reattach", got, filepath.Join(base, "agents"))
	}
}

func TestAgentController_ResetContainmentRefusesEscape(t *testing.T) {
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	home := filepath.Join(base, "agents", "agent-1")

	// A target that escapes the agent home must be refused (nothing deleted).
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	escapeTarget := filepath.Join(home, "..", "..", "..", "outside")
	if err := c.wipeContained(home, escapeTarget); err == nil {
		t.Fatal("want containment refusal for escaping target")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("escape target was deleted despite refusal: %v", err)
	}
	// Home itself must be refused too.
	if err := c.wipeContained(home, home); err == nil {
		t.Fatal("want refusal to wipe the agent home itself")
	}
}

func TestAgentController_UnexpectedCrash_ReportsErrorOnceAndClears(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	fs := rs.last()
	// Crash the session (supervisor/claude died while desired=running → OnExit
	// takes the crash branch and reports error).
	fs.crash(errors.New("boom"))

	waitFor(t, func() bool {
		for _, l := range rep.lifecycleCalls() {
			if l.state == "error" {
				return true
			}
		}
		return false
	})

	errs := 0
	for _, l := range rep.lifecycleCalls() {
		if l.agentID == "agent-1" && l.state == "error" {
			errs++
			if !strings.Contains(l.errMsg, "boom") {
				t.Fatalf("error msg missing cause: %q", l.errMsg)
			}
		}
	}
	if errs != 1 {
		t.Fatalf("want exactly 1 error lifecycle, got %d", errs)
	}

	// Managed entry cleared.
	c.mu.Lock()
	_, ok := c.agents["agent-1"]
	c.mu.Unlock()
	if ok {
		t.Fatal("managed entry not cleared after crash")
	}
}

func TestAgentController_VersionIdempotency_ReplayNoRestart(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 5, "", 1)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if rs.count() != 1 {
		t.Fatalf("want 1 session start, got %d", rs.count())
	}

	// Replay the SAME version (and an older one) → no restart.
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 5, "", 2)); err != nil {
		t.Fatalf("replay reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 3, "", 3)); err != nil {
		t.Fatalf("older reconcile: %v", err)
	}
	if rs.count() != 1 {
		t.Fatalf("replay caused a restart: starts=%d want 1", rs.count())
	}
}

func TestAgentController_RestartOnVersionBump(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// A version bump with desired=running → restart (stop old + start new).
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 2, "", 2)); err != nil {
		t.Fatalf("restart reconcile: %v", err)
	}
	if rs.count() != 2 {
		t.Fatalf("want 2 session starts after restart, got %d", rs.count())
	}
}

func TestAgentController_UnknownCommandType_ReturnsNil(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	cmd := ControlCommand{ID: "x", Offset: 1, CommandType: "agent.frobnicate", Payload: "{}"}
	if err := c.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("unknown command must return nil (don't wedge cursor), got %v", err)
	}
}

func TestAgentController_MalformedPayload_ReturnsNil(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	cmd := ControlCommand{ID: "x", Offset: 1, CommandType: cmdTypeAgentReconcile, Payload: "{not json"}
	if err := c.Handle(context.Background(), cmd); err != nil {
		t.Fatalf("malformed reconcile must return nil, got %v", err)
	}
}

// waitFor polls cond up to ~2s, failing the test on timeout.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestAgentController_Wake_InjectsMessage(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), wakeCmd(t, "agent-1", "wi-1", "msg-1", "human replied here", 2)); err != nil {
		t.Fatalf("wake: %v", err)
	}

	in := rs.last().injectedMsgs()
	if len(in) != 1 || in[0] != "human replied here" {
		t.Fatalf("want the wake message injected once verbatim, got %+v", in)
	}
}

func TestAgentController_Wake_DedupByMessageID(t *testing.T) {
	c, _, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Two wakes with the SAME message_id (e.g. reconnect replay) → inject once.
	if err := c.Handle(context.Background(), wakeCmd(t, "agent-1", "wi-1", "msg-1", "reply text", 2)); err != nil {
		t.Fatalf("wake 1: %v", err)
	}
	if err := c.Handle(context.Background(), wakeCmd(t, "agent-1", "wi-1", "msg-1", "reply text", 3)); err != nil {
		t.Fatalf("wake 2 (replay): %v", err)
	}

	in := rs.last().injectedMsgs()
	if len(in) != 1 || in[0] != "reply text" {
		t.Fatalf("dedup failed: want exactly 1 injection, got %+v", in)
	}
}

func TestAgentController_Wake_NoSessionRetries(t *testing.T) {
	c, _, _ := newTestController(t, t.TempDir())
	// No reconcile(running) yet → wake should return an error (retry), same
	// policy as work().
	if err := c.Handle(context.Background(), wakeCmd(t, "agent-1", "wi-1", "msg-1", "reply", 1)); err == nil {
		t.Fatal("want error for wake with no running session (retry signal)")
	}
}

// D2-e-ii: a wake carrying conversation_id advances the read-state cursor
// (ReportMarkSeen) after a successful inject — for BOTH the immediate (e-i) and
// batch (e-ii) paths (identical command shape; just merged text + batch id).
func TestAgentController_Wake_ImmediateReportsMarkSeen(t *testing.T) {
	c, rep, _ := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), wakeCmdConv(t, "agent-1", "wi-1", "conv-9", "msg-1", "human replied", 2)); err != nil {
		t.Fatalf("wake: %v", err)
	}

	ms := rep.markSeenCalls()
	if len(ms) != 1 || ms[0].conversationID != "conv-9" || ms[0].messageID != "msg-1" || ms[0].agentID != "agent-1" {
		t.Fatalf("mark-seen calls: %+v", ms)
	}
}

func TestAgentController_Wake_BatchReportsMarkSeenWithLastID(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// e-ii batch: merged text, message_id = NEWEST delivered (msg-5).
	merged := "[user:alice] one\n[user:alice] two"
	if err := c.Handle(context.Background(), wakeCmdConv(t, "agent-1", "wi-1", "conv-9", "msg-5", merged, 2)); err != nil {
		t.Fatalf("wake batch: %v", err)
	}

	in := rs.last().injectedMsgs()
	if len(in) != 1 || !strings.Contains(in[0], "one") || !strings.Contains(in[0], "two") {
		t.Fatalf("merged batch not injected as one verbatim message: %+v", in)
	}
	ms := rep.markSeenCalls()
	if len(ms) != 1 || ms[0].messageID != "msg-5" || ms[0].conversationID != "conv-9" {
		t.Fatalf("batch mark-seen calls: %+v", ms)
	}
}

// Mark-seen does not fire when no conversation_id is carried (defensive) and the
// FIFO dedup still works alongside the cursor.
func TestAgentController_Wake_NoConvID_NoMarkSeen_DedupStillWorks(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := c.Handle(context.Background(), wakeCmd(t, "agent-1", "wi-1", "msg-1", "reply text", 2)); err != nil {
		t.Fatalf("wake 1: %v", err)
	}
	if err := c.Handle(context.Background(), wakeCmd(t, "agent-1", "wi-1", "msg-1", "reply text", 3)); err != nil {
		t.Fatalf("wake 2 (replay): %v", err)
	}
	if in := rs.last().injectedMsgs(); len(in) != 1 {
		t.Fatalf("dedup failed alongside no-mark-seen path: %+v", in)
	}
	if ms := rep.markSeenCalls(); len(ms) != 0 {
		t.Fatalf("no conversation_id must skip mark-seen, got %+v", ms)
	}
}

// TestAgentController_OnEvent_TagsActivityWithCurrentWorkItem pins v2.7 #111:
// onEvent must stamp the in-flight WorkItem id onto the activity event's
// task_ref (previously hardcoded "") so the observability projection can
// aggregate tool_calls / tokens / current_activity per work-item. With NO
// in-flight WorkItem the ref stays empty (idle activity), unchanged.
func TestAgentController_OnEvent_TagsActivityWithCurrentWorkItem(t *testing.T) {
	c, rep, rs := newTestController(t, t.TempDir())
	defer c.Shutdown(context.Background())
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	fs := rs.last()

	// No in-flight work item yet → activity carries an empty task_ref.
	fs.emit(claudestream.StreamEvent{Type: "tool_use", ToolName: "Bash", ToolUseID: "tu-0"})
	acts := rep.activityCalls()
	if last := acts[len(acts)-1]; last.taskRef != "" {
		t.Fatalf("idle activity (no in-flight WI) must have empty task_ref, got %q", last.taskRef)
	}

	// Simulate an in-flight work item (set the same field work/wake delivery sets).
	c.mu.Lock()
	c.agents["agent-1"].currentTaskID = "WI-1"
	c.mu.Unlock()

	fs.emit(claudestream.StreamEvent{Type: "tool_use", ToolName: "Bash", ToolUseID: "tu-1"})
	acts = rep.activityCalls()
	last := acts[len(acts)-1]
	if last.taskRef != "WI-1" {
		t.Fatalf("activity during an in-flight work item must carry its task_ref; want WI-1, got %q", last.taskRef)
	}
}
