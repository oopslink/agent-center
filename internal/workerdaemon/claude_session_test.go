package workerdaemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/mcphost"
)

// fakeProc is a test sessionProc: stdin is a recording buffer, stdout is an
// in-memory pipe the test feeds, Wait blocks until the test signals exit, and
// Signal/Kill are recorded + (optionally) trigger exit.
type fakeProc struct {
	mu sync.Mutex

	stdin   bytes.Buffer
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter

	exited  chan struct{}
	waitErr error

	gotSIGTERM bool
	gotKill    bool
}

func newFakeProc() *fakeProc {
	r, w := io.Pipe()
	return &fakeProc{stdoutR: r, stdoutW: w, exited: make(chan struct{})}
}

func (f *fakeProc) Stdin() io.Writer {
	return &lockedWriter{f: f}
}
func (f *fakeProc) Stdout() io.Reader { return f.stdoutR }

func (f *fakeProc) Wait() error {
	<-f.exited
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeProc) Signal(sig os.Signal) error {
	f.mu.Lock()
	if sig == syscall.SIGTERM {
		f.gotSIGTERM = true
	}
	f.mu.Unlock()
	return nil
}

func (f *fakeProc) Kill() error {
	f.mu.Lock()
	f.gotKill = true
	f.mu.Unlock()
	f.exit(nil)
	return nil
}

// feed writes a canned stdout line (the harness adds the newline if absent).
func (f *fakeProc) feed(line string) {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, _ = io.WriteString(f.stdoutW, line)
}

// exit closes stdout (ending the scanner) and unblocks Wait with err.
func (f *fakeProc) exit(err error) {
	f.mu.Lock()
	if f.waitErr == nil {
		f.waitErr = err
	}
	f.mu.Unlock()
	_ = f.stdoutW.Close()
	select {
	case <-f.exited:
	default:
		close(f.exited)
	}
}

func (f *fakeProc) stdinBytes() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stdin.String()
}

// lockedWriter serialises stdin writes for the race detector.
type lockedWriter struct{ f *fakeProc }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	return w.f.stdin.Write(p)
}

// fakeLauncher returns a pre-built fakeProc and records the spec it saw.
type fakeLauncher struct {
	proc     *fakeProc
	gotSpec  ClaudeLaunchSpec
	launched bool
}

func (l *fakeLauncher) Launch(_ context.Context, spec ClaudeLaunchSpec) (sessionProc, error) {
	l.gotSpec = spec
	l.launched = true
	return l.proc, nil
}

