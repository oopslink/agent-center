package workerdaemon

// agentruntime_bridge.go — the Phase 0b seam between the daemon and the
// agentruntime package. It (1) ALIASES the shared types that moved DOWN into
// agentruntime back into workerdaemon so existing daemon code + tests keep
// compiling unchanged, (2) adapts the daemon's concrete session starters to the
// runtime's starter contracts, and (3) builds a per-agent LocalRuntime.

import (
	"context"
	"strings"

	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
)

// --- Type aliases (moved DOWN; daemon aliases back). ---

// agentSession is the narrow session control surface (Inject/Stop/Detach).
type agentSession = agentruntime.Session

// sessionStarter / codexSessionStarter are the per-CLI session factories (test seams).
type sessionStarter = agentruntime.SessionStarter
type codexSessionStarter = agentruntime.CodexSessionStarter

// SupervisorSessionConfig configures a claude supervisor session start.
type SupervisorSessionConfig = agentruntime.SupervisorSessionConfig

// feedbackReporter is the RESULT-feedback seam.
type feedbackReporter = agentruntime.Reporter

// executorEngine is the per-agent concurrent-execution wiring (moved DOWN in Phase
// 0c). The daemon aliases it back so existing code/tests referencing the name keep
// compiling; the ENGINE itself now lives on the agent's LocalRuntime (r.exec).
type executorEngine = agentruntime.ExecutorEngine

// agentToolCaller is the narrow center agent-tool transport seam (moved DOWN). The
// daemon's *AdminClient satisfies it (CallAgentTool); tests inject a fake.
type agentToolCaller = agentruntime.ToolCaller

// UsageReport is one per-turn usage sample.
type UsageReport = agentruntime.UsageReport

// --- Const aliases. ---

const cliCodex = agentruntime.CLICodex
const wakeDedupCap = agentruntime.WakeDedupCap

// DefaultResumeNudge is injected into a RELAUNCHED agent's session for an ACTIVE
// WorkItem on boot/self-heal so the interrupted task continues.
const DefaultResumeNudge = agentruntime.DefaultResumeNudge

// --- Test-facing re-exports (pure helpers moved down; workerdaemon tests call these). ---

var streamActivityPayload = agentruntime.StreamActivityPayload
var activityEventType = agentruntime.ActivityEventType
var toolResultIsError = agentruntime.ToolResultIsError

// buildConverseBrief renders the converse stdin brief (tests call it with a
// conversePayload; convert at the boundary to the runtime's neutral request).
func buildConverseBrief(pl conversePayload) string {
	return agentruntime.BuildConverseBrief(converseRequestOf(pl))
}

// --- Payload → runtime request converters (byte-identical field mapping). ---

func workRequestOf(pl workPayload) agentruntime.WorkRequest {
	return agentruntime.WorkRequest{AgentID: pl.AgentID, TaskID: pl.TaskID, TaskRef: pl.TaskRef, Brief: pl.Brief}
}

func wakeRequestOf(pl wakePayload) agentruntime.WakeRequest {
	return agentruntime.WakeRequest{
		AgentID: pl.AgentID, TaskID: pl.TaskID, TaskRef: pl.TaskRef,
		ConversationID: pl.ConversationID, MessageID: pl.MessageID,
		MessageText: pl.MessageText, RootMessageID: pl.RootMessageID,
	}
}

func converseRequestOf(pl conversePayload) agentruntime.ConverseRequest {
	return agentruntime.ConverseRequest{
		AgentID: pl.AgentID, ConversationID: pl.ConversationID, ConvKind: pl.ConvKind,
		ConvName: pl.ConvName, SenderRef: pl.SenderRef, SenderDisplay: pl.SenderDisplay,
		MessageID: pl.MessageID, MessageText: pl.MessageText, RootMessageID: pl.RootMessageID,
		AttachmentCount: pl.AttachmentCount, OwnerRef: pl.OwnerRef,
	}
}

// --- Session starters (production adapters). ---

