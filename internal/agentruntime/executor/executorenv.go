package executor

// executorenv.go — F1 (process model) executor launch environment.
//
// An executor is a PURE-COMPUTE worker: it never connects to the center, holds
// no credentials, and reaches no mcp server (design §4 / §6). The single most
// important security property of the executor process model is therefore its
// ENVIRONMENT: the forked process must not be able to read worker-daemon
// secrets, the mcp worker token, or the admin endpoint out of its env.
//
// We enforce this with the SAME default-DENY allowlist the supervisor uses for
// claude (internal/agentsupervisor/claudeenv.go), re-implemented here rather than
// imported so the executor package stays dependency-free (the worker daemon
// imports THIS package to wire spawning; agentsupervisor imports the daemon, so
// importing agentsupervisor here would risk an import cycle once wired). The
// allowlists are intentionally kept byte-identical in intent; see that file for
// the rationale behind each entry. On top of the allowlist the executor adds one
// extra hardening pass over the ② per-agent overlay (BuildExecutorEnv): any
// center-credential-shaped key is dropped even if a misconfigured profile set it,
// because an executor must NEVER carry one.

import (
	"sort"
	"strings"
)

// envAllowExact mirrors agentsupervisor.envAllowExact: the minimal system vars a
// CLI child needs to run, plus HTTP-proxy routing config (required to reach the
// Anthropic API in proxied deployments — not a secret). Worker secrets live under
// AGENT_CENTER_* / AC_MCP_* and are absent here, so they are dropped by default.
var envAllowExact = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
	"LANG": true, "TZ": true, "TMPDIR": true, "SHELL": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true, "all_proxy": true,
}

// envAllowPrefix mirrors agentsupervisor.envAllowPrefix: locale + the executor
// CLIs' OWN auth/config namespaces. The executor still runs an LLM (its own
// keychain /login or ANTHROPIC_* auth), so these are required and are NOT center
// credentials. CODEX_* (incl. CODEX_HOME → $CODEX_HOME/auth.json login state) is
// codex's own auth/config namespace, the codex analogue of CLAUDE_* — a cli=codex
// executor (v2.18.1 BE-2) needs it inherited to authenticate, and like the claude
// namespaces it carries NO center credential (those are AGENT_CENTER_* / AC_MCP_*).
var envAllowPrefix = []string{"LC_", "ANTHROPIC_", "CLAUDE_", "CODEX_"}

// envAllowed reports whether an inherited system env var name is allowlisted.
// The two CLAUDE_* carve-outs match agentsupervisor: CLAUDE_CONFIG_DIR is dropped
// (relocating it breaks keychain /login) and the whole CLAUDE_CODE_* runtime
// namespace is dropped (parent SDK markers + inherited OAuth token).
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

// centerCredentialKey reports whether an env var name is a center-facing
// credential / endpoint the executor must never carry. These are the keys the
// mcp-host reads (mcphost.config: AC_MCP_*) plus the worker-daemon namespace
// (AGENT_CENTER_*). The allowlist already excludes them from inherited SYSTEM
// env; this second guard ALSO scrubs them from the ② per-agent overlay so a
// misconfigured profile can never smuggle one into an executor (defense-in-depth,
// design §4 "executor 不持凭据/不碰 mcp").
func centerCredentialKey(name string) bool {
	return strings.HasPrefix(name, "AC_MCP_") || strings.HasPrefix(name, "AGENT_CENTER_")
}

// BuildExecutorEnv builds the executor child's environment (design §4 / §6.A):
//
//	layer 1 — allowlisted system env (default-deny; no worker/mcp secrets, no
//	          admin endpoint). HOME / CLAUDE_CONFIG_DIR left at defaults so the
//	          executor's own keychain /login auth stays intact.
//	layer 2 — the ② per-agent overlay (agentEnv: git identity, Profile.EnvVars),
//	          applied AS-IS EXCEPT any center-credential-shaped key, which is
//	          dropped (centerCredentialKey) so it can never reach an executor.
//
// The result is deterministically ordered. Unlike the supervisor's BuildClaudeEnv
// the executor is NEVER handed an --mcp-config (the caller, Spawner, omits it), so
// even the file-path-to-token indirection the supervisor uses is absent here.
func BuildExecutorEnv(source []string, agentEnv map[string]string) []string {
	out := make([]string, 0, len(source)+len(agentEnv))
	for _, kv := range source {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		if envAllowed(kv[:i]) {
			out = append(out, kv)
		}
	}
	keys := make([]string, 0, len(agentEnv))
	for k := range agentEnv {
		if centerCredentialKey(k) {
			continue // executor hardening: never overlay a center credential
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+agentEnv[k])
	}
	return out
}
