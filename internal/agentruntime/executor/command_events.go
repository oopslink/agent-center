package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

const (
	commandEventStarted  = "command_started"
	commandEventFinished = "command_finished"

	commandEventSourceClaude = "claude_stream_json"
	commandEventSourceCodex  = "codex_jsonl"
)

// CommandExecutionEvent is the append-only evidence source for shell commands the
// executor's runner actually invoked. It is derived from the runner's structured
// stream, not from the runner argv that launched the model.
type CommandExecutionEvent struct {
	At                  time.Time `json:"at"`
	Type                string    `json:"type"`
	Source              string    `json:"source"`
	ToolUseID           string    `json:"tool_use_id"`
	ToolName            string    `json:"tool_name"`
	Command             string    `json:"command,omitempty"`
	ExitStatus          *int      `json:"exit_status,omitempty"`
	ExitStatusAvailable bool      `json:"exit_status_available,omitempty"`
}

func (e CommandExecutionEvent) Validate() error {
	if e.At.IsZero() {
		return errors.New("executor: command event at required")
	}
	switch strings.TrimSpace(e.Type) {
	case commandEventStarted:
		if strings.TrimSpace(e.Command) == "" {
			return errors.New("executor: command_started requires command")
		}
	case commandEventFinished:
		if e.ExitStatusAvailable && e.ExitStatus == nil {
			return errors.New("executor: command_finished exit_status_available requires exit_status")
		}
	default:
		return fmt.Errorf("executor: unknown command event type %q", e.Type)
	}
	if strings.TrimSpace(e.Source) == "" {
		return errors.New("executor: command event source required")
	}
	if strings.TrimSpace(e.ToolUseID) == "" {
		return errors.New("executor: command event tool_use_id required")
	}
	if strings.TrimSpace(e.ToolName) == "" {
		return errors.New("executor: command event tool_name required")
	}
	return nil
}

// AppendCommandEvent appends one structured shell-command event to command_events.jsonl.
// It is best-effort at the call site, but when it does write, the line is validated and
// append-only so evidence_only finalization can audit what the runner stream exposed.
func (fx *FileExchange) AppendCommandEvent(executorID string, e CommandExecutionEvent) error {
	if e.At.IsZero() {
		e.At = fx.clk.Now().UTC()
	}
	if err := e.Validate(); err != nil {
		return err
	}
	path, err := fx.layout.CommandEventsPath(executorID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("executor: mkdir for command events %q: %w", path, err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("executor: marshal command event: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("executor: open command events %q: %w", path, err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("executor: append command events %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("executor: close command events %q: %w", path, err)
	}
	return nil
}

// ReadCommandEvents reads command_events.jsonl and returns both the parsed events and
// the exact bytes used for the artifact digest.
func (fx *FileExchange) ReadCommandEvents(executorID string) ([]CommandExecutionEvent, []byte, error) {
	path, err := fx.layout.CommandEventsPath(executorID)
	if err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, mapReadErr(commandEventsFileName, executorID, err)
	}
	var entries []CommandExecutionEvent
	for i, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e CommandExecutionEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, b, fmt.Errorf("executor: %s for %s line %d corrupt: %w", commandEventsFileName, executorID, i+1, err)
		}
		if err := e.Validate(); err != nil {
			return nil, b, fmt.Errorf("executor: %s for %s line %d invalid: %w", commandEventsFileName, executorID, i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, b, nil
}

type commandStreamRecorder struct {
	codex bool
	emit  func(CommandExecutionEvent)
	// Claude tool_result blocks identify only their tool_use_id, not the tool name.
	// Remember shell starts so Read/Edit/etc results cannot poison command evidence.
	shellToolUseIDs map[string]struct{}
}

func newCommandStreamRecorder(codex bool, emit func(CommandExecutionEvent)) *commandStreamRecorder {
	return &commandStreamRecorder{codex: codex, emit: emit, shellToolUseIDs: make(map[string]struct{})}
}

func (r *commandStreamRecorder) ObserveLine(line string) {
	if r == nil || r.emit == nil {
		return
	}
	var events []CommandExecutionEvent
	if r.codex {
		events = extractCodexCommandEvents([]byte(line))
	} else {
		events = extractClaudeCommandEvents([]byte(line))
	}
	for _, ev := range events {
		if !r.codex {
			switch ev.Type {
			case commandEventStarted:
				r.shellToolUseIDs[ev.ToolUseID] = struct{}{}
			case commandEventFinished:
				if _, ok := r.shellToolUseIDs[ev.ToolUseID]; !ok {
					continue
				}
				delete(r.shellToolUseIDs, ev.ToolUseID)
			}
		}
		r.emit(ev)
	}
}

func extractClaudeCommandEvents(line []byte) []CommandExecutionEvent {
	evs, err := claudestream.ParseStreamLine(line)
	if err != nil {
		return nil
	}
	out := make([]CommandExecutionEvent, 0, len(evs))
	for _, ev := range evs {
		switch ev.Type {
		case "tool_use":
			if !isShellToolName(ev.ToolName) {
				continue
			}
			cmd := shellCommandFromToolInput(ev.ToolInput)
			if cmd == "" {
				continue
			}
			out = append(out, CommandExecutionEvent{
				Type:      commandEventStarted,
				Source:    commandEventSourceClaude,
				ToolUseID: ev.ToolUseID,
				ToolName:  ev.ToolName,
				Command:   cmd,
			})
		case "tool_result":
			blockRaw := ev.Raw
			if fullBlock, ok := claudeToolResultBlock(line, ev.ToolUseID); ok {
				blockRaw = fullBlock
			}
			status, ok := claudeToolResultExitStatus(line, ev.ToolUseID, ev.ToolResult, blockRaw)
			var p *int
			if ok {
				v := status
				p = &v
			}
			out = append(out, CommandExecutionEvent{
				Type:                commandEventFinished,
				Source:              commandEventSourceClaude,
				ToolUseID:           ev.ToolUseID,
				ToolName:            "Bash",
				ExitStatus:          p,
				ExitStatusAvailable: ok,
			})
		}
	}
	return out
}

func claudeToolResultBlock(line []byte, toolUseID string) (json.RawMessage, bool) {
	var top struct {
		Message struct {
			Content []json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &top); err != nil {
		return nil, false
	}
	for _, raw := range top.Message.Content {
		var head struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		if head.Type == "tool_result" && head.ToolUseID == toolUseID {
			return raw, true
		}
	}
	return nil, false
}

func extractCodexCommandEvents(line []byte) []CommandExecutionEvent {
	var top struct {
		Type string `json:"type"`
		Item struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Command  string `json:"command"`
			ExitCode *int   `json:"exit_code"`
		} `json:"item"`
	}
	if err := json.Unmarshal(line, &top); err != nil || top.Item.Type != "command_execution" {
		return nil
	}
	switch top.Type {
	case "item.started":
		cmd := strings.TrimSpace(top.Item.Command)
		if cmd == "" {
			return nil
		}
		return []CommandExecutionEvent{{
			Type:      commandEventStarted,
			Source:    commandEventSourceCodex,
			ToolUseID: top.Item.ID,
			ToolName:  "shell",
			Command:   cmd,
		}}
	case "item.completed":
		ev := CommandExecutionEvent{
			Type:      commandEventFinished,
			Source:    commandEventSourceCodex,
			ToolUseID: top.Item.ID,
			ToolName:  "shell",
			Command:   strings.TrimSpace(top.Item.Command),
		}
		if top.Item.ExitCode != nil {
			v := *top.Item.ExitCode
			ev.ExitStatus = &v
			ev.ExitStatusAvailable = true
		}
		return []CommandExecutionEvent{ev}
	default:
		return nil
	}
}

func isShellToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "shell":
		return true
	default:
		return false
	}
}

func shellCommandFromToolInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	for _, key := range []string{"command", "cmd"} {
		if v, ok := obj[key].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func claudeToolResultExitStatus(line []byte, toolUseID string, result json.RawMessage, blockRaw json.RawMessage) (int, bool) {
	if n, ok := jsonIntField(blockRaw, "exit_code", "exitCode", "exit_status"); ok {
		return n, true
	}
	if n, ok := jsonIntField(result, "exit_code", "exitCode", "exit_status"); ok {
		return n, true
	}
	if n, ok := claudeTopLevelToolExit(line); ok {
		return n, true
	}
	for _, text := range []string{jsonText(result), jsonText(blockRaw)} {
		if n, ok := parseExitStatusText(text); ok {
			return n, true
		}
	}
	if b, ok := jsonBoolField(blockRaw, "is_error"); ok && !b {
		return 0, true
	}
	return 0, false
}

func claudeTopLevelToolExit(line []byte) (int, bool) {
	var top struct {
		ToolUseResult json.RawMessage `json:"tool_use_result"`
	}
	if err := json.Unmarshal(line, &top); err != nil || len(top.ToolUseResult) == 0 {
		return 0, false
	}
	return jsonIntField(top.ToolUseResult, "exit_code", "exitCode", "exit_status")
}

func jsonIntField(raw json.RawMessage, keys ...string) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0, false
	}
	for _, k := range keys {
		v, ok := obj[k]
		if !ok {
			continue
		}
		if n, ok := parseJSONInt(v); ok {
			return n, true
		}
	}
	return 0, false
}

func parseJSONInt(raw json.RawMessage) (int, bool) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, perr := strconv.Atoi(strings.TrimSpace(s)); perr == nil {
			return v, true
		}
	}
	return 0, false
}

func jsonBoolField(raw json.RawMessage, key string) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, false
	}
	v, ok := obj[key]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return false, false
	}
	return b, true
}

func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw)
	}
	for _, key := range []string{"content", "stdout", "stderr", "output", "aggregated_output"} {
		if v, ok := obj[key]; ok {
			if text := jsonText(v); text != "" {
				return text
			}
		}
	}
	return string(raw)
}

var exitStatusPattern = regexp.MustCompile(`(?i)\b(?:exit\s+(?:code|status)|exited\s+with\s+code)\D+(-?\d+)\b`)

func parseExitStatusText(s string) (int, bool) {
	m := exitStatusPattern.FindStringSubmatch(s)
	if len(m) != 2 {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}
