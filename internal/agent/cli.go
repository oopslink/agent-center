package agent

// Agent execution CLI allowlist (#181 / FINDING-F).
//
// Agent creation rejects any cli the runtime cannot actually execute end-to-end
// (to avoid the dishonest state where an agent is created+displayed but silently
// falls back to claude). A cli is in this allowlist only once its full runtime
// path exists: a real agentadapter + a worker session starter the AgentController
// selects on agent.cli.
//
//   - claude-code: the claude supervisor session (claudestream).
//   - codex: the CodexSession (one-shot `codex exec --json` + resume), selected
//     by the AgentController when the reconcile payload carries cli=codex. The
//     codex adapter's BuildCommand/ParseEvent are real (validated on codex-cli
//     0.137.0). opencode remains a stub → still rejected.
//
// The identifier is the canonical agentadapter name (hyphenated "claude-code"),
// the same value workers report as a capability — so agent.cli maps directly to a
// registered adapter + the worker's per-cli session starter.
var supportedExecutionCLIs = map[string]struct{}{
	"claude-code": {},
	"codex":       {},
}

// IsSupportedExecutionCLI reports whether cli is an agent CLI the runtime can
// actually execute. Empty is NOT supported — agent creation must specify the
// cli explicitly (a stronger contract than defaulting; v2.8's multi-CLI world
// would otherwise have to answer "default to which?").
func IsSupportedExecutionCLI(cli string) bool {
	_, ok := supportedExecutionCLIs[cli]
	return ok
}