func TestClaudeSession_EventStreaming(t *testing.T) {
	fp := newFakeProc()
	lp := &fakeLauncher{proc: fp}

	var mu sync.Mutex
	var got []StreamEvent
	s, err := StartClaudeSession(context.Background(), ClaudeSessionConfig{
		AgentID:  "agent-1",
		Launcher: lp,
		OnEvent: func(ev StreamEvent) {
			mu.Lock()
			got = append(got, ev)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Feed the GENUINE claude 2.1.156 stream fixture: system/init, an assistant
	// message (a single thinking block — the PONG run emitted no text block),
	// then a result. Append a faithful text-block assistant line so the
	// assistant_text path is also exercised. The reader fires OnEvent ONCE PER
	// parsed StreamEvent.
	for _, line := range fixtureLines(t) {
		fp.feed(string(line))
	}
	fp.feed(assistantTextLine)
	fp.exit(nil)
	s.Wait()

	mu.Lock()
	defer mu.Unlock()
	// system(1) + thinking(1) + result(1) + assistant_text(1) = 4.
	if len(got) != 4 {
		t.Fatalf("want 4 events, got %d: %+v", len(got), got)
	}
	if got[0].Type != "system" || got[0].Subtype != "init" {
		t.Fatalf("event[0]: %+v", got[0])
	}
	if got[1].Type != "thinking" || !strings.Contains(got[1].Text, "PONG") {
		t.Fatalf("event[1]: %+v", got[1])
	}
	if got[2].Type != "result" || got[2].Subtype != "success" || got[2].Result != "PONG" {
		t.Fatalf("event[2]: %+v", got[2])
	}
	if got[3].Type != "assistant_text" || got[3].Text != "PONG" {
		t.Fatalf("event[3]: %+v", got[3])
	}
	for _, ev := range got {
		if ev.Type == "unknown" {
			t.Fatalf("fixture produced an unknown event: %+v", ev)
		}
	}
}

func TestClaudeSession_Inject(t *testing.T) {
	fp := newFakeProc()
	lp := &fakeLauncher{proc: fp}
	s, err := StartClaudeSession(context.Background(), ClaudeSessionConfig{
		AgentID:  "agent-1",
		Launcher: lp,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background(), false)

	if err := s.Inject(context.Background(), "do the thing"); err != nil {
		t.Fatal(err)
	}
	in := fp.stdinBytes()
	if !strings.Contains(in, "do the thing") {
		t.Fatalf("stdin missing message: %q", in)
	}
	if !strings.HasSuffix(in, "\n") {
		t.Fatalf("stdin not newline-terminated: %q", in)
	}
	if !strings.Contains(in, `"type":"user"`) {
		t.Fatalf("stdin not stream-json user envelope: %q", in)
	}
}

func TestClaudeSession_MultipleInjects(t *testing.T) {
	fp := newFakeProc()
	lp := &fakeLauncher{proc: fp}
	s, err := StartClaudeSession(context.Background(), ClaudeSessionConfig{
		AgentID:  "agent-1",
		Launcher: lp,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background(), false)

	if err := s.Inject(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.Inject(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	in := fp.stdinBytes()
	if !strings.Contains(in, "first") || !strings.Contains(in, "second") {
		t.Fatalf("stdin missing one message: %q", in)
	}
	if n := strings.Count(in, "\n"); n != 2 {
		t.Fatalf("want 2 newline-delimited lines, got %d: %q", n, in)
	}
}

func TestClaudeSession_GracefulStop(t *testing.T) {
	beforeGoroutines := runtime.NumGoroutine()

	fp := newFakeProc()
	lp := &fakeLauncher{proc: fp}

	var exitCount int
	var exitMu sync.Mutex
	s, err := StartClaudeSession(context.Background(), ClaudeSessionConfig{
		AgentID:   "agent-1",
		Launcher:  lp,
		StopGrace: 50 * time.Millisecond,
		OnExit: func(error) {
			exitMu.Lock()
			exitCount++
			exitMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Make the process honour SIGTERM by exiting when it arrives.
	go func() {
		for {
			fp.mu.Lock()
			term := fp.gotSIGTERM
			fp.mu.Unlock()
			if term {
				fp.exit(nil)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	if err := s.Stop(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	fp.mu.Lock()
	if !fp.gotSIGTERM {
		t.Fatal("expected SIGTERM")
	}
	fp.mu.Unlock()

	exitMu.Lock()
	if exitCount != 1 {
		t.Fatalf("OnExit fired %d times, want 1", exitCount)
	}
	exitMu.Unlock()

	// Inject after stop returns ErrSessionClosed.
	if err := s.Inject(context.Background(), "late"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("inject after stop: want ErrSessionClosed, got %v", err)
	}

	// Idempotent Stop.
	if err := s.Stop(context.Background(), true); err != nil {
		t.Fatalf("second stop: %v", err)
	}

	// Reader goroutine joined: no leak.
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > beforeGoroutines+1 {
		t.Fatalf("goroutine leak: before=%d after=%d", beforeGoroutines, after)
	}
}

func TestClaudeSession_HardStopOnCrash(t *testing.T) {
	fp := newFakeProc()
	lp := &fakeLauncher{proc: fp}

	var gotErr error
	var exitCount int
	var mu sync.Mutex
	s, err := StartClaudeSession(context.Background(), ClaudeSessionConfig{
		AgentID:  "agent-1",
		Launcher: lp,
		OnExit: func(err error) {
			mu.Lock()
			gotErr = err
			exitCount++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	crash := errors.New("boom")
	fp.exit(crash)
	s.Wait()

	mu.Lock()
	defer mu.Unlock()
	if exitCount != 1 {
		t.Fatalf("OnExit fired %d times, want 1", exitCount)
	}
	if !errors.Is(gotErr, crash) {
		t.Fatalf("OnExit err: %v, want %v", gotErr, crash)
	}
}

func TestClaudeSession_WritesMCPConfig(t *testing.T) {
	home := t.TempDir()
	b, err := mcphost.GenerateMCPConfig(mcphost.MCPConfigParams{
		ServerName:  "ac",
		Command:     "worker",
		Args:        []string{"mcp-host"},
		AgentID:     "agent-1",
		AdminURL:    "unix:/tmp/admin.sock",
		WorkerToken: "tok",
		AgentRoot:   "/work/agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	fp := newFakeProc()
	lp := &fakeLauncher{proc: fp}
	s, err := StartClaudeSession(context.Background(), ClaudeSessionConfig{
		AgentID:        "agent-1",
		HomeDir:        home,
		Launcher:       lp,
		MCPConfigBytes: b,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		fp.exit(nil)
		s.Wait()
	}()

	wantPath := filepath.Join(home, mcpConfigFileName)
	if lp.gotSpec.MCPConfigPath != wantPath {
		t.Fatalf("spec mcp path: %q want %q", lp.gotSpec.MCPConfigPath, wantPath)
	}
	onDisk, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read mcp-config: %v", err)
	}
	if !bytes.Equal(onDisk, b) {
		t.Fatalf("mcp-config content mismatch:\n got %s\nwant %s", onDisk, b)
	}
}

func TestClaudeSession_RewriteForStreamingInput(t *testing.T) {
	// Mirror what claudecode.Adapter.BuildCommand emits for a long-lived spawn:
	// --output-format stream-json --session-id <id> -p <sentinel-prompt>.
	in := []string{"--output-format", "stream-json", "--session-id", "agent-1", "-p", longLivedSentinelPrompt}
	out := rewriteForStreamingInput(in)
	joined := strings.Join(out, " ")

	// --print PRESENT as a flag (the -p was canonicalised to --print).
	if !contains(out, "--print") {
		t.Fatalf("--print not present: %v", out)
	}
	// The sentinel/positional prompt must be gone, and no bare -p left.
	for _, a := range out {
		if a == "-p" || a == longLivedSentinelPrompt {
			t.Fatalf("sentinel prompt / -p not stripped: %v", out)
		}
	}
	// The three validated stream flags all present.
	if !strings.Contains(joined, "--input-format stream-json") {
		t.Fatalf("missing --input-format stream-json: %v", out)
	}
	if !strings.Contains(joined, "--output-format stream-json") {
		t.Fatalf("missing --output-format stream-json: %v", out)
	}
	if !contains(out, "--verbose") {
		t.Fatalf("missing --verbose: %v", out)
	}
	// session-id preserved.
	if !strings.Contains(joined, "--session-id agent-1") {
		t.Fatalf("session-id dropped: %v", out)
	}
	// No duplicate flags introduced.
	if n := count(out, "--print"); n != 1 {
		t.Fatalf("--print appears %d times: %v", n, out)
	}
	if n := count(out, "--input-format"); n != 1 {
		t.Fatalf("--input-format appears %d times: %v", n, out)
	}
	if n := count(out, "--output-format"); n != 1 {
		t.Fatalf("--output-format appears %d times: %v", n, out)
	}
	if n := count(out, "--verbose"); n != 1 {
		t.Fatalf("--verbose appears %d times: %v", n, out)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func count(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}

// TestAgentSessionUUID validates the agent-id → claude --session-id derivation.
// claude REQUIRES a valid UUID (real claude 2.1.156 rejects a raw ULID), and
// AgentCenter mints agent ids as ULIDs, so the launcher must derive one.
func TestAgentSessionUUID(t *testing.T) {
	// A realistic ULID agent id (what s.idgen.NewULID() produces).
	const ulid = "01J9ZK7QW8X2YB3C4D5E6F7G8H"
	got := agentSessionUUID(ulid)

	// Must be a syntactically valid RFC 4122 UUID: 8-4-4-4-12 lowercase hex,
	// version nibble 5, variant high bits 10.
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(got) {
		t.Fatalf("agentSessionUUID(%q) = %q — not a valid v5 UUID", ulid, got)
	}
	// Deterministic: same agent → same session id (persistent-session intent).
	if again := agentSessionUUID(ulid); again != got {
		t.Fatalf("not deterministic: %q != %q", got, again)
	}
	// Distinct agents → distinct session ids (no "already in use" collisions).
	if other := agentSessionUUID("01J9ZK7QW8X2YB3C4D5E6F7G8J"); other == got {
		t.Fatalf("distinct agent ids collided on session id %q", got)
	}
}
