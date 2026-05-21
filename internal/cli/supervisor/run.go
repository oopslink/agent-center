package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/agentadapter/claudecode"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

// RunInput captures the supervisor-subprocess CLI flag inputs.
type RunInput struct {
	Scope        string
	InvocationID string
	TriggerCSV   string
	MemoryDir    string
	UsageDir     string
	// EventLookup is an optional override (tests pass a fake). Production
	// path uses RunInput.EventLookup=nil; we read trigger events by id
	// from the EventRepository attached to the App.
	EventLookup func(ctx context.Context, id observability.EventID) (*observability.Event, error)
	// ClaudeBinary overrides the claude binary path; default "claude".
	ClaudeBinary string
	// ClaudeExtraArgs are extra args appended after BuildCommand args
	// (test injection — points at fake_claude.sh).
	ClaudeExtraArgs []string
}

// RunResult captures the outcome to report back to the caller (Spawner
// reads usage file + exit code; this is informational for the CLI tests).
type RunResult struct {
	ExitCode  int
	Token     cognition.TokenUsage
	Decisions int
}

// Run drives one supervisor invocation inside the subprocess:
//
//  1. Parse scope + trigger event ids
//  2. PromptAssembler builds prompt + work dir
//  3. claudecode.Adapter.BuildCommand → exec.Command → stream-json pipe
//  4. Parse JSONL: accumulate usage + decisions count + write trace
//  5. Write per-invocation usage JSON file (Spawner reads on exit)
//  6. Return exit code (= claude exit code, transparent)
func Run(ctx context.Context, in RunInput, stdout, stderr io.Writer) (RunResult, error) {
	if in.InvocationID == "" {
		return RunResult{}, errors.New("supervisor: --invocation-id required")
	}
	if in.Scope == "" {
		return RunResult{}, errors.New("supervisor: --scope required")
	}
	scope, err := parseScopeFlag(in.Scope)
	if err != nil {
		return RunResult{}, err
	}
	triggers, err := parseTriggers(in.TriggerCSV)
	if err != nil {
		return RunResult{}, err
	}
	memoryDir := in.MemoryDir
	if memoryDir == "" {
		memoryDir = os.Getenv("AGENT_CENTER_MEMORY_DIR")
	}
	if memoryDir == "" {
		return RunResult{}, errors.New("supervisor: AGENT_CENTER_MEMORY_DIR or --memory-dir required")
	}
	usageDir := in.UsageDir
	if usageDir == "" {
		usageDir = os.Getenv("AGENT_CENTER_USAGE_DIR")
	}
	// resolve trigger events
	events := make([]*observability.Event, 0, len(triggers))
	if in.EventLookup != nil {
		for _, id := range triggers {
			e, err := in.EventLookup(ctx, id)
			if err == nil && e != nil {
				events = append(events, e)
			}
		}
	}
	prompt, err := Assemble(AssembleInput{
		Scope:         scope,
		TriggerEvents: events,
		MemoryDir:     memoryDir,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("supervisor: assemble prompt: %w", err)
	}
	// materialise skill file in a per-invocation temp dir
	skillDir := filepath.Join(os.TempDir(), "agent-center-skills", in.InvocationID)
	skillRef, err := WriteSkillTo(skillDir)
	if err != nil {
		return RunResult{}, fmt.Errorf("supervisor: write skill: %w", err)
	}
	defer os.RemoveAll(skillDir)

	binary := in.ClaudeBinary
	if binary == "" {
		binary = "claude"
	}
	adapter := claudecode.New(binary)
	spec, err := adapter.BuildCommand(agentadapter.SpawnRequest{
		ExecutionID: in.InvocationID,
		Prompt:      prompt.Prompt,
		WorkingDir:  prompt.WorkDir,
		SkillFiles:  []string{skillRef},
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("supervisor: build command: %w", err)
	}
	args := append([]string{}, spec.Args...)
	args = append(args, in.ClaudeExtraArgs...)
	cmd := exec.CommandContext(ctx, spec.Binary, args...)
	cmd.Env = spec.Env
	cmd.Dir = prompt.WorkDir
	cmd.Stderr = stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("supervisor: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("supervisor: start claude: %w", err)
	}
	usage, decisions := parseClaudeStream(adapter, pipe, stdout)
	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	if usageDir != "" {
		_ = writeUsage(usageDir, in.InvocationID, usage)
	}
	return RunResult{ExitCode: exitCode, Token: usage, Decisions: decisions}, nil
}

func parseScopeFlag(s string) (cognition.InvocationScope, error) {
	if s == "" {
		return cognition.InvocationScope{}, errors.New("supervisor: scope empty")
	}
	if s == "global" || s == "global:" || s == "global:_global_" {
		return cognition.NewInvocationScope(cognition.ScopeGlobal, "")
	}
	kind, key, ok := strings.Cut(s, ":")
	if !ok {
		return cognition.InvocationScope{}, fmt.Errorf("supervisor: scope %q must be kind:key", s)
	}
	return cognition.NewInvocationScope(cognition.ScopeKind(kind), key)
}

func parseTriggers(csv string) ([]observability.EventID, error) {
	if csv == "" {
		return nil, errors.New("supervisor: --trigger-events required")
	}
	parts := strings.Split(csv, ",")
	out := make([]observability.EventID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, observability.EventID(p))
	}
	if len(out) == 0 {
		return nil, errors.New("supervisor: --trigger-events parsed to empty")
	}
	return out, nil
}

func parseClaudeStream(adapter *claudecode.Adapter, r io.Reader, traceOut io.Writer) (cognition.TokenUsage, int) {
	var usage cognition.TokenUsage
	decisions := 0
	dec := bufioScanner(r)
	for {
		line, ok := dec()
		if !ok {
			break
		}
		// echo to traceOut for downstream observation (peek-trace).
		if traceOut != nil {
			_, _ = traceOut.Write(line)
			_, _ = traceOut.Write([]byte("\n"))
		}
		ev, err := adapter.ParseEvent(line)
		if err != nil {
			continue
		}
		switch ev.Type {
		case agentadapter.EventTokensReport:
			usage.Input += ev.TokensIn
			usage.Output += ev.TokensOut
		case agentadapter.EventToolCall:
			// count actions invoked through `Bash` calls to agent-center
			if ev.ToolName == "Bash" {
				decisions++
			}
		}
	}
	return usage, decisions
}

// bufioScanner returns a closure that yields the next \n-terminated line
// from r, sans newline; returns ok=false on EOF.
func bufioScanner(r io.Reader) func() ([]byte, bool) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	pos := 0
	eof := false
	return func() ([]byte, bool) {
		for {
			// find newline in buf[pos:]
			for i := pos; i < len(buf); i++ {
				if buf[i] == '\n' {
					line := buf[pos:i]
					pos = i + 1
					return line, true
				}
			}
			if eof {
				if pos < len(buf) {
					line := buf[pos:]
					pos = len(buf)
					return line, true
				}
				return nil, false
			}
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				eof = true
			}
		}
	}
}

func writeUsage(dir, invID string, u cognition.TokenUsage) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(u)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, invID+".usage.json.tmp")
	final := filepath.Join(dir, invID+".usage.json")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// ContextTimeout returns the hard deadline derived from the scope.
func ContextTimeout(scope cognition.InvocationScope) time.Duration {
	return cognition.HardTimeoutFor(scope.Kind()).Duration()
}