// startSupervisorSessionAdapter is the PRODUCTION claude session starter.
func startSupervisorSessionAdapter(ctx context.Context, cfg SupervisorSessionConfig) (agentSession, error) {
	s, err := StartSupervisorSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// startCodexSessionAdapter is the PRODUCTION codex session starter. It converts the
// neutral CodexSpec → CodexSessionConfig (computing the merged runtime env) and
// starts the real CodexSession.
func startCodexSessionAdapter(ctx context.Context, spec agentruntime.CodexSpec) (agentSession, error) {
	s, err := StartCodexSession(ctx, CodexSessionConfig{
		AgentID:  spec.AgentID,
		TasksDir: spec.TasksDir,
		Binary:   spec.Binary,
		Model:    spec.Model,
		Env:      runtimeAgentEnv(spec.AgentID, spec.DisplayName, spec.EnvVars),
		Logger:   spec.Logger,
		OnEvent:  spec.OnEvent,
		OnExit:   spec.OnExit,
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// baseRuntimeConfig builds the per-agent-invariant LocalRuntimeConfig (everything
// except AgentID). The SHARED mutex is &c.mu (pointer — NEVER a copy). Called under
// no lock; the returned value is copied per agent (copy-safe: pointers/funcs/scalars).
func (c *AgentController) baseRuntimeConfig() agentruntime.LocalRuntimeConfig {
	return agentruntime.LocalRuntimeConfig{
		Mu:                      &c.mu,
		Reporter:                c.cfg.Reporter,
		Starter:                 c.cfg.starter,
		CodexStarter:            c.cfg.codexStarter,
		ToolCaller:              func() agentruntime.ToolCaller { return c.cfg.ToolCaller },
		WorkerID:                c.cfg.WorkerID,
		AdminURL:                c.cfg.AdminURL,
		WorkerToken:             c.cfg.WorkerToken,
		ServerFingerprint:       c.cfg.ServerFingerprint,
		BinaryPath:              c.cfg.BinaryPath,
		ClaudeBinary:            c.cfg.ClaudeBinary,
		CodexBinary:             c.cfg.CodexBinary,
		AgentHomeBase:           c.cfg.AgentHomeBase,
		Log:                     c.log,
		Now:                     c.cfg.Now,
		StopGrace:               c.cfg.StopGrace,
		DisableUsageReport:      func() bool { return c.cfg.DisableUsageReport },
		ResumeNudge:             c.cfg.ResumeNudge,
		SelfHeal:                c.selfHeal,
		RateLimitDefaultBackoff: c.cfg.RateLimitDefaultBackoff,
		RateLimitMinBackoff:     c.cfg.RateLimitMinBackoff,
		RateLimitMaxBackoff:     c.cfg.RateLimitMaxBackoff,
		APIErrorBackoffBase:     c.cfg.APIErrorBackoffBase,
		APIErrorBackoffCap:      c.cfg.APIErrorBackoffCap,
		APIErrorMaxRetries:      c.cfg.APIErrorMaxRetries,
		TaskDirManager:          c.cfg.TaskDirManager,
		SegmentMaxBytes:         c.cfg.SegmentMaxBytes,
		TaskLogMaxBytes:         c.cfg.TaskLogMaxBytes,
		EventWriter:             c.eventWriter,
		BG:                      &c.bg,
		RemoveAgent:             c.removeAgentLocked,
	}
}

// newRuntimeFor builds a LocalRuntime for agentID over a fresh SessionState and
// returns both (the daemon's managedAgent holds the SAME state pointer).
func (c *AgentController) newRuntimeFor(agentID string) (*agentruntime.LocalRuntime, *agentruntime.SessionState) {
	st := &agentruntime.SessionState{}
	cfg := c.baseRuntimeConfig()
	cfg.AgentID = agentID
	return agentruntime.NewLocalRuntime(cfg, st), st
}

// removeAgentLocked deletes the managedAgent from the map. Called with c.mu HELD
// (onExit crash path) — it must NOT lock c.mu.
func (c *AgentController) removeAgentLocked(agentID string) {
	delete(c.agents, agentID)
}

// reportRecovered clears a lingering center `error` → running (recovery paths). Routes
// through the agent's runtime; no-op if none.
func (c *AgentController) reportRecovered(agentID string) {
	if rt := c.runtimeFor(agentID); rt != nil {
		rt.ReportRecovered()
	}
}

// maybeReplyNudge re-triggers the T341 reply-guardrail (boot relaunch). Routes through
// the agent's runtime; no-op if none.
func (c *AgentController) maybeReplyNudge(agentID string) {
	if rt := c.runtimeFor(agentID); rt != nil {
		rt.MaybeReplyNudge(agentID)
	}
}

// resumeNudgeText is the message injected to re-drive an interrupted turn (boot
// relaunch), matching the runtime's resolution.
func (c *AgentController) resumeNudgeText() string {
	if msg := strings.TrimSpace(c.cfg.ResumeNudge); msg != "" {
		return c.cfg.ResumeNudge
	}
	return DefaultResumeNudge
}

// bringUpSession reserves a fresh managedAgent (runtime + shared SessionState) for
// the agent, then starts the session via the runtime. On failure it rolls back the
// reservation (identity-guarded). This replaces the old startSession's reserve +
// spawn + rollback (the session-bring-up logic itself now lives in LocalRuntime.Start).
func (c *AgentController) bringUpSession(ctx context.Context, spec agentruntime.StartSpec) error {
	rt, st := c.newRuntimeFor(spec.AgentID)
	ma := &managedAgent{agentID: spec.AgentID, runtime: rt, state: st, appliedVersion: spec.Version}
	c.mu.Lock()
	c.agents[spec.AgentID] = ma
	c.mu.Unlock()
	if err := rt.Start(ctx, spec); err != nil {
		c.mu.Lock()
		if c.agents[spec.AgentID] == ma {
			delete(c.agents, spec.AgentID)
		}
		c.mu.Unlock()
		return err
	}
	return nil
}

// stopViaRuntime stops the agent's session through its runtime. When reportLifecycle
// it settles "stopped" once (the sole lifecycle reporter). Mirrors the old stopSession.
func (c *AgentController) stopViaRuntime(ctx context.Context, agentID string, reportLifecycle bool) {
	c.mu.Lock()
	ma := c.agents[agentID]
	var rt *agentruntime.LocalRuntime
	if ma != nil {
		rt = ma.runtime
	}
	c.mu.Unlock()
	if rt == nil {
		if reportLifecycle {
			c.settleStopped(ctx, agentID)
		}
		return
	}
	if reportLifecycle {
		_ = rt.StopReporting(ctx)
	} else {
		_ = rt.Stop(ctx)
	}
}

// settleStopped emits a lifecycle "stopped" for an agent with no runtime (the
// no-process settle path — matches the old reportLifecycleOnce ma==nil branch).
func (c *AgentController) settleStopped(ctx context.Context, agentID string) {
	if err := c.cfg.Reporter.ReportAgentLifecycle(ctx, agentID, "stopped", "", c.now()); err != nil {
		c.log("agent=%s report stopped: %v", agentID, err)
	}
}

// reportLifecycleOnce routes a lifecycle settle through the agent's runtime (its
// per-instance sync.Once) when one exists, else emits directly. Preserves the old
// controller reportLifecycleOnce semantics.
func (c *AgentController) reportLifecycleOnce(ctx context.Context, agentID, state, errMsg string) {
	c.mu.Lock()
	ma := c.agents[agentID]
	var rt *agentruntime.LocalRuntime
	if ma != nil {
		rt = ma.runtime
	}
	c.mu.Unlock()
	if rt != nil {
		rt.ReportLifecycleOnce(ctx, state, errMsg)
		return
	}
	if err := c.cfg.Reporter.ReportAgentLifecycle(ctx, agentID, state, errMsg, c.now()); err != nil {
		c.log("agent=%s report %s: %v", agentID, state, err)
	}
}

// startSpecOf builds a StartSpec from a reconcile payload (intent-driven start —
// forkResume=false).
func startSpecOf(pl reconcilePayload) agentruntime.StartSpec {
	return agentruntime.StartSpec{
		AgentID:            pl.AgentID,
		Version:            pl.Version,
		ForkResume:         false,
		Resume:             false,
		Model:              pl.Model,
		DisplayName:        pl.DisplayName,
		CLI:                pl.CLI,
		PromptDescription:  pl.PromptDescription,
		EnvVars:            pl.EnvVars,
		ConcurrencyEnabled: concurrencyEnabled(pl),
	}
}

