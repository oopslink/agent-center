package workerdaemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oopslink/agent-center/internal/workforce"
)

// v2.7 #147: the daemon uploads its probed capabilities (rich shape — version
// + feature flags preserved) to the dedicated report endpoint on every online.
func TestAdminClient_ReportCapabilities_PostsRichShape(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()

	caps := []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true, Enabled: true, Version: "1.2.3", SupportsMCP: true},
		{AgentCLI: "opencode", Detected: false},
	}
	if err := client.ReportCapabilities(context.Background(), "w-1", caps); err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/admin/workforce/worker/capabilities" {
		t.Fatalf("expected POST to /capabilities, got %+v", reqs)
	}
	var body struct {
		WorkerID     string                 `json:"worker_id"`
		Capabilities []workforce.Capability `json:"capabilities"`
	}
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body.WorkerID != "w-1" {
		t.Fatalf("worker_id=%q", body.WorkerID)
	}
	if len(body.Capabilities) != 2 {
		t.Fatalf("want 2 caps, got %d", len(body.Capabilities))
	}
	// Rich fields must survive the wire (the whole point of the report channel).
	cc := body.Capabilities[0]
	if cc.AgentCLI != "claude-code" || cc.Version != "1.2.3" || !cc.SupportsMCP || !cc.Detected {
		t.Fatalf("rich fields lost in transit: %+v", cc)
	}
}

func TestAdminClient_ReportCapabilities_EmptyWorkerIDFails(t *testing.T) {
	_, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.ReportCapabilities(context.Background(), "  ", nil); err == nil {
		t.Fatal("expected error for empty worker_id")
	}
}
