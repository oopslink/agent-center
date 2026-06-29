package executor

// usage.go — F2 (v2.20.0 / T613) executor-side per-run token accounting. The
// executor is PURE COMPUTE and never connects to the center; it only PARSES the
// token counts out of the runner output it already captured and records them in
// output.json. The orchestrator's writeback (the only party with center
// credentials) relays them to report_usage, tagged with input.json's Source.TaskRef.

import (
	"bufio"
	"strings"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// maxRunnerLineBytes bounds one scanned runner line. A claude stream-json result
// line carries the whole turn result string, which can be large; the default
// bufio.Scanner 64KiB cap would silently truncate (and so drop) such a line's
// usage. 4 MiB matches the daemon's other stream-line ceilings.
const maxRunnerLineBytes = 4 * 1024 * 1024

// ParseRunnerStream extracts the run's final result TEXT and aggregate token usage
// from a runner's captured stdout in a SINGLE pass. The default CommandRunner runs
// the model-routed agent CLI in claude stream-json mode (--output-format
// stream-json --verbose, T622): each turn ends with a `result` line carrying that
// turn's final text + token `usage`. This returns the LAST non-empty result-line
// text (the run's final answer) and the per-field usage summed across every result
// line (a task may span multiple turns).
//
// It is BEST-EFFORT and format-tolerant: a line that is not a JSON object, or that
// claudestream cannot parse (a codex CLI's plain output, stderr interleaved by
// CombinedOutput, a banner) is skipped. When no result-line text is found it falls
// back to the accumulated assistant text; a fully non-stream output yields an empty
// result (the caller relays the raw output instead) and a zero usage. The executor
// stays pure-compute: this only reads text already captured and opens no connection.
func ParseRunnerStream(out string) (result string, usage TokenUsage) {
	var assistant strings.Builder
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), maxRunnerLineBytes)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// Fast-skip the non-JSON majority (banners, plain CLI text, stderr) before
		// paying for a JSON decode; stream-json lines are always JSON objects.
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		evs, err := claudestream.ParseStreamLine([]byte(line))
		if err != nil {
			continue
		}
		for _, ev := range evs {
			switch ev.Type {
			case "result":
				usage.InputTokens += ev.TokensIn
				usage.OutputTokens += ev.TokensOut
				usage.CacheReadTokens += ev.CacheReadTokens
				usage.CacheWriteTokens += ev.CacheWriteTokens
				if strings.TrimSpace(ev.Result) != "" {
					result = ev.Result // last non-empty result-line text = the run's final answer
				}
			case "assistant_text":
				if ev.Text != "" {
					if assistant.Len() > 0 {
						assistant.WriteString("\n")
					}
					assistant.WriteString(ev.Text)
				}
			}
		}
	}
	if strings.TrimSpace(result) == "" {
		result = strings.TrimSpace(assistant.String())
	}
	return result, usage
}

// ParseRunnerUsage returns just the aggregate per-run token usage (the usage half
// of ParseRunnerStream), for callers that only need accounting.
func ParseRunnerUsage(out string) TokenUsage {
	_, u := ParseRunnerStream(out)
	return u
}
