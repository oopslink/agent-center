package agentruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

// fakeToolCaller records CallAgentTool invocations.
type fakeToolCaller struct {
	tool string
	body map[string]any
	err  error
}

func (f *fakeToolCaller) CallAgentTool(_ context.Context, tool string, body any, _ *json.RawMessage) error {
	f.tool = tool
	// Round-trip through JSON so we assert the wire shape the center receives.
	b, _ := json.Marshal(body)
	_ = json.Unmarshal(b, &f.body)
	return f.err
}

func TestNewCenterClient_NilCaller(t *testing.T) {
	if newCenterClient(nil) != nil {
		t.Fatal("nil caller should yield nil CenterClient (graceful degrade)")
	}
	if newCenterClient(&fakeToolCaller{}) == nil {
		t.Fatal("non-nil caller should yield a CenterClient")
	}
}

func TestCenterClient_CompleteTask(t *testing.T) {
	fc := &fakeToolCaller{}
	cc := newCenterClient(fc)
	if err := cc.CompleteTask(context.Background(), "agent-1", "task-7", "done well"); err != nil {
		t.Fatal(err)
	}
	if fc.tool != "complete_task" {
		t.Errorf("tool = %q", fc.tool)
	}
	if fc.body["agent_id"] != "agent-1" || fc.body["task_id"] != "task-7" || fc.body["summary"] != "done well" {
		t.Errorf("body = %v", fc.body)
	}
}

func TestCenterClient_BlockTask(t *testing.T) {
	fc := &fakeToolCaller{}
	cc := newCenterClient(fc)
	if err := cc.BlockTask(context.Background(), "agent-1", "task-7", "broke", "obstacle"); err != nil {
		t.Fatal(err)
	}
	if fc.tool != "block_task" {
		t.Errorf("tool = %q", fc.tool)
	}
	if fc.body["reason"] != "broke" || fc.body["reason_type"] != "obstacle" || fc.body["task_id"] != "task-7" {
		t.Errorf("body = %v", fc.body)
	}
}

func TestCenterClient_PostMessage(t *testing.T) {
	fc := &fakeToolCaller{}
	cc := newCenterClient(fc)
	if err := cc.PostMessage(context.Background(), "agent-1", "chan-3", "hello"); err != nil {
		t.Fatal(err)
	}
	if fc.tool != "post_message" {
		t.Errorf("tool = %q", fc.tool)
	}
	target, ok := fc.body["target"].(map[string]any)
	if !ok || target["type"] != "conversation" || target["id"] != "chan-3" {
		t.Errorf("target = %v", fc.body["target"])
	}
	if fc.body["content"] != "hello" {
		t.Errorf("content = %v", fc.body["content"])
	}
}

func TestNewUsageReporter_NilCaller(t *testing.T) {
	if newUsageReporter(nil) != nil {
		t.Fatal("nil caller should yield nil UsageReporter (graceful degrade)")
	}
	if newUsageReporter(&fakeToolCaller{}) == nil {
		t.Fatal("non-nil caller should yield a UsageReporter")
	}
}

func TestUsageReporter_ReportUsage_FullBody(t *testing.T) {
	fc := &fakeToolCaller{}
	ur := newUsageReporter(fc)
	at := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	if err := ur.ReportUsage(context.Background(), orchestrator.UsageSample{
		AgentID: "agent-1",
		TaskID:  "task-7",
		Model:   "claude-haiku-4-5",
		Usage:   executor.TokenUsage{InputTokens: 100, OutputTokens: 40, CacheReadTokens: 5, CacheWriteTokens: 2},
		At:      at,
	}); err != nil {
		t.Fatal(err)
	}
	if fc.tool != "report_usage" {
		t.Errorf("tool = %q", fc.tool)
	}
	// JSON numbers round-trip as float64.
	if fc.body["agent_id"] != "agent-1" || fc.body["task_id"] != "task-7" || fc.body["model"] != "claude-haiku-4-5" {
		t.Errorf("body meta = %v", fc.body)
	}
	if fc.body["input_tokens"] != float64(100) || fc.body["output_tokens"] != float64(40) ||
		fc.body["cache_read_tokens"] != float64(5) || fc.body["cache_write_tokens"] != float64(2) {
		t.Errorf("body tokens = %v", fc.body)
	}
	if fc.body["ts"] != at.Format(time.RFC3339Nano) {
		t.Errorf("ts = %v", fc.body["ts"])
	}
}

func TestUsageReporter_ReportUsage_OmitsEmptyOptionals(t *testing.T) {
	// Empty task_id, zero cache fields, and a zero timestamp are omitted (so a
	// task-less run carries no task_id; acceptance ②).
	fc := &fakeToolCaller{}
	ur := newUsageReporter(fc)
	if err := ur.ReportUsage(context.Background(), orchestrator.UsageSample{
		AgentID: "agent-1",
		Model:   "claude-haiku-4-5",
		Usage:   executor.TokenUsage{InputTokens: 7, OutputTokens: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := fc.body["task_id"]; ok {
		t.Errorf("task_id must be omitted when empty, body = %v", fc.body)
	}
	if _, ok := fc.body["cache_read_tokens"]; ok {
		t.Errorf("cache_read_tokens must be omitted when zero")
	}
	if _, ok := fc.body["cache_write_tokens"]; ok {
		t.Errorf("cache_write_tokens must be omitted when zero")
	}
	if _, ok := fc.body["ts"]; ok {
		t.Errorf("ts must be omitted when zero")
	}
	// Required fields still present.
	if fc.body["input_tokens"] != float64(7) || fc.body["output_tokens"] != float64(3) {
		t.Errorf("body tokens = %v", fc.body)
	}
}
