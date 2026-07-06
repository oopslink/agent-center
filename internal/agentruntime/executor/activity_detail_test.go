package executor

import (
	"strings"
	"testing"
)

// asst builds an assistant stream-json line with a single tool_use block.
func asstTool(name, inputJSON string) string {
	return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"` + name + `","input":` + inputJSON + `}]}}`
}

func TestStreamLineActivity_Tools(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		// The detail now renders the REAL, un-redacted tool input as
		// `ToolName(<args>)` — aligned with the supervisor's own activity
		// (AgentActivityRow.preview `${tool_name}(${summarizeArgs(args)})`).
		{"read full path (no basename redaction)", asstTool("Read", `{"file_path":"/Users/x/agent-center/internal/projectmanager/task.go"}`), `Read({"file_path":"/Users/x/agent-center/internal/projectmanager/task.go"})`},
		{"write full path", asstTool("Write", `{"file_path":"web/src/App.tsx"}`), `Write({"file_path":"web/src/App.tsx"})`},
		{"edit full path", asstTool("Edit", `{"file_path":"internal/foo/assign_flow.go"}`), `Edit({"file_path":"internal/foo/assign_flow.go"})`},
		{"grep pattern", asstTool("Grep", `{"pattern":"func Reset"}`), `Grep({"pattern":"func Reset"})`},
		{"glob pattern", asstTool("Glob", `{"pattern":"**/*.go"}`), `Glob({"pattern":"**/*.go"})`},
		{"unknown tool keeps its args", asstTool("WebFetch", `{"url":"https://x"}`), `WebFetch({"url":"https://x"})`},
		{"assistant text → generic label", `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me plan the change to the router"}]}}`, "生成中"},
		// Bash commands now show the FULL command verbatim (args no longer dropped).
		{"bash full command", asstTool("Bash", `{"command":"go test ./..."}`), `Bash({"command":"go test ./..."})`},
		{"bash git push keeps remote/branch", asstTool("Bash", `{"command":"git push origin feat/x"}`), `Bash({"command":"git push origin feat/x"})`},
		{"bash cd && go test (the report case)", asstTool("Bash", `{"command":"cd /x && go test"}`), `Bash({"command":"cd /x && go test"})`},
		{"bash abs-path binary kept as-is", asstTool("Bash", `{"command":"/usr/local/bin/go build"}`), `Bash({"command":"/usr/local/bin/go build"})`},
		// result / system / non-JSON → no activity (caller keeps previous)
		{"result line", `{"type":"result","subtype":"success","result":"done","usage":{"input_tokens":1}}`, ""},
		{"system line", `{"type":"system","subtype":"init"}`, ""},
		{"non-json", `not json at all`, ""},
		{"empty", ``, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := streamLineActivity([]byte(c.line))
			if got != c.want {
				t.Fatalf("streamLineActivity = %q, want %q", got, c.want)
			}
		})
	}
}

// Owner directive ("完全对齐 with supervisor activity"): the executor detail must
// show the REAL command/args, NOT the old sanitized note. These cases prove a
// Bash command's args (paths, flags, sub-commands) are now VISIBLE, not redacted.
func TestStreamLineActivity_BashArgsVisible(t *testing.T) {
	cases := []struct {
		name, cmd, want string
		mustContain     string
	}{
		{"path arg now visible", `cat /etc/passwd`, `Bash({"command":"cat /etc/passwd"})`, "/etc/passwd"},
		{"flag value now visible", `psql -c "SELECT * FROM users"`, `Bash({"command":"psql -c \"SELECT * FROM users\""})`, "SELECT"},
		{"cd chain now visible", `cd /x && go test ./...`, `Bash({"command":"cd /x && go test ./..."})`, "cd /x && go test"},
		{"env prefix now visible", `TOKEN=abc go test ./...`, `Bash({"command":"TOKEN=abc go test ./..."})`, "TOKEN=abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := streamLineActivity([]byte(asstTool("Bash", `{"command":`+jsonQuote(c.cmd)+`}`)))
			if got != c.want {
				t.Fatalf("bash %q → %q, want %q", c.cmd, got, c.want)
			}
			if c.mustContain != "" && !strings.Contains(got, c.mustContain) {
				t.Fatalf("expected %q to contain %q (args must be visible now)", got, c.mustContain)
			}
		})
	}
}

// A pathologically long command is clipped to the (generous) maxDetailLen so a
// note never floods the status file / event, while still preserving enough for
// the expandable view.
func TestStreamLineActivity_Truncation(t *testing.T) {
	long := strings.Repeat("a", maxDetailLen+500)
	got := streamLineActivity([]byte(asstTool("Bash", `{"command":`+jsonQuote(long)+`}`)))
	if !strings.HasPrefix(got, `Bash({"command":"`) || !strings.HasSuffix(got, "…") {
		t.Fatalf("expected a clipped bash note, got %q", got)
	}
	if len([]rune(got)) > maxDetailLen+1 { // cap + the ellipsis rune
		t.Fatalf("note too long (%d runes)", len([]rune(got)))
	}
}

// jsonQuote is a tiny helper to embed an arbitrary command string into JSON.
func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
