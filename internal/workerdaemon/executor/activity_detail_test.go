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
		{"read basename only", asstTool("Read", `{"file_path":"/Users/x/agent-center/internal/projectmanager/task.go"}`), "读 task.go"},
		{"write basename", asstTool("Write", `{"file_path":"web/src/App.tsx"}`), "写 App.tsx"},
		{"edit basename", asstTool("Edit", `{"file_path":"internal/foo/assign_flow.go"}`), "改 assign_flow.go"},
		{"multiedit basename", asstTool("MultiEdit", `{"file_path":"a/b/run.go"}`), "改 run.go"},
		{"notebookedit", asstTool("NotebookEdit", `{"notebook_path":"nb/x.ipynb"}`), "改 x.ipynb"},
		{"grep pattern", asstTool("Grep", `{"pattern":"func Reset"}`), "搜 func Reset"},
		{"glob pattern", asstTool("Glob", `{"pattern":"**/*.go"}`), "找 **/*.go"},
		{"unknown tool", asstTool("WebFetch", `{"url":"https://x"}`), "调 WebFetch"},
		{"assistant text → generic label", `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me plan the change to the router"}]}}`, "生成中"},
		{"bash bareword subcmd", asstTool("Bash", `{"command":"go test ./..."}`), "跑 go test"},
		{"bash git push (drops remote/branch)", asstTool("Bash", `{"command":"git push origin feat/x"}`), "跑 git push"},
		{"bash npm install", asstTool("Bash", `{"command":"npm install"}`), "跑 npm install"},
		{"bash binary only", asstTool("Bash", `{"command":"ls"}`), "跑 ls"},
		{"bash abs-path binary → basename", asstTool("Bash", `{"command":"/usr/local/bin/go build"}`), "跑 go build"},
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

// The core PD-gate guarantee: a Bash command carrying a secret or a path must
// NEVER leak its args/values into the activity note.
func TestStreamLineActivity_BashSanitization(t *testing.T) {
	cases := []struct {
		name, cmd, want string
		mustNotContain  string
	}{
		{"auth header token", `curl -H "Authorization: Bearer sk-secret-123" https://api`, "跑 curl …", "sk-secret"},
		{"env-assign secret prefix", `TOKEN=sk-abc go test ./...`, "跑 go test", "sk-abc"},
		{"path arg hidden", `cat /etc/passwd`, "跑 cat …", "passwd"},
		{"flag value hidden", `psql -c "SELECT * FROM users"`, "跑 psql …", "SELECT"},
		{"pipe/redirect hidden", `echo hi > /root/.ssh/authorized_keys`, "跑 echo …", "authorized_keys"},
		{"only env assign", `FOO=bar`, "跑 …", "bar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := streamLineActivity([]byte(asstTool("Bash", `{"command":`+jsonQuote(c.cmd)+`}`)))
			if got != c.want {
				t.Fatalf("bash %q → %q, want %q", c.cmd, got, c.want)
			}
			if c.mustNotContain != "" && strings.Contains(got, c.mustNotContain) {
				t.Fatalf("LEAK: %q contains %q", got, c.mustNotContain)
			}
		})
	}
}

// Long paths/patterns are clipped so a note stays short (defense-in-depth over the
// 80-rune overall cap).
func TestStreamLineActivity_Truncation(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := streamLineActivity([]byte(asstTool("Read", `{"file_path":"`+long+".go"+`"}`)))
	if !strings.HasPrefix(got, "读 ") || !strings.HasSuffix(got, "…") {
		t.Fatalf("expected clipped read note, got %q", got)
	}
	if len([]rune(got)) > maxDetailLen+2 { // "读 " prefix + cap
		t.Fatalf("note too long (%d runes): %q", len([]rune(got)), got)
	}
}

// jsonQuote is a tiny helper to embed an arbitrary command string into JSON.
func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
