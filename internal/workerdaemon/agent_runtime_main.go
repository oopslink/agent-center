package workerdaemon

// agent_runtime_main.go — the `cmd/agent-runtime` process body (T854 D6, design
// §4.5/§5): ONE agent's self-contained runtime, run as its own OS process (the worker
// launches one per agent). It self-builds its center client (§4.2), self-triggers
// Boot recovery (D4), serves control commands proxied by the worker over a unix
// socket, and drives its own tick loop.
//
// 🔴 ORDERING (PD ruling): Boot self-recovery MUST finish before the control server
// accepts commands, so a proxied work_available cannot race the boot executor
// reconcile (double-fork / double-recover). The sequence is strictly:
//
//	config → self-built center client → NewLocalRuntime → Boot() → serve control → tick → drain
//
// CRASH MODEL (§4.5): this process has NO in-process self-heal (SelfHeal/RemoveAgent
// are nil). An unrecoverable failure = the process exits; the worker's launcher
// rebuilds it. The OS process boundary is the isolation.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/agentruntime"
	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
	"github.com/oopslink/agent-center/internal/agentruntime/sessioninstance"
	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
)

// AgentRuntimeOptions parameterises one agent-runtime process.
type AgentRuntimeOptions struct {
	// AgentID is the agent this process serves (required).
	AgentID string
	// SockDir is the SHORT per-worker runtime dir the control socket binds in (must
	// match the worker controller's SockDir). Required.
	SockDir string
	// TickInterval drives Tick + RunWatchdog (zero → 1s).
	TickInterval time.Duration
	// Worker bootstrap (mirrors RunOptions): the process re-uses the worker's config,
	// admin target, persisted token, and fingerprint — it does NOT re-enroll.
	Run RunOptions
}

