package agentsupervisor

import (
	"os"
	"path/filepath"
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
// safe system vars are kept. Also pins the A isolation: CLAUDE_CONFIG_DIR is set
// to the isolated dir and an inherited one never leaks through.
func TestBuildClaudeEnv_AllowlistDropsWorkerSecretsKeepsClaudeAuth(t *testing.T) {
	source := []string{
		"PATH=/usr/bin", "HOME=/home/op", "LANG=en_US.UTF-8", "LC_ALL=C", "TZ=UTC",
		"ANTHROPIC_API_KEY=sk-ant-xxx",          // claude's own auth — MUST keep
		"CLAUDE_FOO=keep",                       // claude's own (non-CODE) namespace — keep
		"AGENT_CENTER_ADMIN_TOKEN=acat_secret",  // worker secret — MUST drop
		"SOME_UNKNOWN_SECRET=leak",              // unknown var — default-deny drop
		"CLAUDE_CONFIG_DIR=/home/op/.claude",    // inherited — must NOT leak through
		"CLAUDE_CODE_SESSION_ID=parent-sess",    // parent SDK-runtime marker — MUST drop
		"CLAUDE_CODE_ENTRYPOINT=cli",            // parent SDK-runtime marker — MUST drop
		"CLAUDE_CODE_OAUTH_TOKEN=sub-oauth-xxx", // parent token (v2.7 /login-only, no carve-out) — MUST drop
	}
	got := envMap(BuildClaudeEnv(source, "/agent/home/claude-config", nil))

	// Kept: safe system + claude's own (non-CODE) auth/config namespace.
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "ANTHROPIC_API_KEY", "CLAUDE_FOO"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("allowlisted var %q was dropped", k)
		}
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
	// Isolation: our CLAUDE_CONFIG_DIR wins, the operator's does not leak.
	if got["CLAUDE_CONFIG_DIR"] != "/agent/home/claude-config" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want the isolated dir (inherited must not leak)", got["CLAUDE_CONFIG_DIR"])
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
	got := envMap(BuildClaudeEnv(source, "/iso", agentEnv))
	if got["AC_TEST_INJECT"] != "marker" {
		t.Fatalf("injected Profile.EnvVars dropped: %v", got)
	}
	if got["NOT_ALLOWLISTED"] != "present" {
		t.Fatal("AgentEnv overlay must be AS-IS (not allowlist-filtered) — a non-allowlisted intentional var was dropped")
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

// TestPrepareIsolatedClaudeConfig_CopiesAuthNotSettings pins the A fix: the
// isolated config dir gets the auth file (so claude keeps login) but NOT the
// operator's hooks/settings (so they cannot pollute the agent).
func TestPrepareIsolatedClaudeConfig_CopiesAuthNotSettings(t *testing.T) {
	opHome := t.TempDir()
	opConfig := filepath.Join(opHome, ".claude")
	if err := os.MkdirAll(opConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	// Operator config: an auth file (copy) + a settings/hook file (must NOT copy).
	if err := os.WriteFile(filepath.Join(opConfig, ".credentials.json"), []byte(`{"token":"op-auth"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opConfig, "settings.json"), []byte(`{"hooks":{"SessionStart":"evil"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	agentHome := t.TempDir()
	source := []string{"HOME=" + opHome} // → operator config = opHome/.claude
	dir, err := PrepareIsolatedClaudeConfig(agentHome, source)
	if err != nil {
		t.Fatalf("PrepareIsolatedClaudeConfig: %v", err)
	}
	if dir != filepath.Join(agentHome, IsolatedConfigDirName) {
		t.Fatalf("isolated dir = %q, unexpected", dir)
	}
	// Auth COPIED in (claude keeps login under isolation).
	if b, err := os.ReadFile(filepath.Join(dir, ".credentials.json")); err != nil || string(b) != `{"token":"op-auth"}` {
		t.Fatalf("auth file not copied into isolated dir: err=%v", err)
	}
	// settings/hook NOT copied (isolation: operator config cannot pollute).
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("operator settings.json leaked into isolated config dir (A not isolated)")
	}
}
