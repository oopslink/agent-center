package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// ---------------------------------------------------------------------------
// buildCodexArgv
// ---------------------------------------------------------------------------

func TestBuildCodexArgv_Fresh(t *testing.T) {
	argv := buildCodexArgv(codexLaunchSpec{Binary: "codex", Prompt: "do it", Model: "gpt-5"})
	if argv[0] != "codex" || argv[1] != "exec" {
		t.Fatalf("argv prefix: %v", argv)
	}
	if slices.Contains(argv, "resume") {
		t.Fatalf("fresh turn must not have resume: %v", argv)
	}
	for _, want := range []string{"--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"} {
		if !slices.Contains(argv, want) {
			t.Fatalf("missing %q: %v", want, argv)
		}
	}
	if i := slices.Index(argv, "-m"); i < 0 || argv[i+1] != "gpt-5" {
		t.Fatalf("model flag: %v", argv)
	}
	if argv[len(argv)-1] != "do it" {
		t.Fatalf("prompt must be last: %v", argv)
	}
}

func TestBuildCodexArgv_Resume(t *testing.T) {
	argv := buildCodexArgv(codexLaunchSpec{Prompt: "next", ThreadID: "thread-123"})
	if argv[0] != "codex" || argv[1] != "exec" || argv[2] != "resume" || argv[3] != "thread-123" {
		t.Fatalf("resume argv prefix: %v", argv)
	}
	if !slices.Contains(argv, "--json") {
		t.Fatalf("resume missing --json: %v", argv)
	}
	if argv[len(argv)-1] != "next" {
		t.Fatalf("prompt must be last: %v", argv)
	}
}

// ---------------------------------------------------------------------------
// mapCodexLine
// ---------------------------------------------------------------------------

