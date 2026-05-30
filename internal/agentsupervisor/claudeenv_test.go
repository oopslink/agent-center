package agentsupervisor

import (
	"strings"
	"testing"
)

// envMap parses a []string of KEY=VALUE into a map for assertions.
func envMap(entries []string) map[string]string {
	m := map[string]string{}
	for _, e := range entries {
		if i := strings.IndexByte(e, '='); i > 0 {
			// last value wins (matches exec semantics for duplicate keys).
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

// TestBuildClaudeEnv_AllowlistDropsWorkerSecretsKeepsClaudeAuth is the core (C)
// security unit assertion: worker-daemon secrets (and any UNKNOWN var) are
// structurally dropped (default-deny), while claude's OWN auth namespace and the
// safe system vars are kept. It also pins the v2.7 A-isolation choice: HOME is NOT
// relocated (it passes through unchanged — keychain /login is bound to it), and an
// inherited CLAUDE_CONFIG_DIR never passes through (no relocation; the child uses
// the default $HOME/.claude). A isolation itself is the `--setting-sources ""`
// claude argv flag, asserted in the claudestream package.
func TestBuildClaudeEnv_AllowlistDropsWorkerSecretsKeepsClaudeAuth(t *testing.T) {
	source := []string{
		"PATH=/usr/bin", "HOME=/home/op", "LANG=en_US.UTF-8", "LC_ALL=C", "TZ=UTC",
		"ANTHROPIC_API_KEY=sk-ant-xxx",          // claude's own auth — MUST keep
		"CLAUDE_FOO=keep",                       // claude's own (non-CODE) namespace — keep
		"AGENT_CENTER_ADMIN_TOKEN=acat_secret",  // worker secret — MUST drop
		"SOME_UNKNOWN_SECRET=leak",              // unknown var — default-deny drop
		"CLAUDE_CONFIG_DIR=/home/op/.claude",    // inherited — must NOT pass through (no relocation)
		"CLAUDE_CODE_SESSION_ID=parent-sess",    // parent SDK-runtime marker — MUST drop
		"CLAUDE_CODE_ENTRYPOINT=cli",            // parent SDK-runtime marker — MUST drop
		"CLAUDE_CODE_OAUTH_TOKEN=sub-oauth-xxx", // parent token (v2.7 /login-only, no carve-out) — MUST drop
	}
	got := envMap(BuildClaudeEnv(source, nil))

	// Kept: safe system + claude's own (non-CODE) auth/config namespace.
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "ANTHROPIC_API_KEY", "CLAUDE_FOO"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("allowlisted var %q was dropped", k)
		}
	}
	// A isolation is NOT done by relocating HOME — the inherited HOME passes through
	// UNCHANGED (keychain /login is bound to it; relocating it breaks auth).
	if got["HOME"] != "/home/op" {
		t.Fatalf("HOME = %q, want the inherited value unchanged (must NOT be relocated)", got["HOME"])
	}
	// Dropped: worker secret + unknown (the C fix).
	if _, ok := got["AGENT_CENTER_ADMIN_TOKEN"]; ok {
		t.Fatal("worker secret AGENT_CENTER_ADMIN_TOKEN leaked into claude env (C not fixed)")
	}
	if _, ok := got["SOME_UNKNOWN_SECRET"]; ok {
		t.Fatal("unknown var leaked — default-deny allowlist failed")
	}
	// Dropped: the WHOLE parent CLAUDE_CODE_* namespace (NO exception) — SDK-runtime
	// markers (prevent nested-session confusion) AND the inherited OAUTH_TOKEN
	// (v2.7 /login-only: must not override the child's keychain auth) — even though
	// they match the CLAUDE_ allow-prefix.
	for _, k := range []string{"CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if _, ok := got[k]; ok {
			t.Fatalf("parent CLAUDE_CODE_* var %q leaked into child claude env (CLAUDE_CODE_ deny failed)", k)
		}
	}
	// We never relocate the config dir, and an inherited CLAUDE_CONFIG_DIR must NOT
	// pass through → the child uses the default $HOME/.claude that keychain /login is
	// bound to.
	if v, ok := got["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatalf("CLAUDE_CONFIG_DIR present (%q) — must be absent (no relocation; inherited must not pass through)", v)
	}
}

// TestBuildClaudeEnv_AgentEnvOverlaidAsIs pins PM's layering: the ② seam
// (center-injected Profile.EnvVars) is overlaid AS-IS — NOT allowlist-filtered —
// so even a name outside the allowlist appears (it is intentional, trusted env).
func TestBuildClaudeEnv_AgentEnvOverlaidAsIs(t *testing.T) {
	source := []string{"PATH=/usr/bin"}
	agentEnv := map[string]string{
		"AC_TEST_INJECT":  "marker",  // a Profile.EnvVars value
		"NOT_ALLOWLISTED": "present", // outside the allowlist, but intentional → kept
	}
	got := envMap(BuildClaudeEnv(source, agentEnv))
	if got["AC_TEST_INJECT"] != "marker" {
		t.Fatalf("injected Profile.EnvVars dropped: %v", got)
	}
	if got["NOT_ALLOWLISTED"] != "present" {
		t.Fatal("AgentEnv overlay must be AS-IS (not allowlist-filtered) — a non-allowlisted intentional var was dropped")
	}
}

// TestBuildClaudeEnv_KeepsProxyVars pins the GATE-1 fix: the standard HTTP-proxy
// routing vars MUST pass through (claude reaches the Anthropic API only through the
// proxy in a proxied deployment; stripping them → 403 authentication_failed). They
// are routing config, NOT worker secrets — the AGENT_CENTER_* drop is unaffected.
func TestBuildClaudeEnv_KeepsProxyVars(t *testing.T) {
	source := []string{
		"HTTP_PROXY=http://p:8080", "HTTPS_PROXY=http://p:8080", "NO_PROXY=localhost",
		"http_proxy=http://p:8080", "https_proxy=http://p:8080", "no_proxy=localhost",
		"AGENT_CENTER_ADMIN_TOKEN=acat_secret", // a worker secret — MUST still drop
	}
	got := envMap(BuildClaudeEnv(source, nil))
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("proxy var %q must pass through (claude needs it to reach the API through a proxy)", k)
		}
	}
	if _, ok := got["AGENT_CENTER_ADMIN_TOKEN"]; ok {
		t.Fatal("worker secret leaked — proxy allowlist must not weaken the secret drop")
	}
}

// TestBuildSupervisorEnv_StripsWorkerSecrets pins the defense-in-depth ⑤: the
// supervisor's OWN env also drops worker secrets.
func TestBuildSupervisorEnv_StripsWorkerSecrets(t *testing.T) {
	got := envMap(BuildSupervisorEnv([]string{
		"PATH=/usr/bin", "AGENT_CENTER_ADMIN_TOKEN=acat_secret", "ANTHROPIC_API_KEY=sk-x",
	}))
	if _, ok := got["AGENT_CENTER_ADMIN_TOKEN"]; ok {
		t.Fatal("supervisor env still carries worker secret (⑤ depth not applied)")
	}
	if _, ok := got["PATH"]; !ok {
		t.Fatal("supervisor env lost PATH")
	}
}
