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
	sysInfo := workforce.SystemInfo{
		Hostname:           "dev001.local",
		OS:                 "darwin",
		Arch:               "arm64",
		AgentCenterVersion: "v2.10.2",
		InstallPath:        "/usr/local/bin/agent-center",
		WorkerVersion:      "v2.10.2+abc1234",
	}
	if err := client.ReportCapabilities(context.Background(), "w-1", caps, sysInfo); err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/admin/workforce/worker/capabilities" {
		t.Fatalf("expected POST to /capabilities, got %+v", reqs)
	}
	var body struct {
		WorkerID     string                 `json:"worker_id"`
		Capabilities []workforce.Capability `json:"capabilities"`
		SystemInfo   workforce.SystemInfo   `json:"system_info"`
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
	// T752: system info must ride the same report.
	if body.SystemInfo != sysInfo {
		t.Fatalf("system_info lost in transit: got %+v want %+v", body.SystemInfo, sysInfo)
	}
}

// T752: an empty SystemInfo is omitted from the wire (byte-compatible with the
// pre-T752 shape; an older center simply sees no field).
func TestAdminClient_ReportCapabilities_OmitsEmptySystemInfo(t *testing.T) {
	fs, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.ReportCapabilities(context.Background(), "w-1", nil, workforce.SystemInfo{}); err != nil {
		t.Fatalf("ReportCapabilities: %v", err)
	}
	reqs := fs.reqs()
	if len(reqs) != 1 {
		t.Fatalf("want 1 req, got %d", len(reqs))
	}
	var raw map[string]any
	if err := json.Unmarshal(reqs[0].Body, &raw); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if _, present := raw["system_info"]; present {
		t.Fatalf("empty system_info must be omitted, body=%s", string(reqs[0].Body))
	}
}

func TestAdminClient_ReportCapabilities_EmptyWorkerIDFails(t *testing.T) {
	_, client, cleanup := newFakeServer(t)
	defer cleanup()
	if err := client.ReportCapabilities(context.Background(), "  ", nil, workforce.SystemInfo{}); err == nil {
		t.Fatal("expected error for empty worker_id")
	}
}
