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

// ParseRunnerUsage extracts the aggregate per-run token usage from a runner's
// captured stdout. The default CommandRunner runs the model-routed agent CLI in
// claude stream-json mode; each turn ends with a `result` line carrying that
// turn's token `usage`. This sums the usage across every result line in the run
// (a task may span multiple turns) into one TokenUsage.
//
// It is BEST-EFFORT and format-tolerant: a line that is not a JSON object, or that
// claudestream cannot parse (a codex CLI's output, stderr interleaved by
// CombinedOutput, a banner) is skipped — so a runner that emits no parseable
// result line yields a zero TokenUsage (omitted from output.json) rather than an
// error. The executor stays pure-compute: this only reads text already captured
// and opens no connection.
func ParseRunnerUsage(out string) TokenUsage {
	var u TokenUsage
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
			if ev.Type != "result" {
				continue
			}
			u.InputTokens += ev.TokensIn
			u.OutputTokens += ev.TokensOut
			u.CacheReadTokens += ev.CacheReadTokens
			u.CacheWriteTokens += ev.CacheWriteTokens
		}
	}
	return u
}
