package executor

// codex_usage.go — T969: the codex analogue of ParseRunnerStream. A cli=codex executor
// now runs `codex exec --json` (was plain text), so its captured stdout is codex's own
// JSONL event stream — NOT claude stream-json. This parser extracts the run's final
// result text, the codex thread_id, and token usage, so the orchestrator can persist the
// thread_id into Record.SessionID for tier-1 `codex exec resume <thread_id>` recovery.

import (
	"bufio"
	"encoding/json"
	"strings"
)

// ParseCodexRunnerStream extracts the final result TEXT, the codex thread_id, and
// aggregate token usage from a `codex exec --json` run's captured stdout in a single
// pass. Codex's event stream (thread.started / item.completed{agent_message} /
// turn.completed) differs from claude stream-json, so it needs its own lean parser: the
// executor package cannot import the daemon's mapCodexLine (import cycle), and the
// minimal shape below is cheaper to duplicate than to relocate. BEST-EFFORT +
// format-tolerant: non-JSON / unrecognized lines are skipped.
//
// The returned threadID is what the orchestrator persists into Record.SessionID for
// tier-1 resume; an EMPTY threadID (thread.started never arrived / stream truncated)
// signals the caller to degrade to tier-2 rerun (fail-loud), never a resume-with-empty-id.
func ParseCodexRunnerStream(out string) (result, threadID string, usage TokenUsage) {
	var lastAgentMsg string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), maxRunnerLineBytes)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// Fast-skip the non-JSON majority (banners, stderr) before a JSON decode.
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" {
				threadID = ev.ThreadID
			}
		case "item.completed":
			// The last agent_message is the run's final answer (mirrors ParseRunnerStream
			// keeping the last non-empty result text).
			if ev.Item.Type == "agent_message" && strings.TrimSpace(ev.Item.Text) != "" {
				lastAgentMsg = ev.Item.Text
			}
		case "turn.completed":
			if ev.Usage != nil {
				usage.InputTokens += ev.Usage.InputTokens
				usage.OutputTokens += ev.Usage.OutputTokens
			}
		}
	}
	return strings.TrimSpace(lastAgentMsg), threadID, usage
}
