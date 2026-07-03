package agentruntime

// taskevents.go — the W3 per-task event-stream archival + W4 per-task log sink,
// moved off AgentController. recordTaskEvent is the single entry onEvent calls after
// reporting the activity to the Center.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
	"github.com/oopslink/agent-center/internal/workerdaemon/tasklog"
)

const taskLogFile = "task.log"

func tasklogOpen(taskDir string, maxBytes int64) (*tasklog.Writer, error) {
	return tasklog.Open(filepath.Join(taskDir, taskLogFile), maxBytes)
}

var taskTerminalToolSuffixes = []string{"complete_task", "discard_task"}
var taskStartToolSuffixes = []string{"start_task", "claim_task"}

func (r *LocalRuntime) segmentMaxBytes() int64 {
	if r.cfg.SegmentMaxBytes > 0 {
		return r.cfg.SegmentMaxBytes
	}
	return taskexec.DefaultSegmentMaxBytes
}

func (r *LocalRuntime) taskLogMaxBytes() int64 {
	return r.cfg.TaskLogMaxBytes
}

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

// recordTaskEvent persists one stream event to the in-flight task's LOCAL on-disk
// artifacts (best-effort). No-op when task-dir management is disabled or there is no
// in-flight task.
func (r *LocalRuntime) recordTaskEvent(agentID, taskID string, ev claudestream.StreamEvent, eventType, payload string, delivered bool) {
	if r.cfg.TaskDirManager == nil || taskID == "" {
		return
	}
	_, tasksDir, _, err := r.agentPaths(agentID)
	if err != nil {
		return
	}
	taskDir := filepath.Join(tasksDir, taskID)

	r.cfg.Mu.Lock()
	st := r.state
	if st.TaskLogID != taskID && st.TaskLog != nil {
		_ = st.TaskLog.Close()
		st.TaskLog, st.TaskLogID = nil, ""
	}
	if st.TaskLog == nil {
		if w, oerr := tasklogOpen(taskDir, r.taskLogMaxBytes()); oerr != nil {
			r.log("tasklog agent=%s task=%s open: %v", agentID, taskID, oerr)
		} else {
			st.TaskLog, st.TaskLogID = w, taskID
		}
	}
	logw := st.TaskLog
	st.EventSeq++
	seq := st.EventSeq
	r.cfg.Mu.Unlock()

	if logw != nil {
		line := fmt.Sprintf("%s\t%s\t%s\n", r.now().UTC().Format(time.RFC3339Nano), eventType, payload)
		if _, werr := logw.Write([]byte(line)); werr != nil {
			r.log("tasklog agent=%s task=%s write: %v", agentID, taskID, werr)
		}
	}

	raw := taskexec.RawEvent{
		ID:         fmt.Sprintf("%s-%06d", taskID, seq),
		EventType:  eventType,
		TaskRef:    "pm://tasks/" + taskID,
		Payload:    payload,
		OccurredAt: r.now().UTC(),
	}
	if aerr := r.cfg.EventWriter.Append(taskDir, raw); aerr != nil {
		r.log("eventstream agent=%s task=%s append: %v", agentID, taskID, aerr)
		return
	}

	if delivered {
		if _, ackErr := r.cfg.EventWriter.AckCurrent(taskDir, raw.ID); ackErr != nil {
			r.log("eventstream agent=%s task=%s ack: %v", agentID, taskID, ackErr)
		} else if name, rollErr := r.cfg.EventWriter.MaybeRollSegment(taskDir, r.segmentMaxBytes()); rollErr != nil {
			r.log("eventstream agent=%s task=%s roll: %v", agentID, taskID, rollErr)
		} else if name != "" {
			r.log("eventstream agent=%s task=%s rolled segment → %s", agentID, taskID, name)
		}
	}

	if ev.Type == "tool_use" && isTaskTerminalTool(ev.ToolName) {
		r.sealTaskSegment(agentID, taskID, taskDir, raw.ID)
	}
}

// sealTaskSegment force-archives the in-flight task's current event segment on
// completion (best-effort).
func (r *LocalRuntime) sealTaskSegment(agentID, taskID, taskDir, lastEventID string) {
	if _, err := r.cfg.EventWriter.AckCurrent(taskDir, lastEventID); err != nil {
		r.log("eventstream agent=%s task=%s seal ack: %v", agentID, taskID, err)
	}
	if name, err := r.cfg.EventWriter.MaybeRollSegment(taskDir, 1); err != nil {
		r.log("eventstream agent=%s task=%s seal roll: %v", agentID, taskID, err)
	} else if name != "" {
		r.log("eventstream agent=%s task=%s sealed segment → %s", agentID, taskID, name)
	}

	r.cfg.Mu.Lock()
	if r.state.TaskLogID == taskID {
		if r.state.TaskLog != nil {
			_ = r.state.TaskLog.Close()
		}
		r.state.TaskLog, r.state.TaskLogID = nil, ""
	}
	r.cfg.Mu.Unlock()
}
