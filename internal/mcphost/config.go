// config.go (v2.7 b3-ii, ADR-0049) — the pure `--mcp-config` generation
// helper. claude's `--mcp-config` expects a JSON object with a top-level
// `mcpServers` map; each entry is launched as `command` + `args` with the
// given `env`. This mirrors the canonical stdio MCP server config shape (the
// same {mcpServers:{<name>:{command,args,env}}} shape claude consumes, and
// the generic JSON the worker daemon's MCPInjector walks in
// home_dir/mcp_config.json).
//
// This is a PURE function: it takes the launch command + args + binding
// params and returns the bytes. It does NO I/O and hard-codes NO binary path
// (the caller / D2-c supplies command + args). D2-c writes the bytes to disk
// and passes --mcp-config to claude.
//
// The env keys are the exact ones runMCPHost (handlers_mcphost.go) reads:
//
//	AC_MCP_AGENT_ID           operating agent id (process-fixed)
//	AC_MCP_ADMIN_URL          admin endpoint (unix:/path or tcp://host:port)
//	AC_MCP_WORKER_TOKEN       worker bearer token (owner worker:<id>)
//	AC_MCP_SERVER_FINGERPRINT pinned cert fingerprint (required for tcp://)
//	AC_MCP_AGENT_ROOT         agent workspace root (file-tool containment)
package mcphost

import "encoding/json"

// MCPServerSpec is one entry in the `mcpServers` map: a stdio MCP server
// launched as command+args with the given environment.
type MCPServerSpec struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// MCPConfig is the top-level `--mcp-config` document claude consumes.
type MCPConfig struct {
	MCPServers map[string]MCPServerSpec `json:"mcpServers"`
}

// MCPConfigParams are the inputs to GenerateMCPConfig. Command + Args are the
// launch vector for `worker mcp-host` (supplied by the caller — never
// hard-coded here). The remaining fields become the per-server env.
type MCPConfigParams struct {
	ServerName        string
	Command           string
	Args              []string
	AgentID           string
	AdminURL          string
	WorkerToken       string
	ServerFingerprint string
	AgentRoot         string
}

// BuildMCPConfig builds the typed --mcp-config document for a single
// `worker mcp-host` server bound to one agent. Pure; no I/O.
func BuildMCPConfig(p MCPConfigParams) MCPConfig {
	env := map[string]string{
		"AC_MCP_AGENT_ID":           p.AgentID,
		"AC_MCP_ADMIN_URL":          p.AdminURL,
		"AC_MCP_WORKER_TOKEN":       p.WorkerToken,
		"AC_MCP_SERVER_FINGERPRINT": p.ServerFingerprint,
		"AC_MCP_AGENT_ROOT":         p.AgentRoot,
	}
	return MCPConfig{
		MCPServers: map[string]MCPServerSpec{
			p.ServerName: {
				Command: p.Command,
				Args:    p.Args,
				Env:     env,
			},
		},
	}
}

// GenerateMCPConfig builds and marshals the --mcp-config document. The bytes
// are the file content D2-c writes to disk and hands to claude via
// --mcp-config.
func GenerateMCPConfig(p MCPConfigParams) ([]byte, error) {
	return json.MarshalIndent(BuildMCPConfig(p), "", "  ")
}