// RunAgentRuntime runs one agent's runtime until ctx is cancelled, then drains.
func RunAgentRuntime(ctx context.Context, opts AgentRuntimeOptions, logf func(string)) error {
	if logf == nil {
		logf = func(string) {}
	}
	if strings.TrimSpace(opts.AgentID) == "" {
		return fmt.Errorf("agent-runtime: agent-id is required")
	}
	if strings.TrimSpace(opts.SockDir) == "" {
		return fmt.Errorf("agent-runtime: sock-dir is required")
	}

	// 1) config + self-built center client (NO enroll — the worker persisted the token).
	client, targetSpec, token, fingerprint, cfg, err := agentRuntimeClient(opts.Run, logf)
	if err != nil {
		return err
	}

	// 2) construct the single runtime. A supervisor-session crash signals fatalCh → the
	// process exits (drain) and the worker launcher rebuilds it (bounded backoff) — the
	// controller-model replacement for the retired in-process SelfHealStore.
	fatalCh := make(chan string, 1)
	onFatal := func(reason string) {
		select {
		case fatalCh <- reason:
		default: // already signalled
		}
	}
	rt, err := buildAgentRuntime(opts, cfg, client, targetSpec, token, fingerprint, logf, onFatal)
	if err != nil {
		return err
	}

	// 2b) 🔴 ENGINE-ATTACH-BEFORE-BOOT (T854 D6 fix, sibling to Boot-before-serve): a
	// crash-rebuilt agent-runtime process gets NO reconcile command (the center has not
	// re-pushed), so the executor engine MUST be attached from DURABLE config before
	// Boot — otherwise selfReconcile has no engine and in-flight executors can never be
	// recovered (the §4.4 core). The config comes from the center's ResumeState, which
	// survives the restart (in-process memory does not). Best-effort: no engine ⇒
	// single-active until the first reconcile re-attaches it.
	if ecfg, enabled, ferr := agentExecConfig(ctx, client, opts.Run.WorkerID, opts.AgentID); ferr != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s exec-config at boot: %v (single-active until reconcile)", opts.AgentID, ferr))
	} else {
		// Seed the stable identity ref (from ResumeState) BEFORE Boot's self-reconcile —
		// it drives the should-continue check's assignee comparison (T872). Set regardless
		// of concurrency so an agent that turns concurrent via a later reconcile still has
		// its ref (identity is stable; reconcile never overwrites it).
		rt.SetAgentRef(ecfg.AgentRef)
		if enabled {
			if aerr := rt.AttachExecutorEngine(ecfg); aerr != nil {
				logf(fmt.Sprintf("agent-runtime agent=%s attach executor engine: %v", opts.AgentID, aerr))
			} else {
				logf(fmt.Sprintf("agent-runtime agent=%s executor engine attached (max=%d) before Boot", opts.AgentID, ecfg.MaxConcurrentTasks))
			}
		}
	}

	// 3) Boot self-recovery — AFTER the engine is attached, BEFORE serving any command
	// (both ordering red lines).
	if berr := rt.Boot(ctx); berr != nil {
		// A boot-reconcile failure is not fatal on its own (best-effort recovery), but
		// it is logged so a persistent failure is visible; we still serve.
		logf(fmt.Sprintf("agent-runtime agent=%s boot reconcile: %v (continuing)", opts.AgentID, berr))
	}

	// 3b) 🔴 AUTONOMOUS SUPERVISOR-SESSION SELF-START (T860 gap6 fold-in): the agent-runtime
	// process starts its OWN supervisor session from local durable ResumeState HERE. The
	// center does NOT re-push a reconcile command for an already-desired-running agent (no
	// creation event), so without this a restarted/relaunched agent's session would never
	// come up — the restart-recovery deadlock the deleted daemon boot_reconcile guarded
	// against (empirically reconfirmed on a68defe9: control connects, but 0 reconcile → 0
	// rt.Start → session never starts). Runs AFTER executor self-reconcile (step 3), BEFORE
	// serving control (step 4) so a later reconcile — if any — hits the rt.Start idempotency
	// guard and converges on ONE session (no double-start).
	bootStartSupervisorSession(ctx, rt, client, opts, cfg, logf)

	// 4) control server — opened ONLY after Boot returned.
	sockPath := filepath.Join(opts.SockDir, agentcontrol.SocketName(opts.AgentID))
	srv, err := agentcontrol.NewServer(sockPath, opts.AgentID, agentControlHandler{rt: rt, log: logf}, func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) })
	if err != nil {
		return fmt.Errorf("agent-runtime: control server: %w", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve() }()
	logf(fmt.Sprintf("agent-runtime agent=%s serving control at %s", opts.AgentID, sockPath))

	// 5) tick loop.
	tick := opts.TickInterval
	if tick <= 0 {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 6) graceful drain: stop accepting commands, then stop the session.
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = srv.Close(shutCtx)
			_ = rt.Stop(shutCtx)
			cancel()
			logf(fmt.Sprintf("agent-runtime agent=%s drained", opts.AgentID))
			return nil
		case reason := <-fatalCh:
			// gap4: the supervisor session crashed unexpectedly → drain + exit so the
			// launcher rebuilds this process fresh (re-Boot + re-Start). Non-zero exit.
			logf(fmt.Sprintf("agent-runtime agent=%s fatal: %s — exiting for launcher rebuild", opts.AgentID, reason))
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = srv.Close(shutCtx)
			_ = rt.Stop(shutCtx)
			cancel()
			return fmt.Errorf("agent-runtime: supervisor session crashed: %s", reason)
		case err := <-serveErr:
			if err != nil {
				return fmt.Errorf("agent-runtime: control server exited: %w", err)
			}
		case now := <-ticker.C:
			if terr := rt.Tick(ctx, now); terr != nil {
				logf(fmt.Sprintf("agent-runtime agent=%s tick: %v", opts.AgentID, terr))
			}
			rt.RunWatchdog(ctx)
		}
	}
}

