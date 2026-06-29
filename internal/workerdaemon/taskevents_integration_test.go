package workerdaemon

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
)

// realClaudeTaskRun is the verbatim shape of a claude 2.1.156 stream-json
// transcript for a pull-model agent running ONE dispatched task: woken, it checks
// its queue, start_task's the assigned task, runs `ls`, then complete_task's it.
// These are the exact top-level line shapes ParseStreamLine consumes in
// production (system init / assistant tool_use / user tool_result / assistant
// text / result). Driving them through the REAL parser → onEvent proves the W3/W4
// sink produces the on-disk artifacts the deploy acceptance checks, with no
// hand-built StreamEvents in the path.
var realClaudeTaskRun = []string{
	`{"type":"system","subtype":"init","session_id":"abc","model":"claude-opus-4-8","mcp_servers":[{"name":"agent-center"}]}`,
	`{"type":"assistant","message":{"content":[{"type":"text","text":"Let me check my task queue."}]}}`,
	`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__agent-center__list_my_tasks","input":{}}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"task-RUN: List the files (open, assigned to you)"}]}}`,
	`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"mcp__agent-center__start_task","input":{"task_id":"task-RUN"}}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"started"}]}}`,
	`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"ls"}}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"README.md\ngo.mod\ninternal"}]}}`,
	`{"type":"assistant","message":{"content":[{"type":"text","text":"The root has README.md, go.mod, internal."}]}}`,
	`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t4","name":"mcp__agent-center__complete_task","input":{"task_id":"task-RUN","summary":"listed files"}}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t4","content":"completed"}]}}`,
	`{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.01}`,
}

// TestRealClaudeStream_ProducesDeployArtifacts replays a real claude stream-json
// transcript through ParseStreamLine → onEvent and asserts the EXACT disk
// artifacts the deploy-real-run acceptance checks (issue-5753e8fa):
//   - tasks/task-RUN/task.log exists with content (W4)
//   - tasks/task-RUN/events.*.jsonl.gz exists and gunzips back to the events (W3)
//   - events.offset is reset to current/0 after the seal
//
// This is the daemon-level proof that the wiring closes the false-green gap. The
// only thing it does NOT exercise is the supervisor socket pump (covered by
// supervisor_session_test.go) and the launchd/center wake delivery (a separate
// harness concern, see issue-bbeffe0a).
func TestRealClaudeStream_ProducesDeployArtifacts(t *testing.T) {
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.TaskDirManager = taskexec.NewDirManager()
	c.agents["agent-1"] = &managedAgent{agentID: "agent-1"} // pull model: no currentTaskID

	for _, line := range realClaudeTaskRun {
		evs, err := claudestream.ParseStreamLine([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamLine(%s): %v", line, err)
		}
		for _, ev := range evs {
			c.onEvent("agent-1", ev)
		}
	}

	_, tasksDir, _, _ := c.agentPaths("agent-1")
	taskDir := filepath.Join(tasksDir, "task-RUN")
	w := taskexec.NewEventStreamWriter()

	// W4: task.log present + non-empty.
	logBytes, err := os.ReadFile(filepath.Join(taskDir, "task.log"))
	if err != nil || len(logBytes) == 0 {
		t.Fatalf("W4: tasks/task-RUN/task.log missing/empty: err=%v len=%d", err, len(logBytes))
	}

	// W3: archived .gz present and decompresses back to the task's events.
	segs, err := w.ListArchivedSegments(taskDir)
	if err != nil || len(segs) != 1 {
		t.Fatalf("W3: want 1 archived segment, got %v err=%v", segs, err)
	}
	gzPath := filepath.Join(taskDir, segs[0])
	f, err := os.Open(gzPath)
	if err != nil {
		t.Fatalf("open gz: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader (W3 gz must be valid): %v", err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gz: %v", err)
	}
	var count, startSeen, completeSeen int
	for _, line := range splitLines(raw) {
		if len(line) == 0 {
			continue
		}
		var ev taskexec.RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("gz line not valid event JSON: %v", err)
		}
		count++
		if ev.TaskRef != "pm://tasks/task-RUN" {
			t.Errorf("event task_ref = %q, want pm://tasks/task-RUN", ev.TaskRef)
		}
		// Each archived event payload carries the original tool name.
		if ev.EventType == "tool_use" {
			if strings.Contains(ev.Payload, "start_task") {
				startSeen++
			}
			if strings.Contains(ev.Payload, "complete_task") {
				completeSeen++
			}
		}
	}
	// start_task → ... → complete_task all belong to the task segment; the
	// pre-task list_my_tasks (and its tool_result) route nowhere.
	if startSeen != 1 || completeSeen != 1 {
		t.Errorf("sealed segment should bracket start_task..complete_task, got start=%d complete=%d", startSeen, completeSeen)
	}
	if count < 5 {
		t.Errorf("sealed segment unexpectedly small: %d events", count)
	}

	// W3: offset reset to a fresh current segment after the seal.
	off, _ := w.ReadOffset(taskDir)
	if off.Segment != "current" || off.ByteOffset != 0 {
		t.Errorf("offset after seal = %+v, want current/0", off)
	}

	// The Center activity stream saw every parsed event too (both paths fire).
	if len(rep.activities) == 0 {
		t.Error("expected Center activities to also be reported")
	}
	t.Logf("deploy-shape artifacts OK: task.log=%dB, sealed %s with %d events, %d Center activities",
		len(logBytes), segs[0], count, len(rep.activities))
}
