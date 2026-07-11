package agentruntime

// codex_mcp.go — T972: translate the canonical claude mcp_config.runtime.json into the
// codex config.toml [mcp_servers.<name>] stdio-launcher tables codex reads from
// $CODEX_HOME/config.toml. A cli=codex SUPERVISOR thus reaches the SAME agent-center MCP
// host binary + the SAME per-agent AC_MCP_* creds as the claude supervisor — it just
// consumes them via config.toml instead of claude's --mcp-config. (Unlike an EXECUTOR,
// a supervisor is SUPPOSED to carry center creds — it is the party that calls the MCP
// tools; the executor env allowlist that DENIES AC_MCP_* is a different, executor-only
// hardening.) config.toml wiring is the T972 hard point: config-gen correctness is
// unit-locked here; creds actually reaching the host + a real tool call are verified by
// the Accept dual-run (a silently-inert config is the CODEX_HOME / judge-P1 blind spot).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oopslink/agent-center/internal/mcphost"
)

// codexHomeDirName is the per-agent CODEX_HOME subdirectory under the agent home.
// codex reads $CODEX_HOME/config.toml on startup; giving each codex supervisor a
// DEDICATED CODEX_HOME (rather than the shared user ~/.codex) keeps its generated
// [mcp_servers.*] tables + creds isolated per agent and lets a self-heal relaunch
// regenerate them deterministically.
const codexHomeDirName = "codex-home"

// codexConfigFileName is the file codex loads from $CODEX_HOME.
const codexConfigFileName = "config.toml"

// codexAuthFileName is the codex login credential file under $CODEX_HOME.
const codexAuthFileName = "auth.json"

// resolveSourceCodexHome returns the worker's REAL CODEX_HOME (where `codex login` wrote
// auth.json): the CODEX_HOME env if set (read at the worker process, BEFORE the per-agent
// override is applied to the child), else ~/.codex.
func resolveSourceCodexHome() string {
	if h := strings.TrimSpace(os.Getenv("CODEX_HOME")); h != "" {
		return h
	}
	if hd, err := os.UserHomeDir(); err == nil {
		return filepath.Join(hd, ".codex")
	}
	return ""
}

// provisionCodexAuth links the source codex login's auth.json into the per-agent
// codex-home so a codex supervisor can AUTHENTICATE (T977 fix). Codex reads BOTH
// config.toml AND auth.json from $CODEX_HOME; the dedicated per-agent home has the
// generated config.toml but NOT the login auth (which lives in the worker's real
// CODEX_HOME / ~/.codex), so without this codex 401s and the entire MCP chain is
// unreachable. Returns a non-empty WARNING string when it cannot provision (source
// unresolved / auth.json missing / symlink error) so the caller logs FAIL-LOUD — never a
// silent 401, the same discipline as the executor's codexAuthPreflight. Re-links on every
// launch (removes any stale link/copy first) so a source token refresh propagates.
func provisionCodexAuth(codexHome, sourceCodexHome string) string {
	src := strings.TrimSpace(sourceCodexHome)
	if src == "" {
		return "source CODEX_HOME unresolved — cannot provision auth.json (run `codex login` + set CODEX_HOME)"
	}
	srcAuth := filepath.Join(src, codexAuthFileName)
	if _, err := os.Stat(srcAuth); err != nil {
		return fmt.Sprintf("source auth.json missing at %s (run `codex login`)", srcAuth)
	}
	dstAuth := filepath.Join(codexHome, codexAuthFileName)
	_ = os.Remove(dstAuth) // re-link each launch (handles a stale link/copy + token refresh)
	if err := os.Symlink(srcAuth, dstAuth); err != nil {
		return fmt.Sprintf("symlink auth.json into codex-home failed: %v", err)
	}
	return ""
}

// WriteCodexMCPConfig translates the canonical mcp_config.runtime.json into codex
// config.toml and writes it under a per-agent CODEX_HOME ("<home>/codex-home"),
// returning that CODEX_HOME directory (to export as $CODEX_HOME to the codex
// process). It is the codex counterpart to WriteMCPConfig: the same canonical
// runtime.json feeds BOTH the claude supervisor (via --mcp-config) and the codex
// supervisor (via $CODEX_HOME/config.toml), so a cli=codex supervisor reaches the
// SAME agent-center MCP host + per-agent creds. An empty runtimeJSON yields a
// header-only config.toml (no servers) rather than an error.
func WriteCodexMCPConfig(home string, runtimeJSON []byte) (string, error) {
	if home == "" {
		return "", errors.New("codex_session: home required to write codex mcp-config")
	}
	if len(runtimeJSON) == 0 {
		runtimeJSON = []byte("{}") // header-only config (no servers)
	}
	toml, err := codexMCPConfigTOML(runtimeJSON)
	if err != nil {
		return "", err
	}
	codexHome := filepath.Join(home, codexHomeDirName)
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return "", fmt.Errorf("codex_session: mkdir codex-home: %w", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, codexConfigFileName), toml, 0o600); err != nil {
		return "", fmt.Errorf("codex_session: write codex config.toml: %w", err)
	}
	return codexHome, nil
}

// codexMCPConfigTOML translates a canonical mcp_config.runtime.json document
// ({"mcpServers":{<name>:{command,args,env}}}) into codex config.toml content with one
// [mcp_servers.<name>] table per server. Output is DETERMINISTIC (server names + env
// keys sorted) so the generated config is reproducible and testable. An empty/absent
// mcpServers yields a header-only doc (no servers) rather than an error, so a
// no-MCP agent simply gets no [mcp_servers.*] tables.
func codexMCPConfigTOML(runtimeJSON []byte) ([]byte, error) {
	var cfg mcphost.MCPConfig
	if err := json.Unmarshal(runtimeJSON, &cfg); err != nil {
		return nil, fmt.Errorf("codex mcp: parse runtime.json: %w", err)
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Generated by agent-center (T972): codex MCP servers, translated from\n")
	b.WriteString("# the canonical mcp_config.runtime.json. Do not edit by hand.\n")
	for _, name := range names {
		s := cfg.MCPServers[name]
		fmt.Fprintf(&b, "\n[mcp_servers.%s]\n", tomlKey(name))
		fmt.Fprintf(&b, "command = %s\n", tomlString(s.Command))
		b.WriteString("args = [")
		for i, a := range s.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(tomlString(a))
		}
		b.WriteString("]\n")
		if len(s.Env) > 0 {
			keys := make([]string, 0, len(s.Env))
			for k := range s.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString("env = { ")
			for i, k := range keys {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s = %s", tomlKey(k), tomlString(s.Env[k]))
			}
			b.WriteString(" }\n")
		}
	}
	return []byte(b.String()), nil
}

// tomlString renders a Go string as a TOML basic string (double-quoted, escaped).
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlKey renders a TOML key: a bare key when it matches [A-Za-z0-9_-]+ (the common
// case for server names + AC_MCP_* env keys), else a quoted key.
func tomlKey(k string) string {
	if k == "" {
		return `""`
	}
	for _, r := range k {
		bare := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !bare {
			return tomlString(k)
		}
	}
	return k
}