// agentRuntimeClient reproduces RunDaemon's transport bootstrap for the agent process:
// load config, resolve the admin target/fingerprint, and load the worker's persisted
// long-term token (the worker already enrolled — the agent process must NOT re-enroll).
func agentRuntimeClient(opts RunOptions, logf func(string)) (client *AdminClient, targetSpec, token, fingerprint string, cfg config.Config, err error) {
	if strings.TrimSpace(opts.WorkerID) == "" {
		return nil, "", "", "", cfg, fmt.Errorf("agent-runtime: worker-id is required")
	}
	cfg, cerr := config.Load(config.LoadOptions{Path: opts.ConfigPath})
	if cerr != nil {
		return nil, "", "", "", cfg, fmt.Errorf("agent-runtime: config: %v", cerr)
	}
	targetSpec = strings.TrimSpace(opts.AdminTarget)
	if targetSpec == "" {
		sock := strings.TrimSpace(cfg.Server.AdminSocketPath)
		if sock == "" {
			return nil, "", "", "", cfg, fmt.Errorf("agent-runtime: --admin-target or server.admin_socket_path required")
		}
		targetSpec = "unix:" + sock
	}
	parsed, perr := clienttransport.ParseTarget(targetSpec)
	if perr != nil {
		return nil, "", "", "", cfg, fmt.Errorf("agent-runtime: %w", perr)
	}
	fingerprint = strings.TrimSpace(opts.ServerFingerprint)
	if fingerprint == "" {
		fingerprint = strings.TrimSpace(os.Getenv("AGENT_CENTER_SERVER_FINGERPRINT"))
	}
	client, err = NewAdminClientFromTarget(parsed, fingerprint, 30*time.Second)
	if err != nil {
		return nil, "", "", "", cfg, fmt.Errorf("agent-runtime: admin client: %w", err)
	}
	// Prefer the worker's persisted long-term token; fall back to the flag/env.
	token = strings.TrimSpace(opts.AdminToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AGENT_CENTER_ADMIN_TOKEN"))
	}
	tokenPath := workerTokenFilePath(cfg, opts.ConfigPath, opts.WorkerID)
	if existing, rerr := readWorkerTokenFile(tokenPath); rerr == nil && existing != "" {
		token = existing
	} else if token == "" {
		return nil, "", "", "", cfg, fmt.Errorf("agent-runtime: no worker token (expected %s from the worker's enroll)", tokenPath)
	}
	client = client.WithToken(token)
	return client, targetSpec, token, fingerprint, cfg, nil
}

// buildAgentRuntime constructs the single-agent LocalRuntime. It mirrors the daemon's
// baseRuntimeConfig recipe but with the process-model differences: NO in-process self-
// heal — a supervisor-session crash fires OnFatal (→ process exit → launcher rebuild).
//
// It returns an error only for a FAIL-LOUD misconfiguration (a repo-workspace flag that
// is on but unusable): starting inert, in a silently different workspace shape than the
// operator asked for, is worse than not starting.
func buildAgentRuntime(opts AgentRuntimeOptions, cfg config.Config, client *AdminClient, targetSpec, token, fingerprint string, logf func(string), onFatal func(reason string)) (*agentruntime.LocalRuntime, error) {
	binPath, _ := os.Executable()
	disableUsage := false
	if v := strings.TrimSpace(os.Getenv("AGENT_CENTER_DISABLE_USAGE_REPORT")); v == "1" || strings.EqualFold(v, "true") {
		disableUsage = true
	}
	homeBase := agentHomeBase(cfg, opts.Run.ConfigPath, opts.Run.WorkerID)
	rc := agentruntime.LocalRuntimeConfig{
		AgentID:            opts.AgentID,
		Reporter:           client,
		Starter:            startSupervisorSessionAdapter,
		CodexStarter:       startCodexSessionAdapter,
		ToolCaller:         func() agentruntime.ToolCaller { return client },
		WorkerID:           opts.Run.WorkerID,
		AdminURL:           targetSpec,
		WorkerToken:        token,
		ServerFingerprint:  fingerprint,
		BinaryPath:         binPath,
		ClaudeBinary:       strings.TrimSpace(os.Getenv("AGENT_CENTER_CLAUDE_BINARY")),
		CodexBinary:        strings.TrimSpace(os.Getenv("AGENT_CENTER_CODEX_BINARY")),
		AgentHomeBase:      homeBase,
		Log:                func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) },
		DisableUsageReport: func() bool { return disableUsage },
		TaskDirManager:     taskexec.NewDirManager(),
		EventWriter:        taskexec.NewEventStreamWriter(),
		// §4.5 process model: no in-process self-heal — a supervisor crash fires OnFatal →
		// this process exits and the worker launcher rebuilds it (bounded backoff).
		OnFatal: onFatal,
	}
	// Every code executor needs a deterministic repository preflight. The worktree
	// option controls the source-reuse strategy; it no longer controls whether a
	// repository exists at all. Wiring the materializer unconditionally prevents the
	// OFF path from silently spawning a model in an empty directory.
	home := filepath.Join(homeBase, "agents", opts.AgentID)
	reposRoot := filepath.Join(home, "repos")
	mat, merr := reporepo.NewLocalGitMaterializer(reposRoot, nil, nil)
	if merr != nil {
		return nil, fmt.Errorf("agent-runtime agent=%s: build executor repo materializer at %s: %w",
			opts.AgentID, reposRoot, merr)
	}
	mat.Log = func(msg string) { logf(msg) }
	rc.Materializer = mat
	rc.ReposRoot = reposRoot
	return agentruntime.NewLocalRuntime(rc, &agentruntime.SessionState{}), nil
}

