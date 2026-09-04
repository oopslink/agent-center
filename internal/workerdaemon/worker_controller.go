package workerdaemon

// worker_controller.go — the worker-as-controller wiring (T854 D6, design §4.5). When
// enabled, the worker stops hosting N runtimes in-process and instead:
//   - launches ONE `worker agent-runtime` OS process per desired agent (via the
//     agentlauncher, rebuilt on exit);
//   - keeps the single worker-scoped center control stream + cursor and PROXIES each
//     command to the target agent's process (via agentcontrol), cursor-gated so a
//     command is never acked until the agent process accepts it (PD reliability ruling).
//
// This is the SOLE execution path (T860 ③): the pre-D6 in-process AgentController +
// SelfHealStore were removed after the §6 real-deploy acceptance validated the
// controller model, so there is no longer a gate to toggle.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/runtimedeploy"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentlauncher"
	"github.com/oopslink/agent-center/internal/workerdaemon/workercontroller"
)

const forkCommandExpireAfter = 15 * time.Minute

// controllerHandler is the CommandHandler that proxies each center control command to
// its target agent PROCESS. It returns the delivery error unchanged so the control
// loop leaves an undelivered command un-acked (no cursor advance) and retries — the
// no-lost-command guarantee across an agent restart window.
type controllerHandler struct {
	ctrl *workercontroller.Controller
	// reporter sends lifecycle RESULT feedback after a launcher-level stop settles.
	// The center state is already stopping/resetting; this marks it stopped without
	// emitting another reconcile command.
	reporter lifecycleReporter
	// homeBase is the per-agent layout root (agents/<id>/ lives under it); the runtime_fs
	// ops read the agent home directly (T860 gap2 — worker-level, not proxied).
	homeBase string
	// poster posts the runtime_fs read response back to the center (satisfied by
	// *AdminClient; narrowed to an interface so gap2 is unit-testable).
	poster runtimeFsPoster
	// deployer executes runtime.deploy_restart commands locally on the worker host.
	deployer runtimeDeployer
	log      func(string)
}

type lifecycleReporter interface {
	ReportAgentLifecycle(ctx context.Context, agentID, state, errMsg string, at time.Time) error
}

type commandStatusReporter interface {
	ReportControlCommandStatus(ctx context.Context, agentID, commandID, taskID, status, reason, detail, executionID string, at time.Time) error
}

type converseErrorReporter interface {
	ReportConverseError(ctx context.Context, agentID, conversationID, summary string, at time.Time) error
}

type runtimeDeployer interface {
	DeployRestart(ctx context.Context, req runtimedeploy.Request) (runtimedeploy.Result, error)
}

// runtimeFsPoster is the subset of *AdminClient the runtime_fs local handler needs.
type runtimeFsPoster interface {
	ReportRuntimeFsResponse(ctx context.Context, resp runtimefs.Response) error
}

