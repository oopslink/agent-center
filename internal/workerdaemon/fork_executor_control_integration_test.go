package workerdaemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestSpawnExecutor_TaskInputOverAdminGetTaskListFilesDownload(t *testing.T) {
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("true binary unavailable: %v", err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(png)
	fileID := "01M0HRMZEV7XS8A3MNGG64ZZW1"
	var tools []string
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/agent-tools/get_task", func(w http.ResponseWriter, r *http.Request) {
		tools = append(tools, "get_task")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "task-real-input", "title": "real input", "status": "open",
			"assignee": "agent:agent-real-input", "model": "test-model",
		})
	})
	mux.HandleFunc("/admin/agent-tools/list_files", func(w http.ResponseWriter, r *http.Request) {
		tools = append(tools, "list_files")
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{
			"uri": "ac://files/" + fileID, "filename": "T1457-mockup.png", "mime_type": "image/png",
			"size": len(png), "sha256": hex.EncodeToString(sum[:]),
		}}})
	})
	mux.HandleFunc("/admin/agent-tools/start_task", func(w http.ResponseWriter, r *http.Request) {
		tools = append(tools, "start_task")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/admin/agent-tools/get_team_rule_index", func(w http.ResponseWriter, r *http.Request) {
		tools = append(tools, "get_team_rule_index")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/admin/files/"+fileID, func(w http.ResponseWriter, r *http.Request) {
		tools = append(tools, "download")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := &AdminClient{baseURL: srv.URL, httpc: srv.Client()}

	base := t.TempDir()
	agentID := "agent-real-input"
	rt := agentruntime.NewLocalRuntime(agentruntime.LocalRuntimeConfig{
		AgentID: agentID, WorkerID: "worker-control", AgentHomeBase: base,
		BinaryPath: trueBin, Reporter: forkControlReporter{},
		ToolCaller:     func() agentruntime.ToolCaller { return client },
		FileDownloader: client,
		Log:            func(string, ...any) {},
	}, &agentruntime.SessionState{})
	home := filepath.Join(base, "agents", agentID)
	ee, err := rt.BuildExecutorEngine(home, agentruntime.ExecutorConfig{
		AgentID: agentID, AgentRef: "agent-real-input", MaxConcurrentTasks: 1, DefaultExecutorModel: "test-model",
		AllowedExecutors: []agent.ExecutorProfile{{CLI: "claude-code", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	rt.AttachExecutor(ee)

	res, err := rt.SpawnExecutor(context.Background(), agentruntime.SpawnRequest{TaskID: "task-real-input"})
	if err != nil || res == nil || res.ExecutorID == "" {
		t.Fatalf("SpawnExecutor = (%+v, %v)", res, err)
	}
	if len(tools) < 4 || strings.Join(tools[:4], ",") != "get_task,list_files,download,start_task" {
		t.Fatalf("tool/download order = %v, want get_task/list_files/download/start_task before launch", tools)
	}
	got, err := os.ReadFile(filepath.Join(home, "executors", res.ExecutorID, "workspace", "task-input", "v1", "files", "T1457-mockup.png"))
	if err != nil {
		t.Fatalf("read materialized file: %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("materialized bytes differ")
	}
}
