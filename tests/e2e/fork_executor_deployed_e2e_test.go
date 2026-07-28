package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
)

// TestForkExecutorDeployedBinary_AlreadyRunningTask proves the shipped process
// boundary, not just the in-process runtime seam:
//
//	freshly-built agent-center worker agent-runtime
//	  -> real Unix control socket
//	  -> agent.fork_executor for an already-running task
//	  -> freshly-built agent-center worker executor child
//	  -> real /usr/bin/true runner and durable output.json
//
// The fake center is intentionally only the remote HTTP boundary. Both runtime
// processes, the control transport, file exchange, process pool, and child runner
// are production implementations from the binary under test.
func TestForkExecutorDeployedBinary_AlreadyRunningTask(t *testing.T) {
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("true binary unavailable: %v", err)
	}
	binary := ensureBinary(t)
	const (
		workerID = "worker-deployed-fork"
		agentID  = "agent-deployed-fork"
		taskID   = "task-deployed-running"
		token    = "test-worker-token"
	)

	var (
		requestsMu sync.Mutex
		requests   []string
	)
	sockDir, err := os.MkdirTemp("/tmp", "ac-deployed-fork-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	centerSock := filepath.Join(sockDir, "center.sock")
	centerListener, err := net.Listen("unix", centerSock)
	if err != nil {
		t.Fatal(err)
	}
	center := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			http.Error(w, "bad bearer", http.StatusUnauthorized)
			return
		}
		requestsMu.Lock()
		requests = append(requests, r.URL.Path)
		requestsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/admin/environment/worker/resume-state":
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": []any{map[string]any{
				"agent_id": agentID, "agent_ref": agentID, "desired_lifecycle": "stopped",
				"cli": "claude-code", "max_concurrent_tasks": 2,
				"allowed_executors":      []any{map[string]any{"cli": "claude-code", "model": "test-model"}},
				"default_executor_model": "test-model", "version": 1, "tasks": []any{},
			}}})
		case "/admin/agent-tools/get_task":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": taskID, "title": "already admitted deployed task", "description": "exercise the deployed fork path",
				"status": "running", "assignee": "agent:" + agentID, "model": "test-model",
			})
		default:
			// Completion/activity reporting is outside this regression's center-side
			// scope; accept it so the real executor can drain and finalize cleanly.
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	})}
	centerDone := make(chan error, 1)
	go func() { centerDone <- center.Serve(centerListener) }()
	t.Cleanup(func() {
		_ = center.Close()
		<-centerDone
	})
	adminTarget := "unix:" + centerSock

	root := t.TempDir()
	cfgPath := filepath.Join(root, "agent-center.yaml")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("server:\n  sqlite_path: %q\n  admin_socket_path: %q\n", filepath.Join(stateDir, "center.db"), filepath.Join(root, "admin.sock"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	stdoutPath := filepath.Join(root, "agent-runtime.stdout.log")
	stderrPath := filepath.Join(root, "agent-runtime.stderr.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		_ = stdoutFile.Close()
		t.Fatal(err)
	}
	processOutput := func() (string, string) {
		out, _ := os.ReadFile(stdoutPath)
		errOut, _ := os.ReadFile(stderrPath)
		return string(out), string(errOut)
	}
	cmd := exec.Command(binary, "worker", "agent-runtime",
		"--config", cfgPath,
		"--worker-id", workerID,
		"--agent-id", agentID,
		"--sock-dir", sockDir,
		"--admin-target", adminTarget,
		"--admin-token", token,
		"--tick-interval", "50ms",
	)
	cmd.Env = append(os.Environ(),
		"AGENT_CENTER_CLAUDE_BINARY="+trueBin,
		"AGENT_CENTER_DISABLE_USAGE_REPORT=1",
	)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	if err := cmd.Start(); err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		t.Fatalf("start deployed agent-runtime: %v", err)
	}
	// The child has duplicated both fds; close the parent's copies so teardown waits
	// only for the process, never an os/exec pipe-copy goroutine.
	_ = stdoutFile.Close()
	_ = stderrFile.Close()
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-processDone:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-processDone
		}
	})

	sockPath := filepath.Join(sockDir, agentcontrol.SocketName(agentID))
	control := agentcontrol.NewClient(sockPath, 2*time.Second)
	if !waitFor(10*time.Second, func() bool {
		got, probeErr := control.Probe(context.Background())
		return probeErr == nil && got == agentID
	}) {
		out, errOut := processOutput()
		t.Fatalf("deployed agent-runtime control socket never became ready\nstdout:\n%s\nstderr:\n%s", out, errOut)
	}

	payload, err := json.Marshal(map[string]string{"agent_id": agentID, "task_id": taskID})
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Deliver(context.Background(), agentcontrol.Command{
		Type: "agent.fork_executor", AgentID: agentID, Seq: 1, Payload: payload,
	}); err != nil {
		_, errOut := processOutput()
		t.Fatalf("deliver deployed fork command: %v\nstderr:\n%s", err, errOut)
	}

	executorsRoot := filepath.Join(stateDir, "agents", agentID, "executors")
	var outputPaths []string
	if !waitFor(10*time.Second, func() bool {
		outputPaths, _ = filepath.Glob(filepath.Join(executorsRoot, "*", "output.json"))
		return len(outputPaths) == 1
	}) {
		inputs, _ := filepath.Glob(filepath.Join(executorsRoot, "*", "input.json"))
		out, errOut := processOutput()
		t.Fatalf("deployed executor did not produce exactly one output; inputs=%v outputs=%v\nstdout:\n%s\nstderr:\n%s",
			inputs, outputPaths, out, errOut)
	}
	raw, err := os.ReadFile(outputPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Success bool   `json:"success"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("decode deployed executor output: %v; body=%s", err, raw)
	}
	if !output.Success {
		_, errOut := processOutput()
		t.Fatalf("deployed executor output success=false; body=%s\nstderr:\n%s", raw, errOut)
	}

	requestsMu.Lock()
	paths := strings.Join(requests, "\n")
	requestsMu.Unlock()
	if !strings.Contains(paths, "/admin/agent-tools/get_task") {
		t.Fatalf("deployed runtime never fetched task; requests:\n%s", paths)
	}
	if strings.Contains(paths, "/admin/agent-tools/start_task") {
		t.Fatalf("already-running task was admitted twice; requests:\n%s", paths)
	}
}
