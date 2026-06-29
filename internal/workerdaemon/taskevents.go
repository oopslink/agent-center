// Package workerdaemon: taskevents.go wires the W3 per-task event-stream archival
// (taskexec.EventStreamWriter) and the W4 per-task log sink (tasklog.Writer) into
// the live agent execution path (issue-5753e8fa). Until this, both subsystems had
// green unit tests but ZERO runtime callers — a deployed agent produced no
// tasks/{id}/events.current.jsonl, no events.*.jsonl.gz, and no task.log.
//
// The default agent is a supervisor-session claude (`--print`, ONE long-lived
// session running every task inline — no per-task child process to tee). So there
// is no per-task stdout to redirect. The single seam that DOES exist is onEvent:
// the stdout→activity sink the AgentController already runs for every claude
// stream event, which already knows the in-flight task via currentTaskID
// (workItemRef). That is the task-output boundary the Activity stream uses, and we
// reuse it verbatim: every stream event observed while currentTaskID == T is part
// of task T's output. recordTaskEvent is the single entry point onEvent calls
// after reporting the activity to the Center.
package workerdaemon

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
	"github.com/oopslink/agent-center/internal/workerdaemon/tasklog"
)

// taskLogFile is the W4 per-task log filename under tasks/{id}/ (design §3).
const taskLogFile = "task.log"

// tasklogOpen opens the rotating task.log writer for a task dir.
func tasklogOpen(taskDir string, maxBytes int64) (*tasklog.Writer, error) {
	return tasklog.Open(filepath.Join(taskDir, taskLogFile), maxBytes)
}

// taskTerminalToolSuffixes are the MCP center-tool names (claude reports them as
// "mcp__agent-center__<tool>") whose invocation means the agent has finished with
// the in-flight task, so its event segment is final and can be sealed/archived.
// We match by suffix to stay decoupled from the mcp server-name prefix.
var taskTerminalToolSuffixes = []string{"complete_task", "discard_task"}

// segmentMaxBytes is the configured event-segment roll threshold, or the taskexec
// default (8 MiB) when unset/non-positive.
func (c *AgentController) segmentMaxBytes() int64 {
	if c.cfg.SegmentMaxBytes > 0 {
		return c.cfg.SegmentMaxBytes
	}
	return taskexec.DefaultSegmentMaxBytes
}

// taskLogMaxBytes is the configured task.log rotation cap, or 0 to let
// tasklog.Open fall back to its own default (10 MiB).
func (c *AgentController) taskLogMaxBytes() int64 {
	return c.cfg.TaskLogMaxBytes
}

// taskStartToolSuffixes are the MCP center-tool names that BEGIN the agent's work
// on a task in the pull model, so their task_id argument identifies the task the
// W3/W4 sink routes subsequent stream events to.
var taskStartToolSuffixes = []string{"start_task", "claim_task"}

// isTaskTerminalTool reports whether toolName is a center tool that ends the
// in-flight task (complete_task / discard_task), matched by suffix so the
// "mcp__agent-center__" prefix claude prepends is irrelevant.
func isTaskTerminalTool(toolName string) bool {
	return toolMatchesSuffix(toolName, taskTerminalToolSuffixes)
}

func toolMatchesSuffix(toolName string, suffixes []string) bool {
	for _, s := range suffixes {
		if toolName == s || strings.HasSuffix(toolName, "__"+s) {
			return true
		}
	}
	return false
}

// taskIDFromStartTool returns the task_id argument of a start_task/claim_task
// tool_use (the call that opens a task in the pull model), or "" when toolName is
// not a start tool or the input has no task_id. The MCP input is a JSON object
// like {"task_id":"task-..."}.
func taskIDFromStartTool(toolName string, toolInput json.RawMessage) string {
	if !toolMatchesSuffix(toolName, taskStartToolSuffixes) || len(toolInput) == 0 {
		return ""
	}
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(toolInput, &args); err != nil {
		return ""
	}
	return strings.TrimSpace(args.TaskID)
}

