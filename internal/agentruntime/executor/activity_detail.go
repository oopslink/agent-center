package executor

// activity_detail.go — T880: turn one claude stream-json line into a SHORT,
// SANITIZED "what is the executor doing right now" note ("读 task.go", "跑 go test",
// "生成中") for the executor.progress `detail` field. It reuses the exact parser the
// bug1 heartbeat already runs per line (claudestream.ParseStreamLine), so this adds
// a lightweight per-line peek, not a second pass.
//
// SANITIZATION IS THE POINT (design + PD gate). Only a tool name plus a truncated,
// STRUCTURAL hint ever escapes to the activity stream:
//   - a file's BASENAME (never its directory),
//   - a Bash BINARY + a clean sub-command WORD (never a flag, path, arg value, or
//     a `VAR=secret` env prefix — a `curl -H "Authorization: Bearer sk-…"` token
//     lives in the args, so Bash args are dropped),
//   - a truncated grep/glob PATTERN.
// Assistant / thinking CONTENT is replaced by a generic "生成中" label. The raw
// ToolInput and the full stream-json line NEVER leave the wrapper.

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// maxDetailLen bounds a rendered detail note (defense-in-depth over the per-field
// truncation below).
const maxDetailLen = 80

// bashWord matches a clean, secret-free command token: a Bash sub-command like
// "test" / "push" / "install". No leading "-" (a flag), no "." or "/" (a path or
// filename), no "=" (an env value) — so nothing that could carry a path or secret
// passes. Used for the Bash sub-command; the binary basename uses binWord (which
// also allows "." for script names like deploy.sh).
var (
	bashWord = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_:-]{0,20}$`)
	binWord  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_:.-]{0,20}$`)
)

// subcmdTools are the well-known multi-command dev tools whose FIRST positional is
// a verb sub-command (git push, go test, npm install) — safe to surface. For any
// OTHER binary the first positional could be a value/token/path (e.g. `mytool
// sk-secret`), so we show the binary alone. This is the guard that keeps a
// bareword-shaped secret out of the note even when it passes bashWord.
var subcmdTools = map[string]bool{
	"git": true, "go": true, "npm": true, "pnpm": true, "yarn": true, "bun": true,
	"cargo": true, "docker": true, "kubectl": true, "make": true, "pip": true,
	"pip3": true, "gh": true, "terraform": true, "deno": true, "brew": true,
	"apt": true, "apt-get": true, "systemctl": true, "helm": true, "gcloud": true,
	"aws": true, "poetry": true, "uv": true, "gradle": true, "mvn": true,
}

// streamLineActivity peeks one stream-json line and returns a short sanitized note
// of the executor's current action, or "" when there is nothing worth surfacing (a
// non-JSON line, a parse miss, a system/result/tool_result event) — the caller then
// keeps the previous note. A line may carry several events; the LAST tool_use wins
// (the most recent action), else an assistant text block yields the generic label.
func streamLineActivity(line []byte) string {
	evs, err := claudestream.ParseStreamLine(line)
	if err != nil {
		return ""
	}
	detail := ""
	for _, ev := range evs {
		switch ev.Type {
		case "tool_use":
			if d := toolActivity(ev.ToolName, ev.ToolInput); d != "" {
				detail = d // last tool_use on the line wins
			}
		case "assistant_text":
			if detail == "" && strings.TrimSpace(ev.Text) != "" {
				detail = "生成中" // generic label — never the assistant CONTENT
			}
		}
	}
	return clip(detail, maxDetailLen)
}

// toolActivity renders a tool_use as a sanitized note from its name + one
// structural param. Unknown tools fall back to the (clipped) tool name alone.
func toolActivity(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m) // best-effort; m stays nil on any error
	str := func(k string) string {
		s, _ := m[k].(string)
		return strings.TrimSpace(s)
	}
	switch name {
	case "Read":
		if p := str("file_path"); p != "" {
			return "读 " + baseName(p)
		}
		return "读文件"
	case "Write":
		if p := str("file_path"); p != "" {
			return "写 " + baseName(p)
		}
		return "写文件"
	case "Edit", "MultiEdit":
		if p := str("file_path"); p != "" {
			return "改 " + baseName(p)
		}
		return "改文件"
	case "NotebookEdit":
		if p := str("notebook_path"); p != "" {
			return "改 " + baseName(p)
		}
		return "改文件"
	case "Bash":
		return "跑 " + bashSummary(str("command"))
	case "Grep":
		if p := str("pattern"); p != "" {
			return "搜 " + clip(p, 40)
		}
		return "搜索"
	case "Glob":
		if p := str("pattern"); p != "" {
			return "找 " + clip(p, 40)
		}
		return "查找文件"
	case "":
		return ""
	default:
		return "调 " + clip(name, 24)
	}
}

// bashSummary renders a Bash command as a SECRET-SAFE hint: the binary basename,
// plus — only when the next token is a clean sub-command word — that word. Never a
// flag, path, arg value, or a leading `VAR=value` env prefix (its value may be a
// secret). Examples: `go test ./...`→"go test", `git push origin main`→"git push",
// `curl -H "Authorization: …"`→"curl …", `TOKEN=sk-x go test`→"go test", `cat
// /etc/passwd`→"cat …".
func bashSummary(cmd string) string {
	fields := strings.Fields(cmd)
	// Drop leading `VAR=value` env-assignment prefixes — the value may be a secret.
	for len(fields) > 0 && isEnvAssign(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return "…"
	}
	bin := path.Base(fields[0])
	if !binWord.MatchString(bin) {
		return "…" // an unusual binary token → do not echo it
	}
	// Surface the sub-command verb ONLY for known multi-command tools; for any other
	// binary the first positional may be a secret/token/path, so show the binary alone.
	if subcmdTools[bin] && len(fields) >= 2 && bashWord.MatchString(fields[1]) {
		return bin + " " + fields[1]
	}
	if len(fields) >= 2 {
		return bin + " …" // args exist but are hidden (not a known-tool sub-command)
	}
	return bin
}

// isEnvAssign reports whether s is a `KEY=value` env-assignment prefix (the key,
// before the first "=", is a bare word with no path separator) — such a value may
// carry a secret and must never be surfaced.
func isEnvAssign(s string) bool {
	i := strings.IndexByte(s, '=')
	return i > 0 && !strings.ContainsAny(s[:i], "/\\.")
}

// baseName is path.Base clipped — the file's name without its (possibly sensitive)
// directory.
func baseName(p string) string {
	return clip(path.Base(strings.TrimSpace(p)), 48)
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
