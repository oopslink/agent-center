package agentsupervisor

// claudeenv.go — controlled environment for the agent-claude child (v2.7 security
// fix: C = worker-secret leak, A = operator ~/.claude pollution). @oopslink design:
// the supervisor PREPARES the agent's launch environment; worker-daemon secrets
// must NOT reach claude.
//
// Two defenses:
//   - ENV ALLOWLIST (default-DENY): claude (and the supervisor itself) inherit only
//     an allowlist of the system env — the minimal vars the CLI needs to run +
//     claude's OWN auth namespace. Everything else (notably AGENT_CENTER_* worker
//     secrets, and any UNKNOWN worker secret) is structurally dropped. Allowlist >
//     denylist: a secret we never thought of is excluded by default.
//   - A ISOLATION via `--setting-sources ""` (set in the claude argv — see
//     claudestream.BuildStreamingArgv), NOT by relocating the config dir: claude is
//     told to load NO settings sources, so the operator's ~/.claude
//     hooks/settings/SessionStart do NOT run in the agent. HOME / CLAUDE_CONFIG_DIR
//     are LEFT AT THEIR DEFAULTS — relocating either breaks keychain /login (the
//     credential is bound to the original HOME/config path; Tester verified that a
//     CLAUDE_CONFIG_DIR override AND a HOME override both break /login). Auth is thus
//     the operator's untouched keychain login; no auth files are copied.
//
// 🕒 Runtime assumptions verified by Tester's GATE (the fix+verify closure):
// keychain /login authes under the default HOME/config; `--setting-sources ""`
// suppresses operator hooks (operator reverse-marker = 0); no worker secret reaches
// claude's env; the agent can still invoke its MCP tools under suppressed settings.

import (
	"sort"
	"strings"
)

// envAllowExact: inherited env var names passed verbatim — the minimal system
// vars the CLI needs to run (≈ the legacy AgentRunner allowlist).
var envAllowExact = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
	"LANG": true, "TZ": true, "TMPDIR": true, "SHELL": true,
}

// envAllowPrefix: pass any inherited var whose name starts with one of these —
// locale (LC_*) + claude's OWN auth/config namespaces (ANTHROPIC_*, CLAUDE_*).
// claude's own auth (e.g. ANTHROPIC_API_KEY) is REQUIRED and is NOT a worker
// secret. Worker secrets live under AGENT_CENTER_* (NOT allowlisted → dropped).
var envAllowPrefix = []string{"LC_", "ANTHROPIC_", "CLAUDE_"}

// envAllowed reports whether an inherited env var name is allowlisted.
//
// Two CLAUDE_* exclusions are applied even though they match the CLAUDE_ allow
// -prefix below:
//   - CLAUDE_CONFIG_DIR: we deliberately DO NOT relocate the child's config dir (A
//     isolation is done IN-PLACE via `--setting-sources ""`, not by redirecting
//     config — redirecting breaks keychain /login). An inherited CLAUDE_CONFIG_DIR
//     must therefore NOT pass through, so the child uses the default $HOME/.claude
//     that keychain /login is bound to.
//   - CLAUDE_CODE_* (whole prefix, NO exception): the PARENT claude-code's runtime
//     namespace — the SDK-session markers (SESSION_ID / ENTRYPOINT / EXECPATH /
//     TMPDIR / SSE_PORT / ...) AND the non-interactive subscription auth token
//     CLAUDE_CODE_OAUTH_TOKEN. None are the child's own config; passing the markers
//     would confuse the child into a nested session, and passing an inherited
//     OAUTH_TOKEN would silently OVERRIDE the child's intended keychain /login auth
//     with a non-deterministic, unnecessary inherited credential. Dropping the WHOLE
//     namespace (no exception) is the least-privilege choice and future-proof (new
//     CLAUDE_CODE_* covered automatically); the child auths via its own keychain
//     /login and sets its own runtime markers.
//
// (v2.7 = /login keychain auth ONLY. The token auth path — CLAUDE_CODE_OAUTH_TOKEN
// via `claude setup-token` — is DEFERRED; when it lands, re-introduce a carve-out
// for CLAUDE_CODE_OAUTH_TOKEN TOGETHER WITH its authed-run e2e test, not before.)
func envAllowed(name string) bool {
	if name == "CLAUDE_CONFIG_DIR" || strings.HasPrefix(name, "CLAUDE_CODE_") {
		return false
	}
	if envAllowExact[name] {
		return true
	}
	for _, p := range envAllowPrefix {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// filterAllowedEnv returns the subset of source ("KEY=VALUE" entries) whose names
// are allowlisted — the default-deny system-env filter.
func filterAllowedEnv(source []string) []string {
	out := make([]string, 0, len(source))
	for _, kv := range source {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		if envAllowed(kv[:i]) {
			out = append(out, kv)
		}
	}
	return out
}

// BuildSupervisorEnv builds the supervisor process's OWN env (daemon→supervisor
// segment, defense-in-depth ⑤): the allowlisted system env only — no worker
// secrets. The supervisor is trusted code but never needs worker secrets (the
// mcp-config token is a file path, not env), so they are stripped at the source.
func BuildSupervisorEnv(source []string) []string {
	return filterAllowedEnv(source)
}

// BuildClaudeEnv builds the agent-claude child's env (supervisor→claude segment):
//
//	layer 1 — allowlisted system env (default-deny; no worker secrets). HOME and
//	          CLAUDE_CONFIG_DIR are NOT overridden (defaults preserved → keychain
//	          /login intact); A isolation is the claude argv flag `--setting-sources ""`.
//	layer 2 — agentEnv overlaid AS-IS (center-injected per-agent env / Profile.EnvVars,
//	          slice ②; NOT allowlist-filtered — intentional, trusted per-agent env)
//
// agentEnv is the ② SEAM — empty for slice ① (security); slice ② fills it.
func BuildClaudeEnv(source []string, agentEnv map[string]string) []string {
	env := filterAllowedEnv(source)
	// layer 2: overlay center-injected agent env AS-IS (deterministic order).
	keys := make([]string, 0, len(agentEnv))
	for k := range agentEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+agentEnv[k])
	}
	return env
}
