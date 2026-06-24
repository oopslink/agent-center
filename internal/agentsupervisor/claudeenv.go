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
//   - SETTING SOURCES via `--setting-sources user,project` (set in the claude argv —
//     see claudestream.BuildStreamingArgv), with HOME / CLAUDE_CONFIG_DIR LEFT AT
//     THEIR DEFAULTS — relocating either breaks keychain /login (the credential is
//     bound to the default HOME/~/.claude; empirically confirmed both break it).
//     v2.7 #182 (acceptance FINDING-G) CORRECTION: the earlier value `""` (load NO
//     sources) ALSO suppressed the keychain /login credential — auth loads via the
//     "user" source — so every agent turn 403'd. (The earlier comment here claiming
//     "Tester verified keychain /login works under --setting-sources ''" was WRONG.)
//     We now load "user" (auth + the operator's user-level settings) + "project"
//     (the agent's own <workspace>/.claude). CONSEQUENCE: the operator's USER-level
//     ~/.claude settings (hooks/env/plugins) DO load in the agent — user-level
//     isolation is NOT achieved here (auth and operator user-settings share the same
//     source). Full isolation is deferred to v2.8 via a setup-token
//     (CLAUDE_CODE_OAUTH_TOKEN, which authes independent of setting-sources).

import (
	"sort"
	"strings"
)

// envAllowExact: inherited env var names passed verbatim — the minimal system
// vars the CLI needs to run (≈ the legacy AgentRunner allowlist).
//
// The HTTP-proxy vars are REQUIRED, not optional: in a proxied deployment claude
// reaches the Anthropic API only through the proxy, so stripping them makes the API
// reject the request ("403 Request not allowed" / authentication_failed) — exactly
// the GATE-1 ship-blocker. They are standard routing config that every HTTP-client
// child legitimately inherits, NOT worker secrets (those are AGENT_CENTER_*); no-op
// when unset. Both cases (Go and Node honour either).
var envAllowExact = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
	"LANG": true, "TZ": true, "TMPDIR": true, "SHELL": true,
	// HTTP proxy (routing config — claude needs it to reach the Anthropic API).
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true, "all_proxy": true,
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
//   - CLAUDE_CONFIG_DIR: we deliberately DO NOT relocate the child's config dir —
//     redirecting it breaks keychain /login (the credential is bound to the default
//     ~/.claude; #182 empirically reconfirmed CLAUDE_CONFIG_DIR=<elsewhere> → "Not
//     logged in"). An inherited CLAUDE_CONFIG_DIR must therefore NOT pass through, so
//     the child uses the default $HOME/.claude that keychain /login is bound to.
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

// GitIdentityEnv returns the git author/committer identity env for an agent,
// derived from its stable AgentID (T459). Injecting identity as ENV — not
// `git config` — is the root fix for the recurring commit-author misattribution:
// git's GIT_{AUTHOR,COMMITTER}_{NAME,EMAIL} env outrank `git config user.*` and
// apply to EVERY worktree the agent's process creates. So a dev that enters,
// switches, or forks a worktree can no longer inherit a stale/default identity —
// every commit is attributed to the right agent automatically, with zero
// per-worktree `git config` discipline. This realizes "运行时护栏 > 操作纪律".
//
// NAME is the AgentID (stable, 1:1 with the agent); EMAIL is <agent-id>@agent-center.
// These are DEFAULTS overlaid UNDER the ② seam (see mergeGitIdentity): a future
// human-readable display_name plumbed via AgentEnv (GIT_AUTHOR_NAME=…) overrides
// them with no code change. Empty agentID → nil (no injection; caller no-ops).
func GitIdentityEnv(agentID string) map[string]string {
	if agentID == "" {
		return nil
	}
	email := agentID + "@agent-center"
	return map[string]string{
		"GIT_AUTHOR_NAME":     agentID,
		"GIT_AUTHOR_EMAIL":    email,
		"GIT_COMMITTER_NAME":  agentID,
		"GIT_COMMITTER_EMAIL": email,
	}
}

// mergeGitIdentity overlays the center-injected agentEnv (② seam) ON TOP of the
// AgentID-derived git identity, so an explicit center value (e.g. a display_name
// in GIT_AUTHOR_NAME) wins over the derived default while everything else in
// agentEnv passes through unchanged. Returns agentEnv as-is when agentID is empty.
func mergeGitIdentity(agentID string, agentEnv map[string]string) map[string]string {
	base := GitIdentityEnv(agentID)
	if base == nil {
		return agentEnv
	}
	for k, v := range agentEnv {
		base[k] = v
	}
	return base
}

// BuildClaudeEnv builds the agent-claude child's env (supervisor→claude segment):
//
//	layer 1 — allowlisted system env (default-deny; no worker secrets). HOME and
//	          CLAUDE_CONFIG_DIR are NOT overridden (defaults preserved → keychain
//	          /login intact); setting sources are `--setting-sources user,project` in
//	          the claude argv (#182: user = auth, project = agent's own config).
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
