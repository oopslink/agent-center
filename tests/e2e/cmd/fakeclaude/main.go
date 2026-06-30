// Command fakeclaude is a deployment-level test stand-in for the real `claude`
// CLI. It speaks just enough of the claude 2.1.x stream-json protocol (the
// subset internal/claudestream + internal/agentsupervisor exercise) for the REAL
// worker-daemon supervisor session path to drive it as if it were claude — with
// NO real Anthropic API and NO real claude binary.
//
// It is used by tests/e2e/restart_recovery_e2e_test.go to reproduce the v2.24.0
// restart-recovery fix end-to-end against real processes. It is NOT shipped.
//
// Protocol it implements:
//
//   - `fakeclaude --version`  → prints a version string and exits 0 (so the
//     claudecode adapter Probe reports the CLI available).
//   - streaming invocation (`--print --input-format stream-json --output-format
//     stream-json --session-id <uuid> ...`): on start it emits a `system`/`init`
//     line, then on EACH newline-delimited stdin user message it emits an
//     `assistant` text line and a clean `result` line (subtype=success,
//     is_error=false) — which is what makes the daemon call MarkCompletedTurn +
//     maybeReplyNudge for that turn.
//
// Observability + control (via env, so the harness can both steer and verify):
//
//   - CLAUDE_FAKE_LOG=<path>   : append one line per lifecycle event (START,
//     SESSION_ID, RESUME_FROM, STDIN:<text>, RESULT, HANG, EXIT). The harness
//     greps this file to PROVE what the binary received (e.g. the reply-nudge
//     prompt injected after restart) across the kill/restart boundary. The same
//     path is reused by both the pre-kill and post-restart generations (append),
//     so the whole timeline is in one file.
//   - CLAUDE_FAKE_RESULT_ON_START=1 : emit one clean result immediately at start
//     (before any stdin), so a turn completes even though no message was injected
//     — used to set completed_turn=true deterministically for generation 0.
//   - CLAUDE_FAKE_HANG_AFTER=<n>     : after emitting <n> results total, STOP
//     emitting results: keep reading + logging stdin but never finish the turn
//     (simulates a crash mid-turn — the daemon is then SIGKILLed before a result).
//     n<=0 disables (every message completes).
//
// Every flag other than --version is ignored (claude is invoked with many flags;
// the stand-in only cares about stdin/stdout + the env knobs).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	logMu  sync.Mutex
	logF   *os.File
	result int // results emitted so far
)

func logln(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logF == nil {
		return
	}
	fmt.Fprintf(logF, format+"\n", args...)
	_ = logF.Sync()
}

func main() {
	for _, a := range os.Args[1:] {
		if a == "--version" {
			fmt.Println("1.2.3 (fakeclaude e2e stand-in)")
			return
		}
	}

	if p := strings.TrimSpace(os.Getenv("CLAUDE_FAKE_LOG")); p != "" {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			logF = f
			defer f.Close()
		}
	}

	// Surface the session-id + resume-from from argv so the harness can PROVE
	// the resume-gating behavior (a relaunch that --resumes carries a
	// --resume <prevSessionID> --fork-session pair; a fresh relaunch does not).
	args := os.Args[1:]
	logln("START pid=%d argv=%s", os.Getpid(), strings.Join(args, " "))
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) {
			logln("SESSION_ID %s", args[i+1])
		}
		if a == "--resume" && i+1 < len(args) {
			logln("RESUME_FROM %s", args[i+1])
		}
	}

	hangAfter := 0
	if v := strings.TrimSpace(os.Getenv("CLAUDE_FAKE_HANG_AFTER")); v != "" {
		hangAfter, _ = strconv.Atoi(v)
	}

	out := bufio.NewWriter(os.Stdout)
	emit := func(line string) {
		out.WriteString(line)
		out.WriteByte('\n')
		out.Flush()
	}

	// claude emits a system/init line first; mirror it.
	emit(`{"type":"system","subtype":"init","session_id":"fakeclaude"}`)

	maybeResult := func(reason string) {
		if hangAfter > 0 && result >= hangAfter {
			// Mid-turn: do NOT emit a result. The turn never completes; the
			// daemon will be SIGKILLed in this state.
			logln("HANG reason=%s results_so_far=%d", reason, result)
			return
		}
		emit(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ack: ` + reason + `"}]}}`)
		emit(`{"type":"result","subtype":"success","is_error":false,"result":"done","stop_reason":"end_turn","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`)
		result++
		logln("RESULT n=%d reason=%s", result, reason)
	}

	if strings.TrimSpace(os.Getenv("CLAUDE_FAKE_RESULT_ON_START")) == "1" {
		maybeResult("start")
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		text := extractUserText(line)
		logln("STDIN %s", text)
		maybeResult("inject")
	}
	logln("EXIT pid=%d", os.Getpid())
}

// extractUserText pulls the text out of a stream-json user line
// ({"type":"user","message":{"role":"user","content":[{"type":"text","text":...}]}}).
// On any parse miss it returns the raw line so the harness still sees something.
func extractUserText(line string) string {
	var env struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		return line
	}
	var b strings.Builder
	for _, c := range env.Message.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if b.Len() == 0 {
		return line
	}
	return b.String()
}
