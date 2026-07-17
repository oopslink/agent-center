package executor

// activity_detail.go — turn one claude stream-json line into a "what is the
// executor doing right now" note for the executor.progress `detail` field. It
// reuses the exact parser the bug1 heartbeat already runs per line
// (claudestream.ParseStreamLine), so this adds a lightweight per-line peek, not a
// second pass.
//
// SANITIZATION WAS INTENTIONALLY REMOVED (owner directive — oopslink, "完全对齐
// with supervisor activity"). This file used to redact every tool_use down to a
// binary basename plus a structural hint ("跑 cd …", "读 task.go"), dropping all
// args/paths so no secret could escape. That made the second-level executor
// detail useless (an operator saw "跑 cd …" and learned nothing), and it did NOT
// match how the SUPERVISOR's OWN activity is rendered on the frontend
// (AgentActivityRow.preview case 'tool_use' → `${tool_name}(${summarizeArgs})`,
// the REAL command/args, un-redacted). Per the owner directive the executor's
// second-level detail must be FULLY ALIGNED with the supervisor's: it now renders
// the tool_use the same way — `ToolName(<real args>)` — with the FULL command
// preserved so the frontend's expandable/collapsible view can show it verbatim.
//
// ACCEPTED TRADEOFF (owner-directed): a forked executor's real commands — and any
// secret embedded in a command (a `curl -H "Authorization: Bearer sk-…"`, a
// `TOKEN=sk-… go test`) — are now surfaced in the activity stream, exactly as the
// supervisor's own commands already are. This is the same exposure the supervisor
// activity already carries; the owner chose parity over the executor-only
// redaction.

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// maxDetailLen bounds a rendered detail note. It is deliberately generous: the
// single `detail` field must carry the FULL command so the frontend can render an
// expandable/collapsible view (a short CSS-truncated teaser in the collapsed row,
// the complete command when expanded — AgentActivityRow.ExecutorProgressGroup /
// the executor.progress detail block). The bound only guards against a
// pathologically huge tool input flooding the status file / event.
const maxDetailLen = 2000

// streamLineActivity peeks one stream-json line and returns a note of the
// executor's current action, or "" when there is nothing worth surfacing (a
// non-JSON line, a parse miss, a system/result/tool_result event) — the caller
// then keeps the previous note. A line may carry several events; the LAST tool_use
// wins (the most recent action), else an assistant text block yields a generic
// label.
//
// tools is the greppable identifier set for the line's tool_use events (I109 ②) —
// see toolNames. It is returned SEPARATELY from detail because detail is a rendered,
// length-bounded human note: a long command gets clipped, and the clip can cut away
// the very word (`push`) someone later greps for. tools is small, unclipped, and
// carries the answer to "WHAT ran" independent of how much of the command text fit.
//
// isTool distinguishes "a tool actually ran" from "the model was generating text",
// so the caller can record one progress line per tool call and leave prose to the
// throttled heartbeat.
func streamLineActivity(line []byte) (detail string, tools []string, isTool bool) {
	evs, err := claudestream.ParseStreamLine(line)
	if err != nil {
		return "", nil, false
	}
	for _, ev := range evs {
		switch ev.Type {
		case "tool_use":
			if d := toolActivity(ev.ToolName, ev.ToolInput); d != "" {
				detail = d // last tool_use on the line wins
				isTool = true
				tools = mergeNames(tools, toolNames(ev.ToolName, ev.ToolInput))
			}
		case "assistant_text":
			if detail == "" && strings.TrimSpace(ev.Text) != "" {
				detail = "生成中" // generic label — the assistant CONTENT is not a command
			}
		}
	}
	return clip(detail, maxDetailLen), tools, isTool
}

// toolActivity renders a tool_use exactly as the supervisor's activity does —
// `ToolName(<real args>)` — with the REAL, un-redacted argument content, so the
// executor's second-level detail is aligned byte-for-byte in shape with the
// supervisor's `${tool_name}(${summarizeArgs(args)})`. No field is dropped and no
// value is redacted (owner directive "完全对齐"). An empty tool name yields "".
func toolActivity(name string, input json.RawMessage) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	args := argsSummary(input)
	if args == "" {
		return name
	}
	return name + "(" + args + ")"
}

// Bounds on the extracted name set (I109 ②). Small on purpose: this field exists to
// answer "what ran", not to re-encode the command.
const (
	maxToolNames    = 24
	maxToolNameRune = 48
)

// toolNames returns the greppable identifiers for one tool_use: the tool name
// itself, plus — for a Bash tool — the program (and its subcommand) of EVERY
// segment of the shell command.
//
// WHY (I109 ②). progress.jsonl's `message` is a rendered note bounded at
// maxDetailLen. A long Bash command (`cd /x && go test ./... && git add -A && git
// commit -m '…' && git push origin HEAD`) gets clipped, and the clip lands wherever
// the budget runs out — so the tail verbs vanish. That produced a real false
// negative: `grep -c push progress.jsonl` returned 0 for a run whose reflog proved
// `update by push`, and an executor was very nearly written up for lying about its
// own delivery on the strength of that zero. `git add` / `commit` / `rebase` all
// matched in the same log purely because they happened to sit before the cut.
//
// Splitting the SEGMENTS (not just the leading program) is the point: a && chain is
// several commands, and only naming each one makes the record reflect what ran.
// Both the program and its first non-flag argument are kept, because for the
// multiplexers that matter here the verb is the argument — "git" alone does not
// distinguish an add from a push.
func toolNames(name string, input json.RawMessage) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	names := []string{name}
	if !strings.EqualFold(name, "Bash") {
		return names
	}
	var obj struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &obj); err != nil {
		return names
	}
	return mergeNames(names, commandNames(obj.Command))
}

