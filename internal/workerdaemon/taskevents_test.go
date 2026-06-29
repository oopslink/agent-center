package workerdaemon

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
)

// newTaskEventController builds a controller with the per-task dir manager wired
// (so recordTaskEvent is active) and a live managedAgent whose in-flight task is
// taskID. Returns the controller and the resolved task dir.
func newTaskEventController(t *testing.T, taskID string, segMax, logMax int64) (*AgentController, string) {
	t.Helper()
	base := t.TempDir()
	c, _, _ := newTestController(t, base)
	c.cfg.TaskDirManager = taskexec.NewDirManager()
	c.cfg.SegmentMaxBytes = segMax
	c.cfg.TaskLogMaxBytes = logMax
	c.agents["agent-1"] = &managedAgent{agentID: "agent-1", currentTaskID: taskID}
	_, tasksDir, _, err := c.agentPaths("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	return c, filepath.Join(tasksDir, taskID)
}

func textEvent(s string) claudestream.StreamEvent {
	return claudestream.StreamEvent{Type: "assistant_text", Text: s}
}

// readGzEvents decompresses an archived segment and returns its RawEvents.
func readGzEvents(t *testing.T, path string) []taskexec.RawEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open gz: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gz: %v", err)
	}
	var out []taskexec.RawEvent
	for _, line := range splitLines(raw) {
		if len(line) == 0 {
			continue
		}
		var ev taskexec.RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("unmarshal gz line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// TestRecordTaskEvent_WritesLocalArtifacts is the core W3/W4 wiring assertion: a
// stream event for an in-flight task must land in events.current.jsonl AND
// task.log (the false-green bug was that NEITHER was ever written at runtime).
func TestRecordTaskEvent_WritesLocalArtifacts(t *testing.T) {
	c, taskDir := newTaskEventController(t, "task-A", 0, 0)

	c.recordTaskEvent("agent-1", "task-A", textEvent("hello world"),
		"assistant_text", `{"type":"assistant_text","text":"hello world"}`, true)

	// W3: events.current.jsonl exists with the event.
	w := taskexec.NewEventStreamWriter()
	events, err := w.ReadAll(taskDir)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "assistant_text" {
		t.Fatalf("want 1 assistant_text event, got %+v", events)
	}
	if events[0].TaskRef != "pm://tasks/task-A" {
		t.Errorf("task_ref = %q, want pm://tasks/task-A", events[0].TaskRef)
	}

	// W4: task.log exists with content.
	logBytes, err := os.ReadFile(filepath.Join(taskDir, "task.log"))
	if err != nil {
		t.Fatalf("read task.log: %v", err)
	}
	if len(logBytes) == 0 {
		t.Fatal("task.log is empty")
	}

	// Ack advanced the offset to EOF.
	off, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatalf("read offset: %v", err)
	}
	size, _ := w.CurrentSegmentSize(taskDir)
	if off.Segment != "current" || off.ByteOffset != size {
		t.Fatalf("offset = %+v, want current/%d", off, size)
	}
}

// TestRecordTaskEvent_NoInFlightTask is a no-op when there is no current task
// (idle / converse turn) — nothing is written.
func TestRecordTaskEvent_NoInFlightTask(t *testing.T) {
	c, taskDir := newTaskEventController(t, "task-A", 0, 0)
	c.recordTaskEvent("agent-1", "" /*no task*/, textEvent("x"), "assistant_text", `{}`, true)
	if _, err := os.Stat(filepath.Join(taskDir, "events.current.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("events.current.jsonl should not exist, stat err=%v", err)
	}
}

// TestSealOnCompletion verifies the completion seal: when the agent calls
// complete_task, the (final) segment is force-rolled into events.*.jsonl.gz that
// decompresses back to the events, and the offset resets to current/0.
func TestSealOnCompletion(t *testing.T) {
	c, taskDir := newTaskEventController(t, "task-A", 0, 0)
	w := taskexec.NewEventStreamWriter()

	// A couple of ordinary events (well below the 8 MiB threshold → no roll yet).
	c.recordTaskEvent("agent-1", "task-A", textEvent("step 1"), "assistant_text", `{"text":"step 1"}`, true)
	c.recordTaskEvent("agent-1", "task-A", textEvent("step 2"), "assistant_text", `{"text":"step 2"}`, true)
	if segs, _ := w.ListArchivedSegments(taskDir); len(segs) != 0 {
		t.Fatalf("no roll expected below threshold, got %v", segs)
	}

	// Agent completes the task via the MCP center tool (claude reports the prefixed
	// name) → seal.
	complete := claudestream.StreamEvent{Type: "tool_use", ToolName: "mcp__agent-center__complete_task"}
	c.recordTaskEvent("agent-1", "task-A", complete, "tool_use", `{"tool_name":"complete_task"}`, true)

	segs, err := w.ListArchivedSegments(taskDir)
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("want 1 archived segment after seal, got %v", segs)
	}
	gzEvents := readGzEvents(t, filepath.Join(taskDir, segs[0]))
	// 2 text events + the complete_task tool_use = 3 events in the sealed segment.
	if len(gzEvents) != 3 {
		t.Fatalf("want 3 events in sealed gz, got %d: %+v", len(gzEvents), gzEvents)
	}

	// Offset reset to a fresh current segment.
	off, err := w.ReadOffset(taskDir)
	if err != nil {
		t.Fatalf("read offset: %v", err)
	}
	if off.Segment != "current" || off.ByteOffset != 0 {
		t.Fatalf("offset after seal = %+v, want current/0", off)
	}
	// task.log closed (writer cleared) but file persists with content.
	if c.agents["agent-1"].taskLog != nil {
		t.Error("task.log writer should be closed after seal")
	}
	if b, _ := os.ReadFile(filepath.Join(taskDir, "task.log")); len(b) == 0 {
		t.Error("task.log should persist with content after seal")
	}
}

// TestSizeBasedRoll verifies the design §8.1 size-threshold roll: once the
// current segment crosses SegmentMaxBytes AND is fully acked, it archives.
func TestSizeBasedRoll(t *testing.T) {
	c, taskDir := newTaskEventController(t, "task-A", 256 /*tiny threshold*/, 0)
	w := taskexec.NewEventStreamWriter()

	big := ""
	for i := 0; i < 40; i++ {
		big += "0123456789"
	}
	rolled := false
	for i := 0; i < 5; i++ {
		c.recordTaskEvent("agent-1", "task-A", textEvent(big), "assistant_text", `{"text":"`+big+`"}`, true)
		if segs, _ := w.ListArchivedSegments(taskDir); len(segs) > 0 {
			rolled = true
			break
		}
	}
	if !rolled {
		t.Fatal("expected a size-based segment roll once past the threshold")
	}
}

// TestUnackedNotRolled: an undelivered (un-acked) event is persisted but the
// segment is never archived, so un-acked data is never compressed away.
func TestUnackedNotRolled(t *testing.T) {
	c, taskDir := newTaskEventController(t, "task-A", 64, 0)
	w := taskexec.NewEventStreamWriter()
	big := ""
	for i := 0; i < 40; i++ {
		big += "0123456789"
	}
	// delivered=false → no ack → MaybeRollSegment must not archive even past size.
	c.recordTaskEvent("agent-1", "task-A", textEvent(big), "assistant_text", `{"text":"`+big+`"}`, false)
	if segs, _ := w.ListArchivedSegments(taskDir); len(segs) != 0 {
		t.Fatalf("un-acked segment must not roll, got %v", segs)
	}
	events, _ := w.ReadAll(taskDir)
	if len(events) != 1 {
		t.Fatalf("event still persisted, want 1 got %d", len(events))
	}
	off, _ := w.ReadOffset(taskDir)
	if off.ByteOffset != 0 {
		t.Fatalf("un-acked offset must stay 0, got %d", off.ByteOffset)
	}
}

func TestIsTaskTerminalTool(t *testing.T) {
	cases := map[string]bool{
		"complete_task":                    true,
		"mcp__agent-center__complete_task": true,
		"mcp__agent-center__discard_task":  true,
		"discard_task":                     true,
		"block_task":                       false,
		"mcp__agent-center__start_task":    false,
		"ls":                               false,
		"":                                 false,
	}
	for name, want := range cases {
		if got := isTaskTerminalTool(name); got != want {
			t.Errorf("isTaskTerminalTool(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTaskIDFromStartTool(t *testing.T) {
	cases := []struct {
		name, tool, input, want string
	}{
		{"start_task plain", "start_task", `{"task_id":"task-1"}`, "task-1"},
		{"start_task mcp", "mcp__agent-center__start_task", `{"task_id":"task-2"}`, "task-2"},
		{"claim_task", "mcp__agent-center__claim_task", `{"task_id":"task-3"}`, "task-3"},
		{"not a start tool", "mcp__agent-center__complete_task", `{"task_id":"task-4"}`, ""},
		{"ls", "ls", `{}`, ""},
		{"no task_id", "start_task", `{"foo":"bar"}`, ""},
		{"empty input", "start_task", ``, ""},
	}
	for _, tc := range cases {
		got := taskIDFromStartTool(tc.tool, json.RawMessage(tc.input))
		if got != tc.want {
			t.Errorf("%s: taskIDFromStartTool(%q,%q) = %q, want %q", tc.name, tc.tool, tc.input, got, tc.want)
		}
	}
}

// TestOnEvent_PullModelTaskBoundary is the production-shape end-to-end: a
// pull-model agent (currentTaskID NEVER set — no per-task brief inject) drives
// itself via MCP start_task → ls → complete_task. The W3/W4 sink must route the
// events to the task derived from start_task, write task.log + events, and seal
// the segment on complete_task. This is the case the false-green bug actually hit.
func TestOnEvent_PullModelTaskBoundary(t *testing.T) {
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.TaskDirManager = taskexec.NewDirManager()
	// No currentTaskID — exactly the pull model: the controller never injected a
	// brief; the agent self-serves.
	c.agents["agent-1"] = &managedAgent{agentID: "agent-1"}

	mcp := func(name, input string) claudestream.StreamEvent {
		return claudestream.StreamEvent{Type: "tool_use", ToolName: name, ToolUseID: name, ToolInput: json.RawMessage(input)}
	}

	// Pre-task: the agent checks its queue (routes nowhere — no task yet).
	c.onEvent("agent-1", mcp("mcp__agent-center__list_my_tasks", `{}`))
	// Open the task.
	c.onEvent("agent-1", mcp("mcp__agent-center__start_task", `{"task_id":"task-PULL"}`))
	// Do the work.
	c.onEvent("agent-1", textEvent("running ls"))
	c.onEvent("agent-1", mcp("ls", `{}`))
	// Complete → seal.
	c.onEvent("agent-1", mcp("mcp__agent-center__complete_task", `{"task_id":"task-PULL"}`))

	_, tasksDir, _, _ := c.agentPaths("agent-1")
	taskDir := filepath.Join(tasksDir, "task-PULL")
	w := taskexec.NewEventStreamWriter()

	// Segment sealed into a .gz that decompresses back to the task's events.
	segs, err := w.ListArchivedSegments(taskDir)
	if err != nil || len(segs) != 1 {
		t.Fatalf("want 1 sealed segment for the pull task, got %v err=%v", segs, err)
	}
	gzEvents := readGzEvents(t, filepath.Join(taskDir, segs[0]))
	// start_task + assistant_text + ls + complete_task = 4 events (the pre-task
	// list_my_tasks routed nowhere).
	if len(gzEvents) != 4 {
		t.Fatalf("want 4 events in sealed pull segment, got %d: %+v", len(gzEvents), gzEvents)
	}

	// task.log persisted with content.
	if b, _ := os.ReadFile(filepath.Join(taskDir, "task.log")); len(b) == 0 {
		t.Error("pull task.log should have content")
	}
	// Offset reset to a fresh current segment.
	off, _ := w.ReadOffset(taskDir)
	if off.Segment != "current" || off.ByteOffset != 0 {
		t.Errorf("offset after seal = %+v, want current/0", off)
	}
	// The Center activity stream ALSO saw every event (5 incl. the pre-task probe).
	if len(rep.activities) != 5 {
		t.Errorf("want 5 Center activities, got %d", len(rep.activities))
	}
	// Routing anchor cleared after completion.
	if c.agents["agent-1"].eventTaskID != "" {
		t.Errorf("eventTaskID should be cleared after complete_task, got %q", c.agents["agent-1"].eventTaskID)
	}
}

// TestOnEvent_EndToEnd drives a stream event through the real onEvent path (not
// recordTaskEvent directly) to prove the wiring fires from the actual sink and
// the Center activity is ALSO reported (both paths, not one-or-the-other).
func TestOnEvent_EndToEnd(t *testing.T) {
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.TaskDirManager = taskexec.NewDirManager()
	c.agents["agent-1"] = &managedAgent{agentID: "agent-1", currentTaskID: "task-Z"}

	c.onEvent("agent-1", textEvent("driven via onEvent"))

	_, tasksDir, _, _ := c.agentPaths("agent-1")
	taskDir := filepath.Join(tasksDir, "task-Z")
	events, err := taskexec.NewEventStreamWriter().ReadAll(taskDir)
	if err != nil || len(events) != 1 {
		t.Fatalf("onEvent should append 1 local event, got %d err=%v", len(events), err)
	}
	if len(rep.activities) != 1 || rep.activities[0].taskRef != "task-Z" {
		t.Fatalf("onEvent should ALSO report the Center activity for the task, got %+v", rep.activities)
	}
}
