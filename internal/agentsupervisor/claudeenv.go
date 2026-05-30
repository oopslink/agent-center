package agentsupervisor

// claudeenv.go — controlled environment + config isolation for the agent-claude
// child (v2.7 security fix: C = worker-secret leak, A = operator ~/.claude
// pollution). @oopslink design: the supervisor PREPARES the agent's launch
// environment; worker-daemon secrets must NOT reach claude.
//
// Two defenses, both default-DENY:
//   - ENV ALLOWLIST: claude (and the supervisor itself) inherit only an allowlist
//     of the system env — the minimal vars the CLI needs to run + claude's OWN
//     auth namespace. Everything else (notably AGENT_CENTER_* worker secrets, and
//     any UNKNOWN worker secret) is structurally dropped. Allowlist > denylist:
//     a secret we never thought of is excluded by default.
//   - CONFIG ISOLATION: claude reads an ISOLATED CLAUDE_CONFIG_DIR (per-agent),
//     not the operator's ~/.claude — so operator hooks/settings/SessionStart do
//     NOT pollute agent behavior. Auth that lives in the operator config dir is
//     symlinked in (auth preserved; hooks/settings not).
//
// 🕒 Three runtime assumptions are NOT self-certified here — Tester's GATE verifies
// them (the fix+verify closure): (1) claude honors CLAUDE_CONFIG_DIR [double
// marker]; (2) auth survives isolation [GATE-0 claude-starts]; (3) no worker
// secret in claude's env [(a)].

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IsolatedConfigDirName is the per-agent-home isolated claude config directory.
const IsolatedConfigDirName = "claude-config"

// claudeAuthFiles are the file names (relative to a claude config dir) treated as
// AUTH (login state) — symlinked into the isolated config dir so isolation does
// not break claude's login. Hooks/settings (settings.json, settings.local.json,
// etc.) are deliberately NOT here → they stay isolated. FLAG (GATE-0): this list
// is a best-effort guess at claude's auth file(s); GATE-0 (claude must start)
// verifies it — if claude can't auth post-isolation, extend this list.
var claudeAuthFiles = []string{".credentials.json"}

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
//   - CLAUDE_CONFIG_DIR: the supervisor SETS it to the isolated dir, so an
//     inherited value must never leak through.
//   - CLAUDE_CODE_* (whole prefix, NO exception): the PARENT claude-code's runtime
//     namespace — the SDK-session markers (SESSION_ID / ENTRYPOINT / EXECPATH /
//     TMPDIR / SSE_PORT / ...) AND the non-interactive subscription auth token
//     CLAUDE_CODE_OAUTH_TOKEN. None are the child's own config; passing the markers
//     would confuse the child into a nested session, and passing an inherited
//     OAUTH_TOKEN would silently OVERRIDE the child's intended keychain /login auth
//     with a non-deterministic, unnecessary inherited credential. They are
//     orthogonal to claude's own auth (ANTHROPIC_* / the remaining CLAUDE_*).
//     Dropping the WHOLE namespace (no exception) is the least-privilege choice and
//     future-proof (new CLAUDE_CODE_* covered automatically); the child auths via
//     its own keychain /login and sets its own runtime markers.
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
//	layer 1 — allowlisted system env (default-deny; no worker secrets)
//	          + CLAUDE_CONFIG_DIR set to the isolated config dir (A isolation)
//	layer 2 — agentEnv overlaid AS-IS (center-injected per-agent env / Profile.EnvVars,
//	          slice ②; NOT allowlist-filtered — intentional, trusted per-agent env)
//
// agentEnv is the ② SEAM — empty for slice ① (security); slice ② fills it. The
// isolated CLAUDE_CONFIG_DIR always wins over any inherited one (set last; also
// excluded from the allowlist).
func BuildClaudeEnv(source []string, claudeConfigDir string, agentEnv map[string]string) []string {
	env := filterAllowedEnv(source)
	if claudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+claudeConfigDir)
	}
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

// operatorClaudeConfigDir resolves the operator's REAL claude config dir from the
// inherited env: an inherited CLAUDE_CONFIG_DIR if present, else $HOME/.claude.
// Empty if neither is resolvable.
func operatorClaudeConfigDir(source []string) string {
	var home string
	for _, kv := range source {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			if v := strings.TrimPrefix(kv, "CLAUDE_CONFIG_DIR="); v != "" {
				return v
			}
		}
		if strings.HasPrefix(kv, "HOME=") {
			home = strings.TrimPrefix(kv, "HOME=")
		}
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// PrepareIsolatedClaudeConfig creates the per-agent isolated claude config dir
// under home and COPIES ONLY auth files in from the operator's real config dir
// (so isolation keeps claude's login but drops hooks/settings/SessionStart).
// Returns the isolated dir to pass as CLAUDE_CONFIG_DIR.
//
// COPY (not symlink): the agent gets a working snapshot of the auth but CANNOT
// write back to the operator's real credential file (write-isolation); a token
// refresh lands in the isolated copy. Re-copied each spawn so rotated creds are
// picked up. Best-effort: a missing operator config / auth file is fine (auth may
// be in env or keychain); GATE-0 (claude must start) is the authoritative check.
func PrepareIsolatedClaudeConfig(home string, source []string) (string, error) {
	dir := filepath.Join(home, IsolatedConfigDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	opDir := operatorClaudeConfigDir(source)
	if opDir == "" || opDir == dir {
		return dir, nil
	}
	for _, name := range claudeAuthFiles {
		src := filepath.Join(opDir, name)
		data, err := os.ReadFile(src)
		if err != nil {
			continue // auth file absent → nothing to copy (env/keychain auth)
		}
		// Refresh each spawn (rotated creds); 0600 (it is a credential).
		_ = os.WriteFile(filepath.Join(dir, name), data, 0o600)
	}
	return dir, nil
}