// Handle routes one command to the target agent's process.
func (h controllerHandler) Handle(ctx context.Context, cmd ControlCommand) error {
	// T860 gap2: agent.runtime_fs is a WORKER-LEVEL read of the agent's home directory,
	// not an agent-process operation — the worker-controller serves it locally (reads
	// the home + posts the correlated response) instead of proxying it to the agent
	// process. Keeping it here also means it works even when the agent process is down.
	if cmd.CommandType == cmdTypeRuntimeFs {
		return h.handleRuntimeFs(ctx, []byte(cmd.Payload))
	}
	if cmd.CommandType == cmdTypeRuntimeDeploy {
		return h.handleRuntimeDeploy(ctx, cmd)
	}
	var idp struct {
		AgentID             string `json:"agent_id"`
		TaskID              string `json:"task_id"`
		DesiredLifecycle    string `json:"desired_lifecycle"`
		ExecutorGitWorktree bool   `json:"executor_git_worktree"`
		ResetScope          string `json:"reset_scope"`
	}
	_ = json.Unmarshal([]byte(cmd.Payload), &idp)
	agentID := strings.TrimSpace(idp.AgentID)
	if agentID == "" {
		// Unroutable (no agent_id): ack it (return nil) so a malformed command does not
		// wedge the cursor forever; log for visibility.
		h.log(fmt.Sprintf("controller: command type=%s offset=%d has no agent_id — skipping", cmd.CommandType, cmd.Offset))
		return nil
	}
	if cmd.CommandType == cmdTypeAgentForkExec {
		taskID := strings.TrimSpace(cmd.TaskID)
		if taskID == "" {
			taskID = strings.TrimSpace(idp.TaskID)
		}
		if shouldSkipForkCommand(cmd) {
			h.log(fmt.Sprintf("controller: fork_executor command id=%s offset=%d status=%s already terminal — skipping",
				cmd.ID, cmd.Offset, cmd.Status))
			return nil
		}
		if forkControlCommandExpired(cmd, time.Now()) {
			if rep, ok := h.reporter.(commandStatusReporter); ok && rep != nil && cmd.ID != "" {
				if err := rep.ReportControlCommandStatus(ctx, agentID, cmd.ID, taskID,
					environment.CommandStatusExpired, "runtime_command_timeout",
					"fork_executor command was not started before its pending timeout", "", time.Now()); err != nil {
					return err
				}
			}
			h.log(fmt.Sprintf("controller: fork_executor command id=%s offset=%d expired before delivery — skipping", cmd.ID, cmd.Offset))
			return nil
		}
	}
	// A reconcile to a stopping/resetting/stopped agent tears its process down
	// at the launcher level. Do not proxy these into the agent-runtime process:
	// an agent-runtime expected-stop exit reports no lifecycle, and the launcher
	// would otherwise rebuild it because the unit was never marked undesired.
	//
	// StopAgent's intent state is `stopping`; MarkAgentStopped accepts both
	// stopping and resetting as the settled `stopped` result.
	if cmd.CommandType == cmdTypeAgentReconcile && isStopDesiredLifecycle(idp.DesiredLifecycle) {
		if err := h.ctrl.StopAgent(agentID); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(idp.DesiredLifecycle), "resetting") {
			if err := h.resetAgentHome(agentID, idp.ResetScope); err != nil {
				return err
			}
		}
		if h.reporter != nil {
			if err := h.reporter.ReportAgentLifecycle(ctx, agentID, "stopped", "", time.Now()); err != nil && !isAlreadySettledStoppedFeedback(err) {
				return err
			}
		}
		return nil
	}
	if cmd.CommandType == cmdTypeAgentReconcile {
		if err := h.ctrl.EnsureAgentSpec(agentRuntimeSpec(agentID, idp.ExecutorGitWorktree)); err != nil {
			return err
		}
	}
	// A reconcile to a desired-running agent and all work/wake/converse commands
	// (reconcile-running, work, wake, converse, work_available) is proxied to the
	// agent process — reconcile-running also ensures the process is up + brings up the
	// session via the agent's rt.Start.
	acmd := agentcontrol.Command{
		Type:           cmd.CommandType,
		AgentID:        agentID,
		Seq:            cmd.Offset,
		ID:             cmd.ID,
		IdempotencyKey: cmd.IdempotencyKey,
		Status:         cmd.Status,
		CreatedAt:      cmd.CreatedAt,
		Payload:        json.RawMessage(cmd.Payload),
	}
	return h.deliverWithRuntimeRecovery(ctx, cmd, acmd, strings.TrimSpace(idp.TaskID))
}

func (h controllerHandler) deliverWithRuntimeRecovery(ctx context.Context, cmd ControlCommand, acmd agentcontrol.Command, taskID string) error {
	err := h.ctrl.Deliver(ctx, acmd)
	if err == nil {
		return nil
	}
	if !isAgentRuntimeTransportError(err) {
		return err
	}
	h.log(fmt.Sprintf("controller: runtime transport failed agent=%s command=%s offset=%d: %v; recycling runtime and retrying once",
		acmd.AgentID, acmd.Type, acmd.Seq, err))
	if rerr := h.ctrl.RecycleAgent(acmd.AgentID); rerr != nil {
		return fmt.Errorf("controller: recycle agent=%s after runtime transport failure: %w (delivery: %v)", acmd.AgentID, rerr, err)
	}
	retryErr := h.ctrl.Deliver(ctx, acmd)
	if retryErr == nil {
		h.log(fmt.Sprintf("controller: runtime transport recovered agent=%s command=%s offset=%d after recycle",
			acmd.AgentID, acmd.Type, acmd.Seq))
		return nil
	}
	if !isAgentRuntimeTransportError(retryErr) {
		return retryErr
	}
	detail := retryErr.Error()
	if detail == "" {
		detail = err.Error()
	}
	if rerr := h.reportRuntimeDeliveryParked(ctx, cmd, acmd.AgentID, taskID, detail); rerr != nil {
		return rerr
	}
	h.log(fmt.Sprintf("controller: RUNTIME-PARKED — command id=%s offset=%d type=%s agent=%s remained runtime-unreachable after recycle+retry; SKIPPING it so the worker cursor can advance: %v",
		cmd.ID, acmd.Seq, acmd.Type, acmd.AgentID, retryErr))
	return nil
}

