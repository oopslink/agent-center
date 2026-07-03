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
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentcontrol"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
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

	// 2) construct the single runtime (SelfHeal/RemoveAgent nil — crash=process exit).
	rt := buildAgentRuntime(opts, cfg, client, targetSpec, token, fingerprint, logf)

	// 3) Boot self-recovery — BEFORE serving any command (ordering red line).
	if berr := rt.Boot(ctx); berr != nil {
		// A boot-reconcile failure is not fatal on its own (best-effort recovery), but
		// it is logged so a persistent failure is visible; we still serve.
		logf(fmt.Sprintf("agent-runtime agent=%s boot reconcile: %v (continuing)", opts.AgentID, berr))
	}

	// 4) control server — opened ONLY after Boot returned.
	sockPath := filepath.Join(opts.SockDir, agentcontrol.SocketName(opts.AgentID))
	srv, err := agentcontrol.NewServer(sockPath, agentControlHandler{rt: rt, log: logf}, func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) })
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
// baseRuntimeConfig recipe but with the process-model differences: NO SelfHeal and NO
// RemoveAgent (an unrecoverable failure exits the process; the launcher rebuilds it).
func buildAgentRuntime(opts AgentRuntimeOptions, cfg config.Config, client *AdminClient, targetSpec, token, fingerprint string, logf func(string)) *agentruntime.LocalRuntime {
	binPath, _ := os.Executable()
	disableUsage := false
	if v := strings.TrimSpace(os.Getenv("AGENT_CENTER_DISABLE_USAGE_REPORT")); v == "1" || strings.EqualFold(v, "true") {
		disableUsage = true
	}
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
		AgentHomeBase:      agentHomeBase(cfg, opts.Run.ConfigPath, opts.Run.WorkerID),
		Log:                func(f string, a ...any) { logf(fmt.Sprintf(f, a...)) },
		DisableUsageReport: func() bool { return disableUsage },
		TaskDirManager:     taskexec.NewDirManager(),
		EventWriter:        taskexec.NewEventStreamWriter(),
		// §4.5 process model: no in-process self-heal / removal — the process exits and
		// the worker launcher rebuilds it.
		SelfHeal:    nil,
		RemoveAgent: nil,
	}
	return agentruntime.NewLocalRuntime(rc, &agentruntime.SessionState{})
}

// agentControlHandler maps a proxied control Command to the runtime's command entry.
// A handler error propagates to the worker as an undelivered command (the worker
// leaves it un-acked and retries), so a command that cannot be applied is not lost.
type agentControlHandler struct {
	rt  *agentruntime.LocalRuntime
	log func(string)
}

// Handle decodes cmd.Payload (a runtime-request the worker already converted from the
// center payload) and dispatches to the matching runtime method.
func (h agentControlHandler) Handle(ctx context.Context, cmd agentcontrol.Command) error {
	switch cmd.Type {
	case "reconcile":
		var spec agentruntime.StartSpec
		if err := decode(cmd.Payload, &spec); err != nil {
			return err
		}
		return h.rt.Start(ctx, spec)
	case "work":
		var req agentruntime.WorkRequest
		if err := decode(cmd.Payload, &req); err != nil {
			return err
		}
		return h.rt.NotifyWork(ctx, req)
	case "wake":
		var req agentruntime.WakeRequest
		if err := decode(cmd.Payload, &req); err != nil {
			return err
		}
		return h.rt.NotifyWake(ctx, req)
	case "converse":
		var req agentruntime.ConverseRequest
		if err := decode(cmd.Payload, &req); err != nil {
			return err
		}
		return h.rt.NotifyConverse(ctx, req)
	case "work_available":
		var p struct {
			TaskID string `json:"task_id"`
		}
		if err := decode(cmd.Payload, &p); err != nil {
			return err
		}
		return h.rt.NotifyWorkAvailable(ctx, p.TaskID)
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
