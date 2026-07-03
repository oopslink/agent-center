package workerdaemon

// worker_controller.go — the worker-as-controller wiring (T854 D6, design §4.5). When
// enabled, the worker stops hosting N runtimes in-process and instead:
//   - launches ONE `worker agent-runtime` OS process per desired agent (via the
//     agentlauncher, rebuilt on exit);
//   - keeps the single worker-scoped center control stream + cursor and PROXIES each
//     command to the target agent's process (via agentcontrol), cursor-gated so a
//     command is never acked until the agent process accepts it (PD reliability ruling).
//
// This is gated behind AC_WORKER_CONTROLLER during rollout: OFF ⇒ the pre-D6 in-process
// path, byte-for-byte. The final step of D6 flips the default and removes the old path
// + SelfHealStore.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentlauncher"
	"github.com/oopslink/agent-center/internal/workerdaemon/workercontroller"
)

// workerControllerEnabled reports whether the process-per-agent controller path is on.
func workerControllerEnabled() bool {
	v := strings.TrimSpace(os.Getenv("AC_WORKER_CONTROLLER"))
	return v == "1" || strings.EqualFold(v, "true")
}

// controllerHandler is the CommandHandler that proxies each center control command to
// its target agent PROCESS. It returns the delivery error unchanged so the control
// loop leaves an undelivered command un-acked (no cursor advance) and retries — the
// no-lost-command guarantee across an agent restart window.
type controllerHandler struct {
	ctrl *workercontroller.Controller
	log  func(string)
}

// Handle routes one command to the target agent's process.
func (h controllerHandler) Handle(ctx context.Context, cmd ControlCommand) error {
	var idp struct {
		AgentID          string `json:"agent_id"`
		DesiredLifecycle string `json:"desired_lifecycle"`
	}
	_ = json.Unmarshal([]byte(cmd.Payload), &idp)
	agentID := strings.TrimSpace(idp.AgentID)
	if agentID == "" {
		// Unroutable (no agent_id): ack it (return nil) so a malformed command does not
		// wedge the cursor forever; log for visibility.
		h.log(fmt.Sprintf("controller: command type=%s offset=%d has no agent_id — skipping", cmd.CommandType, cmd.Offset))
		return nil
	}
	// A reconcile to a desired-stopped agent tears its process down; everything else
	// (reconcile-running, work, wake, converse, work_available) is proxied to the
	// agent process — reconcile-running also ensures the process is up + brings up the
	// session via the agent's rt.Start.
	if cmd.CommandType == cmdTypeAgentReconcile && idp.DesiredLifecycle == "stopped" {
		return h.ctrl.StopAgent(agentID)
	}
	return h.ctrl.Deliver(ctx, agentcontrol.Command{
		Type:    cmd.CommandType,
		AgentID: agentID,
		Seq:     cmd.Offset,
		Payload: json.RawMessage(cmd.Payload),
	})
}

// buildWorkerController wires the launcher + controller for the worker. sockDir is a
// SHORT per-worker runtime dir (unix socket path limit) the agent processes bind their
// control sockets in; the launched agent-runtime processes are told the same dir.
func buildWorkerController(opts RunOptions, targetSpec, token, fingerprint string, logf func(string)) (*workercontroller.Controller, error) {
	sockDir, err := workerSockDir(opts.WorkerID)
	if err != nil {
		return nil, err
	}
	// The base args every launched agent-runtime process needs to reach the center +
	// bind its control socket (mirrors what the worker resolved). Secrets ride argv here
	// for the single-machine model; the k8s launcher would use a mounted secret instead.
	baseArgs := []string{
		"--worker-id", opts.WorkerID,
		"--sock-dir", sockDir,
	}
	if s := strings.TrimSpace(opts.ConfigPath); s != "" {
		baseArgs = append(baseArgs, "--config", s)
	}
	if s := strings.TrimSpace(targetSpec); s != "" {
		baseArgs = append(baseArgs, "--admin-target", s)
	}
	if s := strings.TrimSpace(token); s != "" {
		baseArgs = append(baseArgs, "--admin-token", s)
	}
	if s := strings.TrimSpace(fingerprint); s != "" {
		baseArgs = append(baseArgs, "--server-fingerprint", s)
	}
	starter, err := agentlauncher.NewExecStarter(agentlauncher.ExecStarterConfig{BaseArgs: baseArgs})
	if err != nil {
		return nil, err
	}
	launcher, err := agentlauncher.New(agentlauncher.Config{
		Starter: starter,
		Log:     func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) },
	})
	if err != nil {
		return nil, err
	}
	return workercontroller.New(workercontroller.Config{
		Launcher: launcher,
		SockDir:  sockDir,
		Log:      func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) },
	})
}

// workerSockDir returns (creating) a SHORT per-worker runtime dir for the agent control
// sockets. Kept short (under the OS temp dir, hashed worker id) so the full socket path
// fits the unix sun_path limit even when the worker home is deep.
func workerSockDir(workerID string) (string, error) {
	sum := sha1.Sum([]byte(workerID))
	dir := filepath.Join(os.TempDir(), "acw-"+hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("worker controller: sock dir: %w", err)
	}
	return dir, nil
}

// reconcileControllerFromResumeState brings the controller's launched set in line with
// the center's desired agent list (the running ones) at boot. Best-effort: a query
// failure logs and leaves the set unchanged (the per-command reconciles still ensure
// agents lazily on first command).
func reconcileControllerFromResumeState(ctx context.Context, ctrl *workercontroller.Controller, client *AdminClient, workerID string, logf func(string)) {
	state, err := client.ResumeState(ctx, workerID)
	if err != nil {
		logf(fmt.Sprintf("controller: resume-state at boot: %v (agents will start lazily on first command)", err))
		return
	}
	var desired []string
	for _, ra := range state.Agents {
		if strings.EqualFold(strings.TrimSpace(ra.DesiredLifecycle), "running") {
			desired = append(desired, ra.AgentID)
		}
	}
	ctrl.Reconcile(desired)
	logf(fmt.Sprintf("controller: reconciled %d desired-running agent(s) at boot", len(desired)))
}
