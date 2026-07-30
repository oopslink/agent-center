package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/claudestream"
)

func TestCenterBypassAlertFromEvent_DetectsAndRedactsShellAccess(t *testing.T) {
	in, _ := json.Marshal(map[string]string{
		"command": "AC_MCP_WORKER_TOKEN=secret sqlite3 /Users/oopslink/.agent-center/var/agent-center.db 'select 1'",
	})
	alert, ok := CenterBypassAlertFromEvent(claudestream.StreamEvent{
		Type:      "tool_use",
		ToolName:  "shell",
		ToolInput: in,
	})
	if !ok {
		t.Fatal("expected center bypass alert")
	}
	got := strings.Join(alert.Reasons, ",")
	if !strings.Contains(got, "agent-center-db") || !strings.Contains(got, "sqlite-agent-center") || !strings.Contains(got, "worker-token-env") {
		t.Fatalf("reasons = %v, want db/sqlite/token reasons", alert.Reasons)
	}
	if strings.Contains(alert.CommandSnippet, "secret") {
		t.Fatalf("command snippet leaked token: %q", alert.CommandSnippet)
	}
	if !strings.Contains(alert.CommandSnippet, "AC_MCP_WORKER_TOKEN=<redacted>") {
		t.Fatalf("command snippet not redacted: %q", alert.CommandSnippet)
	}
}

func TestCenterBypassAlertFromEvent_IgnoresOrdinaryShell(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"command": "go test ./internal/agentruntime"})
	_, ok := CenterBypassAlertFromEvent(claudestream.StreamEvent{
		Type:      "tool_use",
		ToolName:  "shell",
		ToolInput: in,
	})
	if ok {
		t.Fatal("ordinary shell command must not raise center bypass alert")
	}
}