// agentExecConfig fetches THIS agent's executor config from the center ResumeState —
// the DURABLE source that survives a process restart (T854 D6 fix): the launcher
// rebuilds the process with no reconcile command, so the boot engine-attach reads the
// center's desired config here rather than lost in-process memory. Returns (config,
// concurrencyEnabled, err); a missing agent in the resume set is (zero, false, nil).
func agentExecConfig(ctx context.Context, client *AdminClient, workerID, agentID string) (agentruntime.ExecutorConfig, bool, error) {
	state, err := client.ResumeState(ctx, workerID)
	if err != nil {
		return agentruntime.ExecutorConfig{}, false, err
	}
	for _, ra := range state.Agents {
		if ra.AgentID == agentID {
			return execConfigFromResumeAgent(ra)
		}
	}
	return agentruntime.ExecutorConfig{}, false, nil
}

// agentResumeRecord fetches THIS agent's full resume record (desired lifecycle + in-flight
// work + start config) from the center ResumeState, for the boot supervisor-session
// reconcile. ok=false when the center has no record for the agent (an orphan).
func agentResumeRecord(ctx context.Context, client *AdminClient, workerID, agentID string) (ResumeAgent, bool, error) {
	state, err := client.ResumeState(ctx, workerID)
	if err != nil {
		return ResumeAgent{}, false, err
	}
	for _, ra := range state.Agents {
		if ra.AgentID == agentID {
			return ra, true, nil
		}
	}
	return ResumeAgent{}, false, nil
}

// bootStartSupervisorSession is the T860 gap6 fold-in enactment: it wires the previously
// dead-coded supervisor-session boot decision (agentruntime.DecideBootSession) so the
// agent-runtime process autonomously (re)starts its supervisor session from local durable
// state — instead of waiting for a control reconcile command the center does NOT re-push
// for an already-desired-running agent. Probes the local supervisor, decides, and enacts
// exactly one action:
//   - Reattachable + desired-running → reattach the live survivor (never interrupt it),
//   - Unavailable  + desired-running → reap + relaunch (resume-gated on the prior
//     completed turn) + re-trigger the reply-guardrail,
//   - desired-stopped / orphan       → reap residue only.
//
// NOTE (v2.14.0 I14): there is deliberately NO per-agent WorkItem rebind / resume-nudge
// here. AgentWorkItem was retired — resume-state carries no in-flight tasks, and a
// task's continuity across a restart is handled at the TASK layer (its execution lease
// lapses when this session no longer renews it → the center re-dispatches it). Rebinding
// CurrentTaskID at boot would renew a lease for a task the resumed session is not driving
// and BLOCK that re-dispatch recovery; a resume-nudge would risk double-driving it. The
// resumed session's continuity is its conversation context (--resume) + the reply-guardrail.
//
// Best-effort: a per-step failure is logged; the process still serves. Idempotent with a
// later control reconcile via the rt.Start no-double-start guard.
func bootStartSupervisorSession(ctx context.Context, rt *agentruntime.LocalRuntime, client *AdminClient, opts AgentRuntimeOptions, cfg config.Config, logf func(string)) {
	agentID := opts.AgentID
	ra, ok, ferr := agentResumeRecord(ctx, client, opts.Run.WorkerID, agentID)
	if ferr != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session resume-state: %v (skip autonomous start)", agentID, ferr))
		return
	}
	if !ok {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session: no center record — skip", agentID))
		return
	}
	desiredRunning := strings.EqualFold(strings.TrimSpace(ra.DesiredLifecycle), "running")
	home := filepath.Join(agentHomeBase(cfg, opts.Run.ConfigPath, opts.Run.WorkerID), "agents", agentID)

	pr, perr := supervisormanager.ProbeAgent(ctx, home)
	if perr != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session probe: %v — treating as unavailable", agentID, perr))
		pr = supervisormanager.ProbeResult{State: supervisormanager.Unavailable}
	}
	action := agentruntime.DecideBootSession(pr.State, desiredRunning)
	logf(fmt.Sprintf("boot-session decide agent=%s probe=%d desired-running=%v action=%d", agentID, pr.State, desiredRunning, action))

	switch action {
	case agentruntime.BootSessionReattach:
		bootReattachSupervisor(ctx, rt, agentID, home, pr, logf)
	case agentruntime.BootSessionReapRelaunch:
		closeProbeClient(pr)
		bootReapRelaunchSupervisor(ctx, rt, agentID, home, ra, logf)
	case agentruntime.BootSessionStopReap, agentruntime.BootSessionReapOnly:
		closeProbeClient(pr)
		if rerr := supervisormanager.ReapResidual(home); rerr != nil {
			logf(fmt.Sprintf("agent-runtime agent=%s boot-session reap: %v", agentID, rerr))
		}
	default: // BootSessionNoop
		closeProbeClient(pr)
	}
}

