// Package claudecode implements the Claude Code agent CLI adapter (05-
// agent-adapters § 8.1).
package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// AdapterName is the well-known name for the claude-code adapter.
const AdapterName = "claude-code"

// Adapter implements agentadapter.Adapter for `claude` CLI.
type Adapter struct {
	binary string
}

// New constructs the adapter; binary defaults to "claude" looked up via
// $PATH at exec time.
func New(binary string) *Adapter {
	if binary == "" {
		binary = "claude"
	}
	return &Adapter{binary: binary}
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return AdapterName }

// SupportsSession reports session-id support.
func (a *Adapter) SupportsSession() bool { return true }

// BuildCommand assembles the `claude` invocation (05-agent-adapters § 8.1).
func (a *Adapter) BuildCommand(req agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	if req.ExecutionID == "" {
		return agentadapter.CmdSpec{}, errors.New("claudecode: execution_id required")
	}
	if req.Prompt == "" {
		return agentadapter.CmdSpec{}, errors.New("claudecode: prompt required")
	}
	args := []string{
		"--output-format", "stream-json",
		"--session-id", req.ExecutionID,
	}
	for _, sf := range req.SkillFiles {
		args = append(args, "--skill", sf)
	}
	args = append(args, "-p", req.Prompt)
	env := os.Environ()
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}
	return agentadapter.CmdSpec{
		Binary: a.binary,
		Args:   args,
		Env:    env,
		Stdin:  nil,
	}, nil
}

// ParseEvent maps one stream-json line to AgentTraceEvent.
//
// Known kinds (05-agent-adapters § 8.1 table):
//   - "thinking"     → EventThinking, Thinking=text
//   - "tool_use"     → EventToolCall, ToolName=name, ToolInput=input
//   - "tool_result"  → EventToolResult, ToolOutput=content
//   - "usage"        → EventTokensReport
//   - "end_turn"     → EventTurnEnd
//
// Other kinds map to EventUnknown with the original cli_type preserved.
// Malformed JSON returns ErrInvalidEventJSON (caller increments parse-
// failure count via UnknownEventReporter.ReportParseFailure).
func (a *Adapter) ParseEvent(line []byte) (agentadapter.AgentTraceEvent, error) {
	if len(line) == 0 {
		return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: empty line", agentadapter.ErrInvalidEventJSON)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: %v", agentadapter.ErrInvalidEventJSON, err)
	}
	var cliType string
	if rt, ok := raw["type"]; ok {
		_ = json.Unmarshal(rt, &cliType)
	}
	ev := agentadapter.AgentTraceEvent{
		Raw:     append(json.RawMessage(nil), line...),
		CliType: cliType,
	}
	switch cliType {
	case "thinking":
		var s struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(line, &s); err != nil {
			return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: thinking: %v", agentadapter.ErrInvalidEventJSON, err)
		}
		ev.Type = agentadapter.EventThinking
		ev.Thinking = s.Text
	case "tool_use":
		var s struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(line, &s); err != nil {
			return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: tool_use: %v", agentadapter.ErrInvalidEventJSON, err)
		}
		ev.Type = agentadapter.EventToolCall
		ev.ToolName = s.Name
		ev.ToolInput = s.Input
		// Malformed event (missing name) → degrade to unknown not fail.
		if ev.ToolName == "" {
			ev.Type = agentadapter.EventUnknown
		}
	case "tool_result":
		var s struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(line, &s); err != nil {
			return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: tool_result: %v", agentadapter.ErrInvalidEventJSON, err)
		}
		ev.Type = agentadapter.EventToolResult
		ev.ToolOutput = s.Content
	case "usage":
		var s struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}
		if err := json.Unmarshal(line, &s); err != nil {
			return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: usage: %v", agentadapter.ErrInvalidEventJSON, err)
		}
		ev.Type = agentadapter.EventTokensReport
		ev.TokensIn = s.InputTokens
		ev.TokensOut = s.OutputTokens
	case "end_turn":
		ev.Type = agentadapter.EventTurnEnd
	case "error":
		ev.Type = agentadapter.EventError
	default:
		ev.Type = agentadapter.EventUnknown
	}
	return ev, nil
}

// Probe checks whether the claude binary is on PATH and reports its version
// (per ADR-0030 § 2). Uses `claude --version` with the supplied ctx for
// timeout.
func (a *Adapter) Probe(ctx context.Context) (bool, string, error) {
	if _, err := exec.LookPath(a.binary); err != nil {
		return false, "", nil
	}
	cmd := exec.CommandContext(ctx, a.binary, "--version")
	out, err := cmd.Output()
	if err != nil {
		// Binary exists but --version failed — treat as not-available for safety.
		return false, "", nil
	}
	return true, strings.TrimSpace(string(out)), nil
}

// SupportedFeatures returns the v2 feature flags for claude-code. All three
// are supported (per ADR-0030 § 2 example).
func (a *Adapter) SupportedFeatures() agentadapter.FeatureSet {
	return agentadapter.FeatureSet{
		SupportsMCP:     true,
		SupportsSkills:  true,
		SupportsSession: true,
	}
}

// BuildMCPConfigArg returns `--mcp-config <path>` per claude CLI.
func (a *Adapter) BuildMCPConfigArg(runtimeJSONPath string) (agentadapter.MCPSetup, error) {
	if runtimeJSONPath == "" {
		return agentadapter.MCPSetup{}, errors.New("claudecode: runtimeJSONPath required")
	}
	return agentadapter.MCPSetup{
		Args: []string{"--mcp-config", runtimeJSONPath},
	}, nil
}

// BuildSkillMountSetup uses claude's native `--skill-path` flag.
//
// homeDirSkills is the absolute path to home_dir/skills/. execDir is the
// per-execution worktree path (unused for claude — CLIArg mode does not
// need to relocate skills).
func (a *Adapter) BuildSkillMountSetup(homeDirSkills, execDir string) (agentadapter.SkillMountSetup, error) {
	if homeDirSkills == "" {
		return agentadapter.SkillMountSetup{}, errors.New("claudecode: homeDirSkills required")
	}
	// Sanity: caller passes absolute / well-formed path.
	homeDirSkills = filepath.Clean(homeDirSkills)
	return agentadapter.SkillMountSetup{
		Mode: agentadapter.SkillMountCLIArg,
		Args: []string{"--skill-path", homeDirSkills},
	}, nil
}

// init self-registers the adapter on import (worker daemon imports this
// package).
func init() {
	agentadapter.Register(New(""))
}