func TestMapCodexLine_ThreadStarted_CapturesID_NoEvent(t *testing.T) {
	evs, tid, err := mapCodexLine([]byte(`{"type":"thread.started","thread_id":"abc-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tid != "abc-123" {
		t.Fatalf("thread id: %q", tid)
	}
	if len(evs) != 0 {
		t.Fatalf("expected no stream events: %+v", evs)
	}
}

func TestMapCodexLine_AgentMessage_AssistantText(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"PONG"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "assistant_text" || evs[0].Text != "PONG" {
		t.Fatalf("assistant_text mapping: %+v", evs)
	}
}

func TestMapCodexLine_CommandExecution_ToolUseAndResult(t *testing.T) {
	start := `{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"ls","status":"in_progress"}}`
	evs, _, err := mapCodexLine([]byte(start))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "tool_use" || evs[0].ToolName != "shell" || evs[0].ToolUseID != "item_0" {
		t.Fatalf("tool_use mapping: %+v", evs)
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(evs[0].ToolInput, &in); err != nil || in.Command != "ls" {
		t.Fatalf("tool input: %s err=%v", evs[0].ToolInput, err)
	}

	done := `{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"ls","aggregated_output":"a.txt\n","exit_code":0,"status":"completed"}}`
	evs, _, err = mapCodexLine([]byte(done))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "tool_result" || evs[0].ToolUseID != "item_0" {
		t.Fatalf("tool_result mapping: %+v", evs)
	}
	var out string
	if err := json.Unmarshal(evs[0].ToolResult, &out); err != nil || out != "a.txt\n" {
		t.Fatalf("tool result: %s err=%v", evs[0].ToolResult, err)
	}
}

func TestMapCodexLine_Reasoning_Thinking(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"item.completed","item":{"id":"i","type":"reasoning","text":"hmm"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "thinking" || evs[0].Text != "hmm" {
		t.Fatalf("thinking mapping: %+v", evs)
	}
}

func TestMapCodexLine_TurnCompleted_ResultSuccessTokens(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":7}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "result" || evs[0].IsError {
		t.Fatalf("result mapping: %+v", evs)
	}
	if evs[0].TokensIn != 100 || evs[0].TokensOut != 7 {
		t.Fatalf("tokens: %+v", evs[0])
	}
}

func TestMapCodexLine_TurnFailed_ResultError(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"turn.failed","error":{"message":"boom"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "result" || !evs[0].IsError || evs[0].Result != "boom" {
		t.Fatalf("error result mapping: %+v", evs)
	}
}

// issue-c7a1fe3e: a bare `error` event (transient reconnect blip) must map to a
// NON-"result" diagnostic — the session/events terminal gate is Type=="result", so a
// terminal mapping (the old bug) false-killed the turn. turn.failed stays terminal
// (above); error is non-terminal here.
func TestMapCodexLine_TransientError_NonTerminal(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"error","message":"stream disconnected, reconnecting"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d: %+v", len(evs), evs)
	}
	if evs[0].Type == "result" {
		t.Fatalf("transient error must NOT be a terminal result event (would false-kill): %+v", evs[0])
	}
	if evs[0].Type != "error" || evs[0].Subtype != "transient" || !evs[0].IsError ||
		evs[0].Result != "stream disconnected, reconnecting" {
		t.Fatalf("transient error event shape: %+v", evs[0])
	}
}

func TestMapCodexLine_TurnStarted_NoEvent(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"turn.started"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 0 {
		t.Fatalf("turn.started should yield no events: %+v", evs)
	}
}

func TestMapCodexLine_Unknown(t *testing.T) {
	evs, _, err := mapCodexLine([]byte(`{"type":"item.completed","item":{"type":"web_search"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "unknown" {
		t.Fatalf("unknown mapping: %+v", evs)
	}
}

func TestMapCodexLine_Malformed(t *testing.T) {
	if _, _, err := mapCodexLine(nil); !errors.Is(err, ErrCodexInvalidEvent) {
		t.Fatalf("empty: %v", err)
	}
	if _, _, err := mapCodexLine([]byte(`{bad`)); !errors.Is(err, ErrCodexInvalidEvent) {
		t.Fatalf("bad json: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CodexSession with a fake launcher
// ---------------------------------------------------------------------------

type fakeCodexProc struct {
	r       io.Reader
	waitErr error
}

func (p *fakeCodexProc) Stdout() io.Reader { return p.r }
func (p *fakeCodexProc) Wait() error       { return p.waitErr }

type fakeCodexLauncher struct {
	mu        sync.Mutex
	specs     []codexLaunchSpec
	turns     [][]string // JSONL lines to emit per turn (by call index)
	waitErrs  []error    // per-turn Wait() error (by call index)
	launchErr error
}

func (l *fakeCodexLauncher) Launch(_ context.Context, spec codexLaunchSpec) (codexProc, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := len(l.specs)
	l.specs = append(l.specs, spec)
	if l.launchErr != nil {
		return nil, l.launchErr
	}
	var lines []string
	if i < len(l.turns) {
		lines = l.turns[i]
	}
	data := strings.Join(lines, "\n")
	if data != "" {
		data += "\n"
	}
	var werr error
	if i < len(l.waitErrs) {
		werr = l.waitErrs[i]
	}
	return &fakeCodexProc{r: strings.NewReader(data), waitErr: werr}, nil
}

func (l *fakeCodexLauncher) specAt(i int) codexLaunchSpec {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.specs[i]
}

// sessionHarness collects events and signals each turn's terminal "result".
type sessionHarness struct {
	mu      sync.Mutex
	events  []claudestream.StreamEvent
	results chan claudestream.StreamEvent
	exitErr error
	exited  chan struct{}
}

func newHarness() *sessionHarness {
	return &sessionHarness{
		results: make(chan claudestream.StreamEvent, 8),
		exited:  make(chan struct{}),
	}
}

func (h *sessionHarness) onEvent(ev claudestream.StreamEvent) {
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.mu.Unlock()
	if ev.Type == "result" {
		h.results <- ev
	}
}

func (h *sessionHarness) onExit(err error) {
	h.exitErr = err
	close(h.exited)
}

func (h *sessionHarness) snapshot() []claudestream.StreamEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

func (h *sessionHarness) waitResult(t *testing.T) claudestream.StreamEvent {
	t.Helper()
	select {
	case ev := <-h.results:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for turn result")
		return claudestream.StreamEvent{}
	}
}

func TestCodexSession_SingleTurn_FreshThenResume(t *testing.T) {
	lr := &fakeCodexLauncher{turns: [][]string{
		{
			`{"type":"thread.started","thread_id":"T1"}`,
			`{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"hello"}}`,
			`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`,
		},
		{
			`{"type":"item.completed","item":{"id":"i3","type":"agent_message","text":"again"}}`,
			`{"type":"turn.completed","usage":{"input_tokens":11,"output_tokens":3}}`,
		},
	}}
	h := newHarness()
	s, err := StartCodexSession(context.Background(), CodexSessionConfig{
		AgentID: "agent-1", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Inject(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	r1 := h.waitResult(t)
	if r1.IsError || r1.TokensOut != 2 {
		t.Fatalf("turn1 result: %+v", r1)
	}
	if s.ThreadID() != "T1" {
		t.Fatalf("thread id not captured: %q", s.ThreadID())
	}

	if err := s.Inject(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	h.waitResult(t)

	// Turn 1 spawned fresh; turn 2 resumed with the captured thread id.
	if got := lr.specAt(0).ThreadID; got != "" {
		t.Fatalf("turn1 should be fresh, got thread %q", got)
	}
	if got := lr.specAt(1); got.ThreadID != "T1" || got.Prompt != "second" {
		t.Fatalf("turn2 resume spec: %+v", got)
	}

	// assistant_text events surfaced for both turns.
	var texts []string
	for _, ev := range h.snapshot() {
		if ev.Type == "assistant_text" {
			texts = append(texts, ev.Text)
		}
	}
	if !slices.Equal(texts, []string{"hello", "again"}) {
		t.Fatalf("assistant texts: %v", texts)
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-h.exited
	if h.exitErr != nil {
		t.Fatalf("clean stop should exit nil: %v", h.exitErr)
	}
}

// TestCodexSession_TransientErrorThenCompleted_NoFalseDeath is the issue-c7a1fe3e
// regression lock at the SESSION level: a transient `error` mid-turn (reconnect blip)
// followed by a normal turn.completed must yield a SUCCESS result — the blip must not
// false-terminate. Before the fix, the error mapped to a terminal result(IsError) and
// waitResult would return that error; now the terminal result is the turn.completed.
func TestCodexSession_TransientErrorThenCompleted_NoFalseDeath(t *testing.T) {
	lr := &fakeCodexLauncher{turns: [][]string{{
		`{"type":"thread.started","thread_id":"T1"}`,
		`{"type":"error","message":"stream disconnected, reconnecting"}`, // transient blip
		`{"type":"item.completed","item":{"id":"m","type":"agent_message","text":"recovered"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":1}}`,
	}}}
	h := newHarness()
	s, err := StartCodexSession(context.Background(), CodexSessionConfig{
		AgentID: "agent-1", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Inject(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	r := h.waitResult(t)
	// The terminal result is the turn.completed SUCCESS, NOT a false error from the blip.
	if r.IsError {
		t.Fatalf("transient error false-killed the turn: %+v", r)
	}
	if r.TokensOut != 1 {
		t.Fatalf("terminal result should be the turn.completed (out=1), got: %+v", r)
	}
	// Consumption continued past the blip: the post-blip assistant text surfaced.
	var texts []string
	for _, ev := range h.snapshot() {
		if ev.Type == "assistant_text" {
			texts = append(texts, ev.Text)
		}
	}
	if !slices.Contains(texts, "recovered") {
		t.Fatalf("post-blip event was not consumed; texts=%v", texts)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-h.exited
}

// T972 supervisor resume early-persist: the FIRST thread.started fires OnThreadID ONCE
// with the codex thread_id (which the starter persists via sessioninstance.MarkSessionID).
func TestCodexSession_OnThreadID_EarlyPersist(t *testing.T) {
	var captured []string
	lr := &fakeCodexLauncher{turns: [][]string{{
		`{"type":"thread.started","thread_id":"th_new"}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	}}}
	h := newHarness()
	s, err := StartCodexSession(context.Background(), CodexSessionConfig{
		AgentID: "a", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit,
		OnThreadID: func(tid string) { captured = append(captured, tid) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Inject(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	h.waitResult(t)
	if len(captured) != 1 || captured[0] != "th_new" {
		t.Errorf("OnThreadID = %v, want [th_new] exactly once (early-persist)", captured)
	}
	_ = s.Stop(context.Background())
	<-h.exited
}

// T972 resume: a ResumeThreadID (persisted from a prior generation) seeds the session so
// the FIRST turn is `codex exec resume <id>`; a seeded (already-persisted) id must NOT
// re-fire OnThreadID.
func TestCodexSession_ResumeThreadID_SeedsResume(t *testing.T) {
	var rePersist []string
	lr := &fakeCodexLauncher{turns: [][]string{{
		// A resumed turn: no thread.started (codex continues the prior thread).
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	}}}
	h := newHarness()
	s, err := StartCodexSession(context.Background(), CodexSessionConfig{
		AgentID: "a", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit,
		ResumeThreadID: "th_prior",
		OnThreadID:     func(tid string) { rePersist = append(rePersist, tid) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Inject(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	h.waitResult(t)
	if got := lr.specAt(0).ThreadID; got != "th_prior" {
		t.Errorf("first turn thread id = %q, want resume th_prior", got)
	}
	if len(rePersist) != 0 {
		t.Errorf("a seeded (already-persisted) thread_id must not re-fire OnThreadID: %v", rePersist)
	}
	_ = s.Stop(context.Background())
	<-h.exited
}

func TestCodexSession_ToolEvents(t *testing.T) {
	lr := &fakeCodexLauncher{turns: [][]string{{
		`{"type":"thread.started","thread_id":"T"}`,
		`{"type":"item.started","item":{"id":"c0","type":"command_execution","command":"ls","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"c0","type":"command_execution","command":"ls","aggregated_output":"a\n","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"m","type":"agent_message","text":"done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	}}}
	h := newHarness()
	s, _ := StartCodexSession(context.Background(), CodexSessionConfig{AgentID: "a", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit})
	_ = s.Inject(context.Background(), "go")
	h.waitResult(t)
	_ = s.Stop(context.Background())

	var types []string
	for _, ev := range h.snapshot() {
		types = append(types, ev.Type)
	}
	for _, want := range []string{"tool_use", "tool_result", "assistant_text", "result"} {
		if !slices.Contains(types, want) {
			t.Fatalf("missing %q in %v", want, types)
		}
	}
}

func TestCodexSession_CrashWithoutResult_SynthesizesError(t *testing.T) {
	lr := &fakeCodexLauncher{
		turns:    [][]string{{`{"type":"thread.started","thread_id":"T"}`}}, // no turn.completed
		waitErrs: []error{errors.New("exit status 1")},
	}
	h := newHarness()
	s, _ := StartCodexSession(context.Background(), CodexSessionConfig{AgentID: "a", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit})
	_ = s.Inject(context.Background(), "go")
	r := h.waitResult(t)
	if !r.IsError {
		t.Fatalf("expected synthesized error result, got %+v", r)
	}
	_ = s.Stop(context.Background())
}

func TestCodexSession_LaunchFailure_FatalExit(t *testing.T) {
	lr := &fakeCodexLauncher{launchErr: errors.New("codex not found")}
	h := newHarness()
	s, _ := StartCodexSession(context.Background(), CodexSessionConfig{AgentID: "a", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit})
	_ = s.Inject(context.Background(), "go")
	select {
	case <-h.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("expected OnExit after fatal launch failure")
	}
	if h.exitErr == nil {
		t.Fatal("fatal launch failure should exit with error")
	}
	if err := s.Inject(context.Background(), "x"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("inject after fatal exit: %v", err)
	}
}

func TestCodexSession_InjectAfterStop_Closed(t *testing.T) {
	lr := &fakeCodexLauncher{}
	h := newHarness()
	s, _ := StartCodexSession(context.Background(), CodexSessionConfig{AgentID: "a", Launcher: lr, OnEvent: h.onEvent, OnExit: h.onExit})
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Inject(context.Background(), "x"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("inject after stop: %v", err)
	}
}

func TestCodexSession_StopIdempotent_ExitOnce(t *testing.T) {
	lr := &fakeCodexLauncher{}
	var exits int
	var mu sync.Mutex
	s, _ := StartCodexSession(context.Background(), CodexSessionConfig{
		AgentID: "a", Launcher: lr,
		OnExit: func(error) { mu.Lock(); exits++; mu.Unlock() },
	})
	_ = s.Stop(context.Background())
	_ = s.Stop(context.Background())
	s.Detach()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if exits != 1 {
		t.Fatalf("OnExit should fire exactly once, fired %d", exits)
	}
}
