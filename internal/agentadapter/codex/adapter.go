// Package codex is the Codex CLI adapter (OpenAI Codex CLI; per ADR-0030
// § 1).
//
// BuildCommand / ParseEvent are implemented against the real `codex exec
// --json` behaviour (validated on codex-cli 0.137.0). MCP / Skills / session
// continuation remain conservative-false until their runtime wiring exists
// (see IMPLEMENTATION_PLAN.md Stage 2/3): codex agents are still rejected at
// creation by the agent.cli allowlist, so this adapter is exercised by tests
// and probe/discovery, not yet by the runtime spawn path.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// AdapterName is the well-known name.
const AdapterName = "codex"

// Adapter is the Codex adapter. binary defaults to "codex" looked up via
// $PATH at exec time.
type Adapter struct {
	binary string
}

// New constructs the adapter; binary="" defaults to "codex".
func New(binary string) *Adapter {
	if binary == "" {
		binary = "codex"
	}
	return &Adapter{binary: binary}
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return AdapterName }

// SupportsSession reports session-id support. v2 default: false (most codex
// CLI variants do not support session continuation; revisit when probing).
func (a *Adapter) SupportsSession() bool { return false }

// BuildCommand assembles a FRESH `codex exec --json` invocation.
//
// Flags (validated on codex-cli 0.137.0):
//   - exec --json: non-interactive; emits JSONL events on stdout, then exits.
//   - --skip-git-repo-check: the agent workspace may not be a git repo.
//   - --dangerously-bypass-approvals-and-sandbox: the worker process IS the
//     isolation boundary (the same model as claude, which has no internal
//     sandbox); without it codex blocks on approval prompts / read-only and the
//     agent cannot act autonomously.
//   - -C <dir>: codex's working root (when WorkingDir is set).
//   - the prompt is the final positional argument.
//
// Session continuation: unlike claude (--session-id sets the id up front),
// codex MINTS its own thread_id (returned in the thread.started event) and
// resumes via `codex exec resume <thread_id>`. So req.ExecutionID is validated
// for parity but NOT passed as a set-id; capturing thread_id and issuing
// `exec resume` is the runtime resume slice (Stage 2/3), not the adapter.
//
// SystemPrompt: codex has no --append-system-prompt equivalent. As a best-effort
// it is prepended to the user prompt; the idiomatic persistent channel
// (AGENTS.md / -c) is left to the runtime wiring stage.
func (a *Adapter) BuildCommand(req agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	if req.ExecutionID == "" {
		return agentadapter.CmdSpec{}, errors.New("codex: execution_id required")
	}
	if req.Prompt == "" {
		return agentadapter.CmdSpec{}, errors.New("codex: prompt required")
	}
	args := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
	}
	if req.WorkingDir != "" {
		args = append(args, "-C", req.WorkingDir)
	}
	prompt := req.Prompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n" + req.Prompt
	}
	args = append(args, prompt)
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

