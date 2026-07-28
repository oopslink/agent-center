package workerdaemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/agentruntime"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
)

type forkControlReporter struct{}

func (forkControlReporter) ReportAgentActivity(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}
func (forkControlReporter) ReportAgentLifecycle(context.Context, string, string, string, time.Time) error {
	return nil
}
func (forkControlReporter) ReportMarkSeen(context.Context, string, string, string, time.Time) error {
	return nil
}
func (forkControlReporter) ReportConverseError(context.Context, string, string, string, time.Time) error {
	return nil
}
func (forkControlReporter) FetchReplyNudges(context.Context, string) ([]string, error) {
	return nil, nil
}
func (forkControlReporter) ReportUsage(context.Context, agentruntime.UsageReport) error { return nil }
func (forkControlReporter) RenewTaskLease(context.Context, string, string, time.Time) error {
	return nil
}
func (forkControlReporter) ReportRuntimeFsResponse(context.Context, runtimefs.Response) error {
	return nil
}

type runningTaskToolCaller struct{}

func (runningTaskToolCaller) CallAgentTool(_ context.Context, tool string, _ any, out *json.RawMessage) error {
	if tool == "get_task" && out != nil {
		raw, _ := json.Marshal(map[string]any{
			"id": "task-running", "title": "already admitted", "status": "running",
			"assignee": "agent:agent-control", "model": "test-model",
		})
		*out = append((*out)[:0], raw...)
	}
	return nil
}

// TestForkExecutorControl_AlreadyRunningTaskLaunchesThroughUnixSocket covers the real
// worker→agent command boundary: JSON command decode, HTTP over Unix socket, runtime
// running-state compatibility, Pool reservation, and an actual OS child process.
func TestForkExecutorControl_AlreadyRunningTaskLaunchesThroughUnixSocket(t *testing.T) {
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("true binary unavailable: %v", err)
	}
	base := t.TempDir()
	agentID := "agent-control"
	rt := agentruntime.NewLocalRuntime(agentruntime.LocalRuntimeConfig{
		AgentID: agentID, WorkerID: "worker-control", AgentHomeBase: base,
		BinaryPath: trueBin, Reporter: forkControlReporter{},
		ToolCaller: func() agentruntime.ToolCaller { return runningTaskToolCaller{} },
		Log:        func(string, ...any) {},
	}, &agentruntime.SessionState{})
	home := filepath.Join(base, "agents", agentID)
	ee, err := rt.BuildExecutorEngine(home, agentruntime.ExecutorConfig{
		AgentID: agentID, MaxConcurrentTasks: 2, DefaultExecutorModel: "test-model",
		AllowedExecutors: []agent.ExecutorProfile{{CLI: "claude-code", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	rt.AttachExecutor(ee)

	sockDir, err := os.MkdirTemp("/tmp", "ac-fork-int-")
	if err != nil {
		t.Fatalf("MkdirTemp socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, agentcontrol.SocketName(agentID))
	srv, err := agentcontrol.NewServer(sock, agentID, agentControlHandler{rt: rt, log: func(string) {}}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve() }()
	t.Cleanup(func() {
		_ = srv.Close(context.Background())
		<-serveDone
	})

	payload := json.RawMessage(`{"agent_id":"agent-control","task_id":"task-running"}`)
	client := agentcontrol.NewClient(sock, 5*time.Second)
	if err := client.Deliver(context.Background(), agentcontrol.Command{
		Type: cmdTypeAgentForkExec, AgentID: agentID, Seq: 41, Payload: payload,
	}); err != nil {
		t.Fatalf("Deliver fork command: %v", err)
	}

	inputs, err := filepath.Glob(filepath.Join(home, "executors", "*", "input.json"))
	if err != nil {
		t.Fatalf("glob executor inputs: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("executor input files = %d, want exactly 1; paths=%v", len(inputs), inputs)
	}
	raw, err := os.ReadFile(inputs[0])
	if err != nil {
		t.Fatalf("read executor input: %v", err)
	}
	var input struct {
		Source struct {
			TaskRef string `json:"task_ref"`
		} `json:"source"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatalf("decode executor input: %v", err)
	}
	if input.Source.TaskRef != "task-running" {
		t.Fatalf("executor task_ref = %q, want task-running", input.Source.TaskRef)
	}
}
