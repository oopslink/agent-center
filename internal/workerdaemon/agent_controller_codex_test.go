package workerdaemon

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// fakeCodexSess is the TEST-ONLY agentSession for the cli=codex path: it records
// Inject/Stop/Detach and lets the test drive OnEvent/OnExit the real CodexSession
// would fire — without spawning codex. Mirrors fakeSession but holds a
// CodexSessionConfig.
type fakeCodexSess struct {
	cfg CodexSessionConfig

	mu       sync.Mutex
	injected []string
	stopped  bool
	detached bool
	exited   bool
}

func (f *fakeCodexSess) Inject(_ context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped || f.detached {
		return ErrSessionClosed
	}
	f.injected = append(f.injected, msg)
	return nil
}

func (f *fakeCodexSess) Stop(_ context.Context) error { f.fireExit(true, nil); return nil }
func (f *fakeCodexSess) Detach()                       { f.fireExit(false, nil) }

func (f *fakeCodexSess) fireExit(viaStop bool, err error) {
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

func (f *fakeCodexSess) emit(ev claudestream.StreamEvent) {
	f.mu.Lock()
	cb := f.cfg.OnEvent
	f.mu.Unlock()
	if cb != nil {
		cb(ev)
	}
}

func (f *fakeCodexSess) injectedMsgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.injected...)
}

type recordingCodexStarter struct {
	mu       sync.Mutex
	sessions []*fakeCodexSess
	nextErr  error
}

func (s *recordingCodexStarter) start(_ context.Context, cfg CodexSessionConfig) (agentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextErr != nil {
		err := s.nextErr
		s.nextErr = nil
		return nil, err
	}
	fs := &fakeCodexSess{cfg: cfg}
	s.sessions = append(s.sessions, fs)
	return fs, nil
}

func (s *recordingCodexStarter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func (s *recordingCodexStarter) last() *fakeCodexSess {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) == 0 {
		return nil
	}
	return s.sessions[len(s.sessions)-1]
}

func reconcileCmdCLI(t *testing.T, agentID, desired, cli string, version int, offset int64) ControlCommand {
	t.Helper()
	pl := reconcilePayload{AgentID: agentID, DesiredLifecycle: desired, CLI: cli, Version: version}
	return ControlCommand{
		ID:          "cmd-r",
		Offset:      offset,
		CommandType: cmdTypeAgentReconcile,
		Payload:     mustJSON(t, pl),
	}
}

// TestAgentController_ReconcileRunning_Codex_StartsCodexSession pins the cli=codex
// runtime dispatch: a reconcile carrying cli=codex starts a CodexSession (NOT the
// claude supervisor), persists the cli marker (so boot-recovery routes it away
// from the supervisor path), injects work into the codex session, and maps its
// StreamEvents to activity.
func TestAgentController_ReconcileRunning_Codex_StartsCodexSession(t *testing.T) {
	base := t.TempDir()
	c, rep, rs := newTestController(t, base)
	cs := &recordingCodexStarter{}
	c.cfg.codexStarter = cs.start
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmdCLI(t, "agent-c", "running", cliCodex, 1, 1)); err != nil {
		t.Fatalf("reconcile running (codex): %v", err)
	}

	// The codex starter ran; the claude supervisor starter did NOT.
	if cs.count() != 1 {
		t.Fatalf("want 1 codex session start, got %d", cs.count())
	}
	if rs.count() != 0 {
		t.Fatalf("supervisor starter must NOT be used for cli=codex, got %d starts", rs.count())
	}

	// cli marker persisted so boot-recovery routes this agent away from the
	// supervisor probe/relaunch.
	home, _, err := c.agentPaths("agent-c")
	if err != nil {
		t.Fatal(err)
	}
	if got := readAgentCLIMarker(home); got != cliCodex {
		t.Fatalf("cli marker = %q, want codex", got)
	}

	// Work is injected into the codex session.
	if err := c.Handle(context.Background(), workCmd(t, "agent-c", "wi-1", "do the task", 2)); err != nil {
		t.Fatalf("work: %v", err)
	}
	if msgs := cs.last().injectedMsgs(); len(msgs) == 0 || !strings.Contains(strings.Join(msgs, "\n"), "do the task") {
		t.Fatalf("work brief not injected into codex session: %v", msgs)
	}

	// Codex StreamEvents map to activity (the reply + the terminal result).
	cs.last().emit(claudestream.StreamEvent{Type: "assistant_text", Text: "PONG"})
	cs.last().emit(claudestream.StreamEvent{Type: "result", Subtype: "success"})
	var sawText, sawResult bool
	for _, a := range rep.activityCalls() {
		if a.agentID != "agent-c" {
			t.Fatalf("activity for wrong agent: %+v", a)
		}
		switch a.eventType {
		case "assistant_text":
			sawText = sawText || strings.Contains(a.payload, "PONG")
		case "result":
			sawResult = true
		}
	}
	if !sawText || !sawResult {
		t.Fatalf("codex activity not reported: sawText=%v sawResult=%v", sawText, sawResult)
	}
}

// TestAgentController_Codex_CrashDoesNotSupervisorSelfHeal pins the onExit guard:
// a codex session's unexpected exit reports "error" once and does NOT enter the
// supervisor Mode-B self-heal (which would spawn claude).
func TestAgentController_Codex_CrashDoesNotSupervisorSelfHeal(t *testing.T) {
	base := t.TempDir()
	c, rep, rs := newTestController(t, base)
	cs := &recordingCodexStarter{}
	c.cfg.codexStarter = cs.start
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmdCLI(t, "agent-c", "running", cliCodex, 1, 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Unexpected exit (codex fatal) → onExit crash branch.
	cs.last().fireExit(false /*viaStop*/, context.DeadlineExceeded)

	// No supervisor relaunch was scheduled/performed.
	if rs.count() != 0 {
		t.Fatalf("codex crash must NOT trigger a supervisor relaunch, got %d", rs.count())
	}
	// Lifecycle "error" reported once.
	var sawError bool
	for _, l := range rep.lifecycleCalls() {
		if l.agentID == "agent-c" && l.state == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("codex crash should report lifecycle error once; calls=%+v", rep.lifecycleCalls())
	}
}