// ParseEvent maps one `codex exec --json` JSONL line to AgentTraceEvent.
//
// codex event schema (validated on codex-cli 0.137.0):
//   - thread.started / turn.started → EventUnknown (no normalized slot; the
//     thread_id needed for resume is preserved in Raw)
//   - turn.completed → EventTurnEnd, with usage tokens attached (codex packs
//     usage into the turn boundary rather than a separate line as claude does)
//   - turn.failed / error → EventError
//   - item.started{command_execution} → EventToolCall (ToolName="shell",
//     ToolInput={"command": ...})
//   - item.completed{command_execution} → EventToolResult (ToolOutput =
//     aggregated_output as a JSON string)
//   - item.completed{reasoning} → EventThinking
//   - item.completed{agent_message} → EventUnknown (AgentTraceEvent has no
//     assistant-text slot — same gap as the claude adapter; Raw is preserved)
//
// Malformed JSON returns ErrInvalidEventJSON (caller increments parse-failure
// count via UnknownEventReporter.ReportParseFailure).
func (a *Adapter) ParseEvent(line []byte) (agentadapter.AgentTraceEvent, error) {
	if len(line) == 0 {
		return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: empty line", agentadapter.ErrInvalidEventJSON)
	}
	var top struct {
		Type  string          `json:"type"`
		Item  json.RawMessage `json:"item"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(line, &top); err != nil {
		return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: %v", agentadapter.ErrInvalidEventJSON, err)
	}
	ev := agentadapter.AgentTraceEvent{
		Raw:     append(json.RawMessage(nil), line...),
		CliType: top.Type,
	}
	switch top.Type {
	case "turn.completed":
		ev.Type = agentadapter.EventTurnEnd
		if top.Usage != nil {
			ev.TokensIn = top.Usage.InputTokens
			ev.TokensOut = top.Usage.OutputTokens
		}
	case "turn.failed", "error":
		ev.Type = agentadapter.EventError
	case "item.started", "item.completed":
		return parseItem(top.Type, top.Item, ev)
	default:
		ev.Type = agentadapter.EventUnknown
	}
	return ev, nil
}

// parseItem maps an item.started / item.completed event by its inner item type.
func parseItem(topType string, itemRaw json.RawMessage, ev agentadapter.AgentTraceEvent) (agentadapter.AgentTraceEvent, error) {
	if len(itemRaw) == 0 {
		ev.Type = agentadapter.EventUnknown
		return ev, nil
	}
	var item struct {
		Type             string `json:"type"`
		Text             string `json:"text"`
		Command          string `json:"command"`
		AggregatedOutput string `json:"aggregated_output"`
	}
	if err := json.Unmarshal(itemRaw, &item); err != nil {
		return agentadapter.AgentTraceEvent{}, fmt.Errorf("%w: item: %v", agentadapter.ErrInvalidEventJSON, err)
	}
	switch item.Type {
	case "command_execution":
		if topType == "item.completed" {
			ev.Type = agentadapter.EventToolResult
			// aggregated_output is a plain string → wrap as a JSON string so
			// ToolOutput is always valid JSON (mirrors the claude tool_result shape).
			out, _ := json.Marshal(item.AggregatedOutput)
			ev.ToolOutput = out
		} else { // item.started
			ev.Type = agentadapter.EventToolCall
			ev.ToolName = "shell"
			in, _ := json.Marshal(struct {
				Command string `json:"command"`
			}{Command: item.Command})
			ev.ToolInput = in
		}
	case "reasoning":
		ev.Type = agentadapter.EventThinking
		ev.Thinking = item.Text
	default:
		// agent_message and any other item type → Unknown (Raw preserved).
		ev.Type = agentadapter.EventUnknown
	}
	return ev, nil
}

// Probe checks whether the codex binary is on PATH and reports its version
// (per ADR-0030 § 2).
func (a *Adapter) Probe(ctx context.Context) (bool, string, error) {
	if _, err := exec.LookPath(a.binary); err != nil {
		return false, "", nil
	}
	cmd := exec.CommandContext(ctx, a.binary, "--version")
	out, err := cmd.Output()
	if err != nil {
		return false, "", nil
	}
	return true, strings.TrimSpace(string(out)), nil
}

// SupportedFeatures — v2 conservative defaults. Codex MCP support is
// uncertain; mark false until probed. Session continuation likewise false.
// Skills support is also unknown; default false. Revisit after CLI probe.
func (a *Adapter) SupportedFeatures() agentadapter.FeatureSet {
	return agentadapter.FeatureSet{
		SupportsMCP:     false,
		SupportsSkills:  false,
		SupportsSession: false,
	}
}

// BuildMCPConfigArg returns zero MCPSetup (codex MCP path unverified).
// DispatchService.FeatureCheck rejects MCP-bearing agents on codex workers
// per SupportedFeatures().SupportsMCP=false before this is called.
func (a *Adapter) BuildMCPConfigArg(_ string) (agentadapter.MCPSetup, error) {
	return agentadapter.MCPSetup{}, errors.New("codex: MCP injection not yet supported")
}

// BuildSkillMountSetup returns zero SkillMountSetup (codex skill path
// unverified). DispatchService rejects skill-bearing agents on codex
// workers per SupportedFeatures().SupportsSkills=false.
func (a *Adapter) BuildSkillMountSetup(_, _ string) (agentadapter.SkillMountSetup, error) {
	return agentadapter.SkillMountSetup{}, errors.New("codex: skill mount not yet supported")
}

// init self-registers the adapter on import.
func init() {
	agentadapter.Register(New(""))
}