func (h controllerHandler) reportRuntimeDeliveryParked(ctx context.Context, cmd ControlCommand, agentID, taskID, detail string) error {
	if cmd.CommandType == cmdTypeAgentForkExec {
		rep, _ := h.reporter.(commandStatusReporter)
		if rep == nil || strings.TrimSpace(cmd.ID) == "" {
			return nil
		}
		return rep.ReportControlCommandStatus(ctx, agentID, cmd.ID, taskID, environment.CommandStatusFailed,
			"agent_runtime_unreachable", detail, "", time.Now())
	}
	if cmd.CommandType == cmdTypeAgentConverse {
		rep, _ := h.reporter.(converseErrorReporter)
		if rep == nil {
			return nil
		}
		var pl conversePayload
		_ = json.Unmarshal([]byte(cmd.Payload), &pl)
		if strings.TrimSpace(pl.ConversationID) == "" {
			return nil
		}
		return rep.ReportConverseError(ctx, agentID, pl.ConversationID,
			"agent runtime was unreachable after recycle; converse command was skipped", time.Now())
	}
	return nil
}

func isAgentRuntimeTransportError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "agentcontrol: deliver") || strings.Contains(msg, "agent returned") {
		return false
	}
	for _, needle := range []string{
		"no such file or directory",
		"connection refused",
		"connection reset by peer",
		"broken pipe",
		"unexpected eof",
		": eof",
		"server closed idle connection",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func shouldSkipForkCommand(cmd ControlCommand) bool {
	switch strings.TrimSpace(cmd.Status) {
	case environment.CommandStatusRejected, environment.CommandStatusFailed, environment.CommandStatusExpired:
		return true
	default:
		return false
	}
}

func forkControlCommandExpired(cmd ControlCommand, now time.Time) bool {
	status := strings.TrimSpace(cmd.Status)
	if status != "" && status != environment.CommandStatusPending {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(cmd.CreatedAt))
	if err != nil || created.IsZero() {
		return false
	}
	return now.Sub(created) > forkCommandExpireAfter
}

func (h controllerHandler) handleRuntimeDeploy(ctx context.Context, cmd ControlCommand) error {
	rep, _ := h.reporter.(commandStatusReporter)
	var req runtimedeploy.Request
	if err := json.Unmarshal([]byte(cmd.Payload), &req); err != nil {
		return h.reportRuntimeDeployStatus(ctx, rep, cmd, "", environment.CommandStatusRejected, "invalid_payload", err.Error())
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(cmd.AgentID)
	}
	if agentID == "" {
		h.log(fmt.Sprintf("controller: runtime deploy command id=%s offset=%d has no agent_id — skipping", cmd.ID, cmd.Offset))
		return nil
	}
	if err := validateVerifiedRuntimeDeploy(req); err != nil {
		return h.reportRuntimeDeployStatus(ctx, rep, cmd, agentID, environment.CommandStatusRejected, "verification_required", err.Error())
	}
	if h.deployer == nil {
		return h.reportRuntimeDeployStatus(ctx, rep, cmd, agentID, environment.CommandStatusFailed, "runtime_deployer_not_wired", "controller has no runtime deployer")
	}
	if rep != nil && cmd.ID != "" && strings.TrimSpace(cmd.Status) == environment.CommandStatusPending {
		if err := rep.ReportControlCommandStatus(ctx, agentID, cmd.ID, "", environment.CommandStatusStarted, "runtime_deploy_started", "", "", time.Now()); err != nil {
			return err
		}
	}
	res, err := h.deployer.DeployRestart(ctx, req)
	if err != nil {
		return h.reportRuntimeDeployStatus(ctx, rep, cmd, agentID, environment.CommandStatusFailed, "runtime_deploy_failed", err.Error())
	}
	if err := runtimedeploy.ValidateTerminalSuccessResult(res, req.VerifiedTargetSHA); err != nil {
		detailBytes, _ := json.Marshal(res)
		detail := strings.TrimSpace(string(detailBytes))
		if detail == "" || detail == "{}" {
			detail = err.Error()
		} else {
			detail = err.Error() + ": " + detail
		}
		return h.reportRuntimeDeployStatus(ctx, rep, cmd, agentID, environment.CommandStatusFailed, "runtime_deploy_result_invalid", detail)
	}
	detailBytes, _ := json.Marshal(res)
	return h.reportRuntimeDeployStatus(ctx, rep, cmd, agentID, environment.CommandStatusSucceeded, "runtime_deploy_succeeded", string(detailBytes))
}

func validateVerifiedRuntimeDeploy(req runtimedeploy.Request) error {
	target := strings.ToLower(strings.TrimSpace(req.TargetSHA))
	verified := strings.ToLower(strings.TrimSpace(req.VerifiedTargetSHA))
	switch {
	case target == "":
		return errors.New("target_sha required")
	case verified == "":
		return errors.New("verified_target_sha required")
	case target != verified:
		return fmt.Errorf("verified_target_sha %s does not match target_sha %s", verified, target)
	case strings.TrimSpace(req.VerifiedBaseSHA) == "":
		return errors.New("verified_base_sha required")
	case strings.TrimSpace(req.VerifiedAt) == "":
		return errors.New("verified_at required")
	}
	return nil
}

func (h controllerHandler) reportRuntimeDeployStatus(ctx context.Context, rep commandStatusReporter, cmd ControlCommand, agentID, status, reason, detail string) error {
	if rep == nil || strings.TrimSpace(cmd.ID) == "" || strings.TrimSpace(agentID) == "" {
		if status == environment.CommandStatusFailed || status == environment.CommandStatusRejected {
			return fmt.Errorf("runtime deploy %s: %s", reason, detail)
		}
		return nil
	}
	if err := rep.ReportControlCommandStatus(ctx, agentID, cmd.ID, "", status, reason, detail, "", time.Now()); err != nil {
		return err
	}
	return nil
}

func (h controllerHandler) SnapshotConcurrency() map[string]concurrency.AgentSnapshot {
	if h.ctrl == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.ctrl.SnapshotConcurrency(ctx)
}

func (h controllerHandler) SnapshotExecutionState(ctx context.Context) map[string]concurrency.ExecutionStateSnapshot {
	if h.ctrl == nil {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return h.ctrl.SnapshotExecutionState(reqCtx)
}

func isStopDesiredLifecycle(lc string) bool {
	switch strings.ToLower(strings.TrimSpace(lc)) {
	case "stopping", "resetting", "stopped":
		return true
	default:
		return false
	}
}

func (h controllerHandler) resetAgentHome(agentID, scope string) error {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		return errors.New("controller: reset_scope required for desired_lifecycle=resetting")
	}
	home, err := containedAgentHome(h.homeBase, agentID)
	if err != nil {
		return err
	}
	var targets []string
	switch scope {
	case "memory":
		targets = []string{filepath.Join(home, "memory")}
	case "workspace":
		targets = []string{
			filepath.Join(home, "tasks"),
			filepath.Join(home, "plans"),
			filepath.Join(home, "executors"),
			filepath.Join(home, "workspace"),
		}
	case "all":
		targets = []string{home}
	default:
		return fmt.Errorf("controller: invalid reset_scope %q", scope)
	}
	for _, target := range targets {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("controller: reset %s remove %s: %w", scope, target, err)
		}
	}
	h.log(fmt.Sprintf("controller: reset agent=%s scope=%s applied", agentID, scope))
	return nil
}