// closeProbeClient closes a probe's attach client when we are NOT reattaching (Unavailable
// carries a nil client; a Reattachable one we choose not to take over must be closed).
func closeProbeClient(pr supervisormanager.ProbeResult) {
	if pr.Client != nil {
		_ = pr.Client.Close()
	}
}

// bootReapRelaunchSupervisor reaps residue then relaunches a fresh supervisor session,
// resume-gated on whether the PRIOR generation completed a clean turn (else the
// no-completed-turn crash loop). Mirrors the retired daemon bootReapRelaunch; the
// executor engine was already attached at boot step 2b.
func bootReapRelaunchSupervisor(ctx context.Context, rt *agentruntime.LocalRuntime, agentID, home string, ra ResumeAgent, logf func(string)) {
	if rerr := supervisormanager.ReapResidual(home); rerr != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session reap before relaunch: %v", agentID, rerr))
	}
	// Read the prior instance BEFORE Start — Start's AcquireInstance bumps the generation
	// and writes a fresh instance with CompletedTurn=false.
	prev, prevErr := sessioninstance.ReadInstance(home)
	if prevErr != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session read session.instance: %v — relaunch fresh (no resume)", agentID, prevErr))
		prev = sessioninstance.InstanceState{}
	}
	cli := strings.TrimSpace(ra.CLI)
	isCodex := cli == "codex"
	resume := !isCodex && prev.CompletedTurn // codex is one-shot per turn — no resume
	if serr := rt.Start(ctx, agentruntime.StartSpec{
		AgentID:            agentID,
		Version:            ra.Version,
		ForkResume:         !isCodex,
		Resume:             resume,
		Model:              ra.Model,
		DisplayName:        ra.DisplayName,
		CLI:                cli,
		PromptDescription:  ra.PromptDescription,
		EnvVars:            ra.EnvVars,
		ConcurrencyEnabled: agent.Profile{MaxConcurrentTasks: ra.MaxConcurrentTasks, AllowedExecutors: ra.AllowedExecutors}.ConcurrencyEnabled(),
	}); serr != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session relaunch: %v — skip", agentID, serr))
		return
	}
	logf(fmt.Sprintf("boot-session relaunch agent=%s resume=%v version=%d", agentID, resume, ra.Version))
	// Reply-guardrail re-trigger (T341 fix B): proactively re-inject an unanswered directed
	// message after the relaunch. (No per-agent WorkItem resume — see the note on
	// bootStartSupervisorSession: task continuity is the lease/re-dispatch layer's job.)
	rt.MaybeReplyNudge(agentID)
}

// bootReattachSupervisor takes over a live survivor supervisor without interrupting it
// (the "don't re-exec a mid-turn claude" invariant — the supervisor-session analog of the
// executor adopt-alive path). The probe carries the open client; the reattached session
// takes it over.
func bootReattachSupervisor(ctx context.Context, rt *agentruntime.LocalRuntime, agentID, home string, pr supervisormanager.ProbeResult, logf func(string)) {
	ref := supervisormanager.RefFromProbe(home, pr)
	if ref == nil || ref.Client == nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session reattach: probe carried no live ref/client — skip", agentID))
		closeProbeClient(pr)
		return
	}
	sess, err := ReattachSupervisorSession(ctx, ref, ref.Client,
		rt.OnEventCallback(), rt.OnExitCallback(),
		func(msg string) { logf(msg) },
		pr.Hello.BaseOffset)
	if err != nil {
		logf(fmt.Sprintf("agent-runtime agent=%s boot-session reattach: %v", agentID, err))
		return
	}
	rt.Attach(sess)
	logf(fmt.Sprintf("boot-session reattach agent=%s from offset=%d", agentID, pr.Hello.BaseOffset))
}

