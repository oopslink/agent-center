package agent

// Agent execution CLI allowlist (v2.7 #181 / FINDING-F).
//
// codex/opencode are auto-discovered on workers (#147) and shown in the
// Environment view (#176), but their agentadapter BuildCommand/ParseEvent are
// ErrNotImplemented stubs — the runtime dispatch only ever builds a
// claude-code (claudestream) command. So an agent bound to codex/opencode is
// created+displayed but never actually runs that CLI; it silently falls back
// to claude. To avoid that dishonest state, agent creation rejects any cli the
// runtime cannot execute. Per-CLI dispatch (v2.8 #180) will extend this
// allowlist as codex/opencode adapters become real.
//
// The identifier is the canonical agentadapter name ("claude-code", hyphen),
// the same value workers report as a capability — so a future agent.cli maps
// directly to a registered adapter.
var supportedExecutionCLIs = map[string]struct{}{
	"claude-code": {},
}

// IsSupportedExecutionCLI reports whether cli is an agent CLI the runtime can
// actually execute. Empty is NOT supported — agent creation must specify the
// cli explicitly (a stronger contract than defaulting; v2.8's multi-CLI world
// would otherwise have to answer "default to which?").
func IsSupportedExecutionCLI(cli string) bool {
	_, ok := supportedExecutionCLIs[cli]
	return ok
}