func containedAgentHome(homeBase, agentID string) (string, error) {
	if strings.TrimSpace(homeBase) == "" {
		return "", errors.New("controller: home base required for reset")
	}
	if strings.TrimSpace(agentID) == "" || strings.ContainsAny(agentID, `/\:`) || strings.Contains(agentID, "..") {
		return "", fmt.Errorf("controller: invalid agent_id %q for reset", agentID)
	}
	base, err := filepath.Abs(homeBase)
	if err != nil {
		return "", fmt.Errorf("controller: resolve home base: %w", err)
	}
	home := filepath.Join(base, "agents", agentID)
	rel, err := filepath.Rel(base, home)
	if err != nil {
		return "", fmt.Errorf("controller: reset containment: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("controller: reset path escapes home base for agent %q", agentID)
	}
	return home, nil
}

func isAlreadySettledStoppedFeedback(err error) bool {
	var adminErr *AdminError
	if !errors.As(err, &adminErr) {
		return false
	}
	return adminErr.Status == http.StatusConflict && strings.Contains(adminErr.Body, "illegal_transition")
}

// handleRuntimeFs serves an agent.runtime_fs read locally (T860 gap2): it runs the op
// against the agent's home directory and posts the correlated response to the center. A
// malformed command / unknown op / op failure is reported as a Response with an error
// code (never leaks another agent's data — the home is joined from the command's own
// agent id under this worker's layout root, and the ops re-verify path containment).
// Returns nil (ack) once the response is posted; returns an error only when posting the
// response fails, so the control loop keeps the command un-acked and retries.
func (h controllerHandler) handleRuntimeFs(ctx context.Context, payload []byte) error {
	var pl runtimefs.Command
	if err := json.Unmarshal(payload, &pl); err != nil {
		// Unparseable payload has no ReqID to correlate — ack it (log) so it doesn't wedge
		// the cursor; the FE request just times out.
		h.log(fmt.Sprintf("controller: runtime_fs bad payload: %v", err))
		return nil
	}
	resp := runtimefs.Response{AgentID: pl.AgentID, ReqID: pl.ReqID}
	home := filepath.Join(h.homeBase, "agents", pl.AgentID)

	var (
		result any
		opErr  *runtimeFsError
	)
	switch pl.Op {
	case runtimefs.OpList:
		result, opErr = runtimeFsList(home, pl.Path)
	case runtimefs.OpRead:
		result, opErr = runtimeFsRead(home, pl.Path)
	case runtimefs.OpGitLog:
		result, opErr = runtimeFsGitLog(ctx, home, pl.Path, pl.Limit)
	case runtimefs.OpGitDiff:
		result, opErr = runtimeFsGitDiff(ctx, home, pl.Path, pl.Ref)
	default:
		opErr = &runtimeFsError{runtimefs.ErrCodeInternal, "unknown op " + pl.Op}
	}

	switch {
	case opErr != nil:
		resp.Code, resp.Message = opErr.code, opErr.msg
	default:
		raw, merr := json.Marshal(result)
		if merr != nil {
			resp.Code, resp.Message = runtimefs.ErrCodeInternal, merr.Error()
		} else {
			resp.Result = raw
		}
	}
	if err := h.poster.ReportRuntimeFsResponse(ctx, resp); err != nil {
		h.log(fmt.Sprintf("controller: runtime_fs post response req=%s: %v", pl.ReqID, err))
		return err // keep un-acked, retry
	}
	return nil
}

// buildWorkerController wires the launcher + controller for the worker. sockDir is a
// SHORT per-worker runtime dir (unix socket path limit) the agent processes bind their
// control sockets in; the launched agent-runtime processes are told the same dir.
func buildWorkerController(opts RunOptions, targetSpec, token, fingerprint string, client *AdminClient, logf func(string)) (*workercontroller.Controller, error) {
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
	starter, err := agentlauncher.NewExecStarter(agentlauncher.ExecStarterConfig{
		BaseArgs: baseArgs,
		BaseEnv:  withoutEnv(os.Environ(), "AC_EXECUTOR_GIT_WORKTREE"),
	})
	if err != nil {
		return nil, err
	}
	// Durable pid store (T860 gap5) so a worker restart re-adopts surviving agent
	// processes instead of double-spawning. Lives in the short per-worker sock dir.
	pidStore, err := agentlauncher.NewFilePIDStore(filepath.Join(sockDir, "agent-pids.json"))
	if err != nil {
		return nil, err
	}
	launcher, err := agentlauncher.New(agentlauncher.Config{
		Starter: starter,
		PIDs:    pidStore,
		// T860 gap4: a poison agent that crash-loops past MaxAttempts stops being rebuilt;
		// report it terminally errored so the center sees a stuck agent instead of a
		// silent hot-loop.
		OnExhausted: func(agentID string, lastErr error) {
			msg := "agent crash-looped past rebuild cap"
			if lastErr != nil {
				msg = msg + ": " + lastErr.Error()
			}
			rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if rerr := client.ReportAgentLifecycle(rctx, agentID, "error", msg, time.Now()); rerr != nil {
				logf(fmt.Sprintf("controller: report poison agent=%s: %v", agentID, rerr))
			}
		},
		Log: func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) },
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
	var desired []agentlauncher.AgentSpec
	for _, ra := range state.Agents {
		if strings.EqualFold(strings.TrimSpace(ra.DesiredLifecycle), "running") {
			desired = append(desired, agentRuntimeSpec(ra.AgentID, ra.ExecutorGitWorktree))
		}
	}
	// T860 gap5: adopt-aware boot reconcile — re-adopt agent processes that survived a
	// prior worker incarnation (verified live via pid pre-filter + control-socket
	// identity probe) instead of double-spawning them.
	ctrl.ReconcileWithAdoptionSpecs(ctx, desired)
	logf(fmt.Sprintf("controller: reconciled %d desired-running agent(s) at boot", len(desired)))
}

func agentRuntimeSpec(agentID string, gitWorktree bool) agentlauncher.AgentSpec {
	spec := agentlauncher.AgentSpec{AgentID: agentID}
	if gitWorktree {
		spec.Env = []string{"AC_EXECUTOR_GIT_WORKTREE=1"}
	}
	return spec
}

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}