// execConfigFromResumeAgent projects a ResumeAgent's concurrency fields into the neutral
// ExecutorConfig BuildExecutorEngine consumes, plus whether concurrency is enabled (the
// SAME predicate the daemon's concurrencyEnabled uses).
func execConfigFromResumeAgent(ra ResumeAgent) (agentruntime.ExecutorConfig, bool, error) {
	enabled := agent.Profile{MaxConcurrentTasks: ra.MaxConcurrentTasks, AllowedExecutors: ra.AllowedExecutors}.ConcurrencyEnabled()
	return agentruntime.ExecutorConfig{
		AgentID:              ra.AgentID,
		AgentRef:             ra.AgentRef,
		DisplayName:          ra.DisplayName,
		EnvVars:              ra.EnvVars,
		MaxConcurrentTasks:   ra.MaxConcurrentTasks,
		AllowedExecutors:     ra.AllowedExecutors,
		OrchestratorModel:    ra.OrchestratorModel,
		DefaultExecutorModel: ra.DefaultExecutorModel,
		JudgeEnabled:         ra.JudgeEnabled, // T950 ②: per-agent judge opt-in (default OFF)
		// ra.Model = the agent's own (supervisor) model — the router's last-resort
		// executor-model fallback ("use whatever the supervisor uses"). Previously dropped
		// here, so an agent with no default/orchestrator/pool config stranded its tasks in
		// a silent fork-fail loop.
		SupervisorModel: ra.Model,
		CLI:             ra.CLI,
	}, enabled, nil
}

// agentControlHandler maps a proxied control Command to the runtime's command entry.
// A handler error propagates to the worker as an undelivered command (the worker
// leaves it un-acked and retries), so a command that cannot be applied is not lost.
type agentControlHandler struct {
	rt  *agentruntime.LocalRuntime
	log func(string)
}

// Handle decodes cmd.Payload — the RAW center command payload the worker proxied
// verbatim — using the SAME daemon payload types + converters the in-process path
// used, and dispatches to the matching runtime method. Reusing the daemon's decoders
// (this file is in the workerdaemon package) keeps the wire contract byte-identical to
// the pre-D6 in-process routing.
func (h agentControlHandler) Handle(ctx context.Context, cmd agentcontrol.Command) error {
	switch cmd.Type {
	case cmdTypeAgentReconcile:
		var pl reconcilePayload
		if err := decode(cmd.Payload, &pl); err != nil {
			return err
		}
		if err := h.rt.Start(ctx, startSpecOf(pl)); err != nil {
			return err
		}
		// Steady-state / first-dispatch path: attach the executor engine when the
		// reconcile enables concurrency and it isn't already attached (the boot path
		// attaches from ResumeState; this covers a config that turns concurrency ON
		// after boot).
		if execConfigOf(pl).ConcurrencyEnabled() {
			if !h.rt.HasExecutor() {
				if err := h.rt.AttachExecutorEngine(execConfigOf(pl)); err != nil {
					return err
				}
			} else {
				// Already attached (boot or a prior reconcile): a live profile edit must
				// refresh the model routing IN PLACE. Re-attaching would rebuild the
				// engine and drop live executors/orphans — the invariant the old
				// !HasExecutor() guard protected — so we update the config only. This is
				// what makes a web-console config change reach a RUNNING concurrent agent
				// without a restart (k8s: the reconcile payload carries the fresh config).
				h.rt.UpdateExecutorConfig(execConfigOf(pl))
			}
		}
		return nil
	case cmdTypeAgentWork:
		var pl workPayload
		if err := decode(cmd.Payload, &pl); err != nil {
			return err
		}
		return h.rt.NotifyWork(ctx, workRequestOf(pl))
	case cmdTypeAgentWake:
		var pl wakePayload
		if err := decode(cmd.Payload, &pl); err != nil {
			return err
		}
		return h.rt.NotifyWake(ctx, wakeRequestOf(pl))
	case cmdTypeAgentConverse:
		var pl conversePayload
		if err := decode(cmd.Payload, &pl); err != nil {
			return err
		}
		return h.rt.NotifyConverse(ctx, converseRequestOf(pl))
	case cmdTypeWorkAvailable:
		var pl workAvailablePayload
		if err := decode(cmd.Payload, &pl); err != nil {
			return err
		}
		return h.rt.NotifyWorkAvailable(ctx, pl.TaskID)
	default:
		return fmt.Errorf("agent-runtime: unknown control command type %q", cmd.Type)
	}
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}
