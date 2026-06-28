package workerdaemon

import (
	"context"
	"encoding/json"
	"testing"
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
