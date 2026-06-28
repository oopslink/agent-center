package workerdaemon

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestCodexSession_RealCodex_EndToEnd drives the REAL codex binary end-to-end
// through CodexSession: it spawns `codex exec --json`, maps the live JSONL to
// claudestream.StreamEvent, captures the thread_id, and continues the
// conversation with `codex exec resume`. It proves the whole codex execution path
// works against the actual CLI (validated on codex-cli 0.137.0).
//
// Env-gated (AC_CODEX_E2E=1) because it needs the codex binary on PATH, a logged-
// in codex (ChatGPT/API), network egress, and consumes model tokens — so it must
// NOT run in the normal `go test ./...` suite. Run it with:
//
//	AC_CODEX_E2E=1 LITELLM_KEY=... \
//	  go test ./internal/workerdaemon/ -run TestCodexSession_RealCodex_EndToEnd -v
//
// (unset HTTP(S)_PROXY first per the codex networking rule).
func TestCodexSession_RealCodex_EndToEnd(t *testing.T) {
	if os.Getenv("AC_CODEX_E2E") != "1" {
		t.Skip("set AC_CODEX_E2E=1 to run the real-codex end-to-end test")
	}

	workspace := t.TempDir()
	h := newHarness()
	s, err := StartCodexSession(context.Background(), CodexSessionConfig{
		AgentID:  "e2e-codex",
		TasksDir: workspace,
		OnEvent:  h.onEvent,
		OnExit:   h.onExit,
		Logger:   func(m string) { t.Log(m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Turn 1 (fresh): a trivial, tool-free prompt.
	if err := s.Inject(context.Background(), "Reply with exactly the word PONG and nothing else. Do not use any tools."); err != nil {
		t.Fatal(err)
	}
	r1 := waitResultTimeout(t, h, 150*time.Second)
	if r1.IsError {
		t.Fatalf("turn 1 ended in error: %q", r1.Result)
	}
	if tid := s.ThreadID(); tid == "" {
		t.Fatal("turn 1 did not capture a thread_id")
	}
	if !assistantSaid(h, "PONG") {
		t.Fatalf("turn 1 assistant did not say PONG; events=%v", textsOf(h))
	}

	// Turn 2 (resume): relies on conversation continuity via the captured thread_id.
	if err := s.Inject(context.Background(), "What word did you just say? Reply with that one word only."); err != nil {
		t.Fatal(err)
	}
	r2 := waitResultTimeout(t, h, 150*time.Second)
	if r2.IsError {
		t.Fatalf("turn 2 (resume) ended in error: %q", r2.Result)
	}
	if !assistantSaid(h, "PONG") {
		t.Fatalf("resume continuity failed; assistant did not recall PONG; events=%v", textsOf(h))
	}
	t.Logf("real codex e2e OK: thread_id=%s", s.ThreadID())
}

func waitResultTimeout(t *testing.T, h *sessionHarness, d time.Duration) (ev streamResult) {
	t.Helper()
	select {
	case e := <-h.results:
		return streamResult{IsError: e.IsError, Result: e.Result}
	case <-time.After(d):
		t.Fatalf("timeout (%s) waiting for codex turn result; events so far=%v", d, textsOf(h))
		return
	}
}

type streamResult struct {
	IsError bool
	Result  string
}

func assistantSaid(h *sessionHarness, want string) bool {
	for _, ev := range h.snapshot() {
		if ev.Type == "assistant_text" && strings.Contains(strings.ToUpper(ev.Text), strings.ToUpper(want)) {
			return true
		}
	}
	return false
}

func textsOf(h *sessionHarness) []string {
	var out []string
	for _, ev := range h.snapshot() {
		if ev.Type == "assistant_text" {
			out = append(out, ev.Text)
		}
	}
	return out
}