// recordTaskEvent persists one claude stream event to the in-flight task's LOCAL
// on-disk artifacts (design §3 task.log + §8 event stream). It is the local
// counterpart to the Center activity report and is BEST-EFFORT: every failure is
// logged, never propagated (a logging-sink failure must not disturb the live
// turn). It is a no-op when task-dir management is disabled (TaskDirManager nil)
// or there is no in-flight task (taskID == "", e.g. an idle/converse turn).
//
//   - W4: tee a human-readable line into tasks/{id}/task.log (rotating writer).
//   - W3: append the event to tasks/{id}/events.current.jsonl.
//   - ack (delivered == true): advance events.offset to EOF, then roll the segment
//     into events.{seq}.jsonl.gz if it crossed the size threshold AND is fully
//     acked (design §8.1).
//   - completion: when ev is a terminal-tool call (complete_task/discard_task),
//     force-seal the segment regardless of size and close the task.log.
func (c *AgentController) recordTaskEvent(agentID, taskID string, ev StreamEvent, eventType, payload string, delivered bool) {
	if c.cfg.TaskDirManager == nil || taskID == "" {
		return
	}
	_, tasksDir, _, err := c.agentPaths(agentID)
	if err != nil {
		return
	}
	taskDir := filepath.Join(tasksDir, taskID)

	// Acquire (lazily open / rotate) the per-task log writer and a monotonic event
	// seq under the lock; do the actual disk IO outside it (tasklog.Writer and the
	// stateless eventWriter are each independently safe).
	c.mu.Lock()
	ma := c.agents[agentID]
	if ma == nil {
		c.mu.Unlock()
		return
	}
	if ma.taskLogID != taskID && ma.taskLog != nil {
		// In-flight task changed — the previous task's log is done.
		_ = ma.taskLog.Close()
		ma.taskLog, ma.taskLogID = nil, ""
	}
	if ma.taskLog == nil {
		if w, oerr := tasklogOpen(taskDir, c.taskLogMaxBytes()); oerr != nil {
			c.log("tasklog agent=%s task=%s open: %v", agentID, taskID, oerr)
		} else {
			ma.taskLog, ma.taskLogID = w, taskID
		}
	}
	logw := ma.taskLog
	ma.eventSeq++
	seq := ma.eventSeq
	c.mu.Unlock()

	// W4: tee the event into task.log as a single tab-separated line.
	if logw != nil {
		line := fmt.Sprintf("%s\t%s\t%s\n", c.now().UTC().Format(time.RFC3339Nano), eventType, payload)
		if _, werr := logw.Write([]byte(line)); werr != nil {
			c.log("tasklog agent=%s task=%s write: %v", agentID, taskID, werr)
		}
	}

	// W3: append the event to events.current.jsonl.
	raw := taskexec.RawEvent{
		ID:         fmt.Sprintf("%s-%06d", taskID, seq),
		EventType:  eventType,
		TaskRef:    "pm://tasks/" + taskID,
		Payload:    payload,
		OccurredAt: c.now().UTC(),
	}
	if aerr := c.eventWriter.Append(taskDir, raw); aerr != nil {
		c.log("eventstream agent=%s task=%s append: %v", agentID, taskID, aerr)
		return
	}

	// Ack the delivered event locally (advance offset to EOF) and roll the segment
	// if it crossed the size threshold while fully acked (design §8.1).
	if delivered {
		if _, ackErr := c.eventWriter.AckCurrent(taskDir, raw.ID); ackErr != nil {
			c.log("eventstream agent=%s task=%s ack: %v", agentID, taskID, ackErr)
		} else if name, rollErr := c.eventWriter.MaybeRollSegment(taskDir, c.segmentMaxBytes()); rollErr != nil {
			c.log("eventstream agent=%s task=%s roll: %v", agentID, taskID, rollErr)
		} else if name != "" {
			c.log("eventstream agent=%s task=%s rolled segment → %s", agentID, taskID, name)
		}
	}

	// Completion: a terminal-tool call means the agent is done with this task, so
	// its (now final) segment is sealed into an archive even below the size
	// threshold, and the task.log is closed.
	if ev.Type == "tool_use" && isTaskTerminalTool(ev.ToolName) {
		c.sealTaskSegment(agentID, taskID, taskDir, raw.ID)
	}
}

// sealTaskSegment force-archives the in-flight task's current event segment when
// the task completes, regardless of the size threshold: a completed task's
// segment is final, so it is acked to EOF (all its events were delivered to the
// activity stream) and rolled into events.{seq}.jsonl.gz. It then closes the
// task.log writer. Best-effort: every step logs and continues.
func (c *AgentController) sealTaskSegment(agentID, taskID, taskDir, lastEventID string) {
	if _, err := c.eventWriter.AckCurrent(taskDir, lastEventID); err != nil {
		c.log("eventstream agent=%s task=%s seal ack: %v", agentID, taskID, err)
	}
	// threshold 1: any non-empty, fully-acked segment rolls.
	if name, err := c.eventWriter.MaybeRollSegment(taskDir, 1); err != nil {
		c.log("eventstream agent=%s task=%s seal roll: %v", agentID, taskID, err)
	} else if name != "" {
		c.log("eventstream agent=%s task=%s sealed segment → %s", agentID, taskID, name)
	}

	c.mu.Lock()
	if ma := c.agents[agentID]; ma != nil && ma.taskLogID == taskID {
		if ma.taskLog != nil {
			_ = ma.taskLog.Close()
		}
		ma.taskLog, ma.taskLogID = nil, ""
	}
	c.mu.Unlock()
}
