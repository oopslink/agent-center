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

var codexInheritedResourceDirs = []string{".tmp", "plugins", "computer-use"}

var (
	codexNodeReplCommand    = "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"
	codexNodeCommand        = "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node"
	codexNodeModuleDirs     = "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules"
	codexComputerUseService = "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/@oai/sky/Codex Computer Use.app/Contents/MacOS/SkyComputerUseService"
)

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

func provisionCodexResourceLinks(codexHome, sourceCodexHome string) []string {
	srcRoot := strings.TrimSpace(sourceCodexHome)
	if srcRoot == "" {
		return []string{"source CODEX_HOME unresolved — cannot inherit Codex plugin resources"}
	}
	var warnings []string
	for _, name := range codexInheritedResourceDirs {
		src := filepath.Join(srcRoot, name)
		if _, err := os.Stat(src); err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("%s unavailable at source: %v", name, err))
			}
			continue
		}
		dst := filepath.Join(codexHome, name)
		if target, err := os.Readlink(dst); err == nil {
			if target == src {
				continue
			}
			if err := os.Remove(dst); err != nil {
				warnings = append(warnings, fmt.Sprintf("replace stale %s symlink: %v", name, err))
				continue
			}
		} else if !os.IsNotExist(err) {
			if err := os.RemoveAll(dst); err != nil {
				warnings = append(warnings, fmt.Sprintf("replace stale %s directory: %v", name, err))
				continue
			}
		}
		if err := os.Symlink(src, dst); err != nil {
			warnings = append(warnings, fmt.Sprintf("symlink %s into codex-home failed: %v", name, err))
		}
	}
	return warnings
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
	return writeCodexMCPConfig(home, runtimeJSON, nil, "")
}

func WriteCodexMCPConfigFromSource(home string, runtimeJSON []byte, sourceCodexHome string) (string, error) {
	var base []byte
	src := strings.TrimSpace(sourceCodexHome)
	if src != "" {
		b, err := os.ReadFile(filepath.Join(src, codexConfigFileName))
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("codex_session: read source config.toml: %w", err)
		}
		base = stripCodexMCPConfigTables(b)
	}
	return writeCodexMCPConfig(home, runtimeJSON, base, src)
}

func writeCodexMCPConfig(home string, runtimeJSON, baseConfig []byte, sourceCodexHome string) (string, error) {
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
	content := mergeCodexBaseAndGeneratedConfig(baseConfig, toml)
	if nodeRepl := codexNodeReplMCPConfigTOML(codexHome, sourceCodexHome); len(nodeRepl) > 0 {
		content = mergeCodexBaseAndGeneratedConfig(content, nodeRepl)
	}
	if err := os.WriteFile(filepath.Join(codexHome, codexConfigFileName), content, 0o600); err != nil {
		return "", fmt.Errorf("codex_session: write codex config.toml: %w", err)
	}
	return codexHome, nil
}

func codexComputerUseAvailable(sourceCodexHome string) bool {
	if strings.TrimSpace(sourceCodexHome) == "" {
		return false
	}
	if !regularFileExists(codexNodeReplCommand) || !regularFileExists(codexNodeCommand) || !codexDirExists(codexNodeModuleDirs) {
		return false
	}
	return strings.TrimSpace(codexComputerUseServicePath(sourceCodexHome)) != ""
}

func codexComputerUseServicePath(sourceCodexHome string) string {
	src := strings.TrimSpace(sourceCodexHome)
	if src != "" {
		p := filepath.Join(src, "computer-use", "Codex Computer Use.app", "Contents", "MacOS", "SkyComputerUseService")
		if regularFileExists(p) {
			return p
		}
	}
	if regularFileExists(codexComputerUseService) {
		return codexComputerUseService
	}
	return ""
}

func codexNodeReplMCPConfigTOML(codexHome, sourceCodexHome string) []byte {
	servicePath := codexComputerUseServicePath(sourceCodexHome)
	if strings.TrimSpace(codexHome) == "" || strings.TrimSpace(sourceCodexHome) == "" || servicePath == "" || !codexComputerUseAvailable(sourceCodexHome) {
		return nil
	}
	trustedPaths := []string{codexHome, codexNodeModuleDirs}
	if src := strings.TrimSpace(sourceCodexHome); src != "" {
		trustedPaths = append([]string{src}, trustedPaths...)
	}
	env := map[string]string{
		"BROWSER_USE_AVAILABLE_BACKENDS":               "chrome,iab,computer-use",
		"CODEX_HOME":                                   codexHome,
		"NODE_REPL_INSTRUCTIONS_USE_CASE_COMPUTER_USE": "1",
		"NODE_REPL_NATIVE_PIPE_CONNECT_TIMEOUT_MS":     "120000",
		"NODE_REPL_NODE_MODULE_DIRS":                   codexNodeModuleDirs,
		"NODE_REPL_NODE_PATH":                          codexNodeCommand,
		"NODE_REPL_TRUSTED_CODE_PATHS":                 strings.Join(trustedPaths, ":"),
		"SKY_CUA_SERVICE_PATH":                         servicePath,
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Generated by agent-center: Codex Computer Use support via node_repl.\n")
	b.WriteString("\n[mcp_servers.node_repl]\n")
	fmt.Fprintf(&b, "command = %s\n", tomlString(codexNodeReplCommand))
	b.WriteString("args = []\n")
	b.WriteString("startup_timeout_sec = 120\n")
	b.WriteString("env = { ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %s", tomlKey(k), tomlString(env[k]))
	}
	b.WriteString(" }\n")
	return []byte(b.String())
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func codexDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mergeCodexBaseAndGeneratedConfig(baseConfig, generated []byte) []byte {
	base := bytesTrimSpace(baseConfig)
	if len(base) == 0 {
		return generated
	}
	out := make([]byte, 0, len(base)+len(generated)+2)
	out = append(out, base...)
	out = append(out, '\n', '\n')
	out = append(out, generated...)
	return out
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func stripCodexMCPConfigTables(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(src), "\n")
	var out strings.Builder
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if table, ok := tomlTableName(trimmed); ok {
			skip = table == "mcp_servers" || strings.HasPrefix(table, "mcp_servers.")
		}
		if skip {
			continue
		}
		out.WriteString(line)
	}
	return []byte(out.String())
}

func tomlTableName(line string) (string, bool) {
	if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]")), true
	}
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")), true
	}
	return "", false
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
