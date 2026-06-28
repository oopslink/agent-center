package executor

import (
	"strings"
	"testing"
)

// envMap parses "KEY=VALUE" slices into a map for assertions.
func envMap(kvs []string) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestBuildExecutorEnv_AllowsSystemAndAuth(t *testing.T) {
	src := []string{
		"PATH=/usr/bin",
		"HOME=/home/agent",
		"LANG=en_US.UTF-8",
		"LC_ALL=C",
		"TZ=UTC",
		"HTTPS_PROXY=http://proxy:8080",
		"ANTHROPIC_API_KEY=sk-secret-but-claudes-own",
		"CLAUDE_FOO=bar",
	}
	got := envMap(BuildExecutorEnv(src, nil))
	for _, k := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "HTTPS_PROXY", "ANTHROPIC_API_KEY", "CLAUDE_FOO"} {
		if _, ok := got[k]; !ok {
			t.Errorf("expected allowlisted %q to be present", k)
		}
	}
}

func TestBuildExecutorEnv_StripsCenterAndUnknownSecrets(t *testing.T) {
	src := []string{
		"AC_MCP_AGENT_ID=agent-1",
		"AC_MCP_WORKER_TOKEN=tok-secret",
		"AC_MCP_ADMIN_URL=unix:/tmp/admin.sock",
		"AC_MCP_AGENT_ROOT=/home/agent",
		"AC_MCP_SERVER_FINGERPRINT=ff:ee",
		"AGENT_CENTER_DB=postgres://secret",
		"SOME_UNKNOWN_SECRET=nope",
		"CLAUDE_CONFIG_DIR=/elsewhere",
		"CLAUDE_CODE_OAUTH_TOKEN=inherited-token",
		"CLAUDE_CODE_SESSION_ID=parent-session",
	}
	got := envMap(BuildExecutorEnv(src, nil))
	for k := range got {
		if strings.HasPrefix(k, "AC_MCP_") || strings.HasPrefix(k, "AGENT_CENTER_") {
			t.Errorf("center credential %q must NOT reach executor env", k)
		}
		if strings.HasPrefix(k, "CLAUDE_CODE_") || k == "CLAUDE_CONFIG_DIR" {
			t.Errorf("parent runtime/config %q must NOT reach executor env", k)
		}
	}
	if _, ok := got["SOME_UNKNOWN_SECRET"]; ok {
		t.Error("unknown (non-allowlisted) var must be dropped by default-deny")
	}
}

func TestBuildExecutorEnv_OverlayAppliedButCenterCredsScrubbed(t *testing.T) {
	src := []string{"PATH=/usr/bin"}
	agentEnv := map[string]string{
		"GIT_AUTHOR_NAME":     "agent-center-dev1",
		"GIT_COMMITTER_NAME":  "agent-center-dev1",
		"MY_PROFILE_VAR":      "ok",
		"AC_MCP_WORKER_TOKEN": "leaked-via-profile", // must be scrubbed even from overlay
		"AGENT_CENTER_SECRET": "leaked",             // must be scrubbed even from overlay
	}
	got := envMap(BuildExecutorEnv(src, agentEnv))
	if got["GIT_AUTHOR_NAME"] != "agent-center-dev1" {
		t.Error("expected ② overlay git identity to be applied")
	}
	if got["MY_PROFILE_VAR"] != "ok" {
		t.Error("expected non-credential profile var to be applied")
	}
	if _, ok := got["AC_MCP_WORKER_TOKEN"]; ok {
		t.Error("AC_MCP_* in overlay must be scrubbed (executor hardening)")
	}
	if _, ok := got["AGENT_CENTER_SECRET"]; ok {
		t.Error("AGENT_CENTER_* in overlay must be scrubbed (executor hardening)")
	}
}

func TestBuildExecutorEnv_DeterministicOverlayOrderAndMalformed(t *testing.T) {
	// Malformed entries (no '=' or leading '=') are skipped, not panicked on.
	src := []string{"PATH=/usr/bin", "NOEQUALS", "=leadingequals"}
	agentEnv := map[string]string{"B_VAR": "2", "A_VAR": "1"}
	out := BuildExecutorEnv(src, agentEnv)
	// Overlay keys are appended in sorted order after system env.
	var overlay []string
	for _, kv := range out {
		if strings.HasPrefix(kv, "A_VAR=") || strings.HasPrefix(kv, "B_VAR=") {
			overlay = append(overlay, kv)
		}
	}
	if len(overlay) != 2 || overlay[0] != "A_VAR=1" || overlay[1] != "B_VAR=2" {
		t.Errorf("expected deterministic sorted overlay [A_VAR=1 B_VAR=2], got %v", overlay)
	}
	for _, kv := range out {
		if kv == "NOEQUALS" || strings.HasPrefix(kv, "=") {
			t.Errorf("malformed env entry %q should have been skipped", kv)
		}
	}
}
