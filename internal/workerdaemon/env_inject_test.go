package workerdaemon

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestEnvInjection_ForShimAndAgent(t *testing.T) {
	eta := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	env := dispatch.DispatchEnvelope{
		ExecutionID: "E-1", TaskID: "T-1", WorkerID: "W-1", ProjectID: "P-1",
		ConversationID: "C-1", AgentCLI: "claude-code",
		WorkspaceMode: execution.WorkspaceWorktree, Priority: "high",
		EtaAt: &eta,
	}
	inj := FromEnvelope(env, "/repo/.wt/task-E-1", "tok-xyz")
	shim := inj.ForShim()
	if shim["AGENT_CENTER_EXECUTION_ID"] != "E-1" {
		t.Fatal("exec id")
	}
	if shim["AGENT_CENTER_SHIM_TOKEN"] != "tok-xyz" {
		t.Fatal("token")
	}
	if shim["AGENT_CENTER_CWD"] != "/repo/.wt/task-E-1" {
		t.Fatal("cwd")
	}
	if shim["AGENT_CENTER_CONVERSATION_ID"] != "C-1" {
		t.Fatal("conv")
	}
	if shim["AGENT_CENTER_PRIORITY"] != "high" {
		t.Fatal("priority")
	}
	if shim["AGENT_CENTER_ETA_AT"] == "" {
		t.Fatal("eta")
	}
	agent := inj.ForAgent("/path/to/shim.sock")
	if _, ok := agent["AGENT_CENTER_SHIM_TOKEN"]; ok {
		t.Fatal("agent must not see shim token")
	}
	if agent["AGENT_CENTER_WORKER_SOCK"] != "/path/to/shim.sock" {
		t.Fatal("worker sock")
	}
}

func TestEnvInjection_EmptyConvAndEta(t *testing.T) {
	env := dispatch.DispatchEnvelope{
		ExecutionID: "E", TaskID: "T", WorkerID: "W", ProjectID: "P",
		AgentCLI: "x", WorkspaceMode: execution.WorkspaceDirect, Priority: "low",
	}
	inj := FromEnvelope(env, "/repo", "")
	shim := inj.ForShim()
	if shim["AGENT_CENTER_CONVERSATION_ID"] != "" {
		t.Fatal("expected empty conv")
	}
	if shim["AGENT_CENTER_ETA_AT"] != "" {
		t.Fatal("expected empty eta")
	}
}