// shellSep splits a shell command into its separately-executed segments. It is a
// deliberately shallow lexer — enough to name the programs in the pipelines and
// boolean chains an agent actually writes, not a shell grammar. A quoted separator
// yields a spurious extra name, which is a harmless over-report; the failure mode we
// must avoid is UNDER-reporting.
var shellSep = regexp.MustCompile(`&&|\|\||[;|&\n()]+`)

// commandNames extracts the program (and its subcommand) from each segment of cmd.
func commandNames(cmd string) []string {
	var out []string
	for _, seg := range shellSep.Split(cmd, -1) {
		fields := strings.Fields(seg)
		// Skip leading VAR=value assignments and `sudo`-style prefixes so the name is the
		// real program, not the environment it ran in.
		for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "-") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		prog := cleanName(fields[0])
		if prog == "" {
			continue
		}
		out = append(out, prog)
		// The first bare WORD argument is the verb for the multiplexers this exists to
		// catch (`git push`, `go test`, `npm run`). Skipped: flags, and anything with a
		// path separator — the latter because a flag's VALUE is not itself flag-shaped
		// (`git -C /x push` would otherwise name "x" and never reach "push").
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") || strings.ContainsAny(f, `/\`) {
				continue
			}
			if sub := cleanName(f); sub != "" {
				out = append(out, sub)
			}
			break
		}
	}
	return out
}

// cleanName normalizes one extracted token: a path renders as its basename (so
// /usr/local/bin/git greps as "git"), surrounding quotes are dropped, and anything
// implausible as a name (empty, over-long, or containing whitespace) is discarded.
func cleanName(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, `/\`) {
		s = filepath.Base(strings.ReplaceAll(s, `\`, "/"))
	}
	if s == "" || s == "." || s == ".." {
		return ""
	}
	if r := []rune(s); len(r) > maxToolNameRune {
		return ""
	}
	return s
}

// mergeNames appends add to base, dropping duplicates (case-insensitively) and
// stopping at maxToolNames so a pathological command cannot grow the record without
// bound. Order is preserved: the names read in execution order.
func mergeNames(base, add []string) []string {
	seen := make(map[string]bool, len(base)+len(add))
	for _, b := range base {
		seen[strings.ToLower(b)] = true
	}
	for _, a := range add {
		if len(base) >= maxToolNames {
			return base
		}
		k := strings.ToLower(a)
		if seen[k] {
			continue
		}
		seen[k] = true
		base = append(base, a)
	}
	return base
}

// bulkyArgKeys are tool_use input fields that carry a large content BLOB (a whole
// file body, a full replacement string, a notebook cell) rather than a command or
// an identifier. They flood the activity note — a Write's `content` is the entire
// file — so argsSummary drops them and keeps the salient fields (file_path, …).
//
// This is NOT the old blanket redaction (owner directive "完全对齐"): commands,
// paths, patterns, urls and every other arg stay FULLY visible; only these
// oversized editor payloads are elided, because the supervisor's own activity
// never shows them either (its summarizeArgs truncates to 40 chars, so a Write's
// content is clipped off there too). oopslink DM 2026-07-06: a forked executor's
// Write was dumping a whole Go test file into the ACTIVITY panel.
var bulkyArgKeys = map[string]bool{
	"content":    true, // Write
	"new_string": true, // Edit
	"old_string": true, // Edit
	"edits":      true, // MultiEdit
	"new_source": true, // NotebookEdit
}

// argsSummary renders a tool_use's raw input the same way the supervisor frontend
// AgentActivityRow.summarizeArgs does: a bare JSON string value renders unquoted;
// anything else renders as its compact JSON (whitespace stripped so the note is a
// single line). Real commands/args are preserved — nothing is redacted — so a
// Bash command's full text (e.g. `cd /x && go test`) is visible and carried for
// the expandable view. The one exception: oversized editor content blobs
// (bulkyArgKeys) are dropped so a Write/Edit shows its file_path, not the whole
// file body.
func argsSummary(input json.RawMessage) string {
	s := strings.TrimSpace(string(input))
	if s == "" || s == "null" {
		return ""
	}
	// A JSON string value renders unquoted (parity with summarizeArgs'
	// `typeof args === 'string'` branch).
	var str string
	if err := json.Unmarshal(input, &str); err == nil {
		return strings.TrimSpace(str)
	}
	// A JSON object: drop the oversized editor content blobs (keep file_path etc.)
	// so a Write/Edit note doesn't carry the entire file body.
	if stripped, ok := stripBulkyArgs(input); ok {
		return stripped
	}
	// Otherwise compact the JSON (object/array/number) onto a single line —
	// parity with the frontend's JSON.stringify(args).
	var buf bytes.Buffer
	if err := json.Compact(&buf, input); err == nil {
		return buf.String()
	}
	return s
}

// stripBulkyArgs, for a JSON OBJECT input carrying one or more bulkyArgKeys,
// returns the object re-serialized WITHOUT those keys (compact, single line) and
// ok=true. For a non-object, or an object with no bulky key, it returns ok=false
// so the caller keeps the untouched compact form. Marshalling a map sorts keys,
// which is fine for a one-line note.
func stripBulkyArgs(input json.RawMessage) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil || len(obj) == 0 {
		return "", false
	}
	dropped := false
	for k := range bulkyArgKeys {
		if _, ok := obj[k]; ok {
			delete(obj, k)
			dropped = true
		}
	}
	if !dropped {
		return "", false
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// clip trims s to at most n runes, appending an ellipsis when it truncates.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
